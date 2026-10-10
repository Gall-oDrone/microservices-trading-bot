// Command etoro-spike records the eToro facts the index-CFD port depends on
// (porting plan, phase P1) as committed evidence:
//
//   - instrument ids of the traded CFDs and their ETF benchmarks;
//   - the account's eligibility (allowed leverages, minimum exposure, SL bounds);
//   - live quotes and spreads, and a what-if cost preview (spread, overnight
//     fee) for the forward-test size at x1;
//   - daily bars from the live route turned into trading-day bars, with what
//     was dropped (weekend / holiday / in-progress) and coverage;
//   - hourly bar coverage by weekday (when the CFDs actually quote);
//   - how far back and how stale the DataPlatform history route is;
//   - the demo account's cash and equity (no account ids);
//   - optionally (-roundtrip) one demo open+close through the broker adapter,
//     including an idempotent re-Open with the same client reference.
//
// It never runs against a real account. Credentials come from the
// environment (ETORO_PUBLIC_KEY, ETORO_PRIVATE_KEY, ETORO_ENV=demo) and are
// never written to the output.
//
//	set -a; . ./.env.etoro.local; set +a
//	go run ./cmd/etoro-spike -out ../../docs/etoro/evidence-2026-10-10 [-roundtrip]
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/broker/etorobroker"
	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/etorodaily"
)

func main() {
	out := flag.String("out", "", "evidence directory (required)")
	traded := flag.String("symbols", "NSDQ100,SPX500", "traded index CFDs")
	bench := flag.String("benchmarks", "QQQ,SPY", "ETF benchmarks")
	amount := flag.Float64("amount", 1000, "forward-test position size (USD) for cost previews and the round trip")
	roundtrip := flag.Bool("roundtrip", false, "place and close one demo position through the broker adapter")
	rtSymbol := flag.String("roundtrip-symbol", "NSDQ100", "instrument for -roundtrip")
	flag.Parse()
	if *out == "" {
		log.Fatal("-out is required")
	}
	env, err := etoro.ParseEnvironment(strings.ToLower(strings.TrimSpace(os.Getenv("ETORO_ENV"))))
	if err != nil {
		log.Fatal(err)
	}
	if env != etoro.EnvDemo {
		log.Fatal("etoro-spike runs on the demo environment only (ETORO_ENV=demo)")
	}
	c, err := etoro.NewClient(etoro.Config{
		PublicKey: os.Getenv("ETORO_PUBLIC_KEY"), PrivateKey: os.Getenv("ETORO_PRIVATE_KEY"), Env: env,
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	now := time.Now().UTC()
	s := &spike{c: c, out: *out, now: now, summary: map[string]any{"captured_at": now.Format(time.RFC3339), "env": string(env)}}

	tradedSyms, benchSyms := split(*traded), split(*bench)
	ids := s.instruments(ctx, append(append([]string{}, tradedSyms...), benchSyms...))
	var allIDs []int64
	for _, sym := range append(tradedSyms, benchSyms...) {
		allIDs = append(allIDs, ids[sym])
	}
	s.eligibility(ctx, allIDs)
	s.rates(ctx, allIDs, ids)
	s.costs(ctx, append(tradedSyms, benchSyms...), ids, *amount)
	for _, sym := range tradedSyms {
		s.daily(ctx, sym, ids[sym])
		s.hourly(ctx, sym, ids[sym])
		s.history(ctx, sym, ids[sym])
	}
	s.account(ctx)
	if *roundtrip {
		s.roundtrip(ctx, broker.Instrument{Venue: etorobroker.Venue, Symbol: *rtSymbol, ID: ids[*rtSymbol]}, *amount)
	}
	s.write("summary.json", s.summary)
	if err := writeSums(*out); err != nil {
		log.Fatal(err)
	}
	log.Printf("evidence written to %s", *out)
}

type spike struct {
	c       *etoro.Client
	out     string
	now     time.Time
	summary map[string]any
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.ToUpper(p))
		}
	}
	return out
}

func (s *spike) must(err error, what string) {
	if err != nil {
		log.Fatalf("%s: %v", what, err)
	}
}

func (s *spike) write(name string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	s.must(err, "marshal "+name)
	s.must(os.WriteFile(filepath.Join(s.out, name), append(b, '\n'), 0o644), "write "+name)
}

func (s *spike) instruments(ctx context.Context, syms []string) map[string]int64 {
	type row struct {
		Symbol       string             `json:"symbol"`
		Resolved     etoro.Instrument   `json:"resolved"`
		LooseMatches []etoro.Instrument `json:"loose_matches"`
	}
	ids := map[string]int64{}
	var rows []row
	for _, sym := range syms {
		in, err := s.c.ResolveSymbol(ctx, sym)
		s.must(err, "resolve "+sym)
		loose, err := s.c.SearchInstruments(ctx, sym)
		s.must(err, "search "+sym)
		ids[sym] = in.InstrumentID
		rows = append(rows, row{sym, in, loose})
		log.Printf("%s -> instrument %d (%s, %s)", sym, in.InstrumentID, in.AssetClass, in.Exchange)
	}
	s.write("instruments.json", rows)
	s.summary["instrument_ids"] = ids
	return ids
}

func (s *spike) eligibility(ctx context.Context, ids []int64) {
	els, err := s.c.Eligibility(ctx, ids)
	s.must(err, "eligibility")
	s.write("eligibility.json", els)
	sum := map[string]any{}
	for _, e := range els {
		lx1, okx1 := e.Config("long", 1)
		sum[e.Symbol] = map[string]any{
			"min_position_exposure_usd": e.MinPositionExposure,
			"max_units_per_order":       e.MaxUnitsPerOrder,
			"long_x1_allowed":           okx1,
			"long_x1_settlement":        lx1.SettlementType,
			"long_x1_min_amount_usd":    lx1.MinPositionAmount,
		}
	}
	s.summary["eligibility"] = sum
}

func (s *spike) rates(ctx context.Context, ids []int64, bySym map[string]int64) {
	rs, err := s.c.GetRates(ctx, ids...)
	s.must(err, "rates")
	type row struct {
		etoro.Rate
		Symbol    string  `json:"symbol"`
		Mid       float64 `json:"mid"`
		SpreadBps float64 `json:"spread_bps"`
	}
	sym := map[int64]string{}
	for k, v := range bySym {
		sym[v] = k
	}
	var rows []row
	sum := map[string]float64{}
	for _, r := range rs {
		bps := r.Spread() / r.Mid() * 1e4
		rows = append(rows, row{r, sym[r.InstrumentID], r.Mid(), bps})
		sum[sym[r.InstrumentID]] = round(bps, 2)
	}
	s.write("rates.json", rows)
	s.summary["quoted_spread_bps_at_capture"] = sum
}

func (s *spike) costs(ctx context.Context, syms []string, ids map[string]int64, amount float64) {
	type row struct {
		Symbol            string             `json:"symbol"`
		Request           etoro.OrderRequest `json:"request"`
		Preview           etoro.CostPreview  `json:"preview"`
		SpreadBps         float64            `json:"spread_cost_bps_of_amount"`
		OvernightBps      float64            `json:"overnight_fee_bps_per_night"`
		OvernightAnnual   float64            `json:"overnight_fee_pct_per_year_365_nights"`
		TransactionFeeUSD float64            `json:"transaction_fee_usd"`
	}
	var rows []row
	sum := map[string]any{}
	for _, sym := range syms {
		req := etoro.MarketBuyByAmount(ids[sym], amount, 1)
		p, err := s.c.Costs(ctx, req)
		s.must(err, "costs "+sym)
		r := row{Symbol: sym, Request: req, Preview: p,
			SpreadBps:         p.Get(etoro.CostMarketSpread) / amount * 1e4,
			OvernightBps:      p.Get(etoro.CostOvernightFee) / amount * 1e4,
			TransactionFeeUSD: p.Get(etoro.CostTransactionFee)}
		r.OvernightAnnual = r.OvernightBps * 365 / 100
		rows = append(rows, r)
		sum[sym] = map[string]float64{"spread_bps": round(r.SpreadBps, 2), "overnight_bps_per_night": round(r.OvernightBps, 2),
			"overnight_pct_per_year": round(r.OvernightAnnual, 2), "transaction_fee_usd": r.TransactionFeeUSD}
	}
	s.write("costs.json", rows)
	s.summary["costs_x1_amount_usd"] = amount
	s.summary["costs_x1"] = sum
}

func (s *spike) daily(ctx context.Context, sym string, id int64) {
	cs, err := s.c.Candles(ctx, id, etoro.OneDay, etoro.MaxCandles)
	s.must(err, "daily candles "+sym)
	rows, rep := etorodaily.ToRows(cs, s.now, etorodaily.Options{})
	name := "daily_" + strings.ToLower(sym) + ".csv"
	s.must(bitsodaily.WriteCSV(filepath.Join(s.out, name), "etoro_"+strings.ToLower(sym), rows), "write "+name)
	all, repAll := etorodaily.ToRows(cs, s.now, etorodaily.Options{KeepNonTradingDays: true})
	var extra []string
	have := map[string]bool{}
	for _, r := range rows {
		have[r.Date] = true
	}
	for _, r := range all {
		if !have[r.Date] {
			extra = append(extra, r.Date+" "+mustDay(r.Date).Weekday().String()[:3])
		}
	}
	fresh := ""
	if err := etorodaily.CheckFresh(rows, s.now); err != nil {
		fresh = err.Error()
	}
	s.write("daily_"+strings.ToLower(sym)+"_report.json", map[string]any{
		"route":                    "GET /api/v1/market-data/instruments/{id}/history/candles/desc/OneDay/1000",
		"bucket":                   "UTC day (fromDate 00:00Z); bar D closes at D+1 00:00Z",
		"trading_days_only":        rep,
		"all_days":                 repAll,
		"dropped_non_trading_days": extra,
		"missing_trading_days":     etorodaily.MissingTradingDays(rows),
		"freshness_error":          fresh,
	})
	s.summary["daily_"+sym] = map[string]any{"kept": rep.Kept, "first": rep.First, "last": rep.Last,
		"weekend_dropped": rep.Weekend, "holiday_dropped": rep.Holiday, "missing_trading_days": rep.MissingTrading}
}

func mustDay(s string) time.Time {
	d, _ := time.Parse("2006-01-02", s)
	return d
}

func (s *spike) hourly(ctx context.Context, sym string, id int64) {
	cs, err := s.c.Candles(ctx, id, etoro.OneHour, etoro.MaxCandles)
	s.must(err, "hourly candles "+sym)
	byWeekday := map[string]int{}
	hoursByWeekday := map[string]map[int]bool{}
	for _, c := range cs {
		wd := c.FromDate.UTC().Weekday().String()[:3]
		byWeekday[wd]++
		if hoursByWeekday[wd] == nil {
			hoursByWeekday[wd] = map[int]bool{}
		}
		hoursByWeekday[wd][c.FromDate.UTC().Hour()] = true
	}
	hours := map[string][]int{}
	for wd, hs := range hoursByWeekday {
		for h := range hs {
			hours[wd] = append(hours[wd], h)
		}
		sort.Ints(hours[wd])
	}
	rep := map[string]any{"route": "OneHour x1000 (live)", "bars": len(cs), "bars_by_utc_weekday": byWeekday, "utc_hours_with_bars": hours}
	if len(cs) > 0 {
		rep["first"], rep["last"] = cs[0].FromDate, cs[len(cs)-1].FromDate
	}
	s.write("hourly_"+strings.ToLower(sym)+"_coverage.json", rep)
	s.summary["hourly_bars_by_weekday_"+sym] = byWeekday
}

func (s *spike) history(ctx context.Context, sym string, id int64) {
	hs, err := s.c.HistoryCandles(ctx, id, "1d", time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{}, 10)
	s.must(err, "history "+sym)
	rep := map[string]any{"route": "GET /api/v1/data/instruments/{id}/candles?interval=1d (DataPlatform)", "bars": len(hs)}
	if len(hs) > 0 {
		last := hs[len(hs)-1].Time
		hoursUTC := map[int]int{}
		for _, h := range hs {
			hoursUTC[h.Time.UTC().Hour()]++
		}
		rep["first"], rep["last"] = hs[0].Time, last
		rep["lag_days_at_capture"] = round(s.now.Sub(last).Hours()/24, 1)
		rep["bar_open_hour_utc_counts"] = hoursUTC
	}
	s.write("history_"+strings.ToLower(sym)+"_coverage.json", rep)
	s.summary["history_"+sym] = rep
}

func (s *spike) account(ctx context.Context) {
	p, err := s.c.GetPortfolio(ctx)
	s.must(err, "portfolio")
	acct := map[string]any{"credit_usd": p.Credit, "equity_usd": p.Equity(), "unrealized_pnl_usd": p.UnrealizedPnL,
		"open_positions": len(p.Positions), "pending_open_orders": len(p.OrdersForOpen), "account_currency_id": p.AccountCurrencyID}
	s.write("account.json", acct)
	s.summary["account"] = acct
}

// roundtrip opens and closes one demo position through the broker adapter
// and checks the idempotency paths: a second Open with the same reference
// resolves to the first position (eToro rejects the reused request id), and a
// second Close of the closed position reports ErrPositionNotOpen.
func (s *spike) roundtrip(ctx context.Context, in broker.Instrument, amount float64) {
	b := etorobroker.New(s.c)
	ref := fmt.Sprintf("spike-%s-%s-roundtrip", s.now.Format("20060102T150405Z"), in.Symbol)
	intent := time.Now().UTC()
	req := broker.OpenRequest{Instrument: in, Side: broker.Long, Amount: amount, Leverage: 1, ClientRef: ref + "-open", IntentAt: intent}
	rep := map[string]any{"instrument": in, "amount_usd": amount, "leverage": 1, "client_ref_open": req.ClientRef,
		"request_id_open": etoro.RequestIDFor(req.ClientRef), "intent_at": intent}
	q, err := b.Quote(ctx, in)
	s.must(err, "roundtrip quote")
	rep["quote_before"] = q
	before, err := b.Positions(ctx)
	s.must(err, "roundtrip positions")

	open, err := b.Open(ctx, req)
	rep["open"], rep["open_error"] = open, errString(err)
	if err != nil || open.OrderID == "" {
		s.write("roundtrip.json", rep)
		log.Fatalf("roundtrip open: %v (%+v)", redact(errString(err)), open)
	}
	var oid int64
	fmt.Sscan(open.OrderID, &oid)
	if info, err := s.c.LookupOrder(ctx, oid); err == nil {
		rep["open_lookup_by_order_id"] = info
	}
	// Evidence: reference lookup does not find API-placed orders.
	_, found, lerr := s.c.LookupOrderByReference(ctx, etoro.RequestIDFor(req.ClientRef))
	rep["lookup_by_reference_found"], rep["lookup_by_reference_error"] = found, errString(lerr)

	again, err := b.Open(ctx, req)
	rep["reopen_same_ref"], rep["reopen_error"] = again, errString(err)
	samePos := err == nil && len(again.PositionIDs) == 1 && len(open.PositionIDs) == 1 && again.PositionIDs[0] == open.PositionIDs[0]
	rep["reopen_resolved_to_same_position"] = samePos
	mid, err := b.Positions(ctx)
	s.must(err, "roundtrip positions after open")
	rep["positions_added_by_open_and_reopen"] = len(mid) - len(before)

	var reclosedNotOpen bool
	if open.Status == broker.StatusExecuted && len(open.PositionIDs) == 1 {
		cl, err := b.Close(ctx, broker.CloseRequest{Instrument: in, PositionID: open.PositionIDs[0], ClientRef: ref + "-close"})
		rep["close"], rep["close_error"] = cl, errString(err)
		var cid int64
		fmt.Sscan(cl.OrderID, &cid)
		if cid > 0 {
			if info, err := s.c.GetCloseOrder(ctx, cid); err == nil {
				rep["close_order_info"] = info
			}
		}
		cl2, err := b.Close(ctx, broker.CloseRequest{Instrument: in, PositionID: open.PositionIDs[0], ClientRef: ref + "-close"})
		reclosedNotOpen = errors.Is(err, broker.ErrPositionNotOpen)
		rep["reclose_same_position"], rep["reclose_error"], rep["reclose_is_position_not_open"] = cl2, errString(err), reclosedNotOpen
	}
	after, err := b.Positions(ctx)
	s.must(err, "roundtrip positions after close")
	rep["positions_after_vs_before"] = len(after) - len(before)
	time.Sleep(3 * time.Second) // history lags a few seconds behind the close
	if hist, err := s.c.TradingHistory(ctx, s.now.AddDate(0, 0, -2), 1, 50); err == nil {
		var mine []etoro.ClosedTrade
		for _, h := range hist {
			for _, pid := range open.PositionIDs {
				if fmt.Sprint(h.PositionID) == pid {
					mine = append(mine, h)
				}
			}
		}
		rep["history_rows_for_roundtrip_position"] = mine
	}
	s.write("roundtrip.json", rep)
	s.summary["roundtrip"] = map[string]any{"open_status": open.Status, "open_avg_price": open.AvgPrice, "open_units": open.Units,
		"open_amount_usd": open.Amount, "open_spread_cost_usd": open.SpreadCost,
		"lookup_by_reference_found":          found,
		"reopen_resolved_to_same_position":   samePos,
		"positions_added_by_open_and_reopen": rep["positions_added_by_open_and_reopen"],
		"reclose_is_position_not_open":       reclosedNotOpen,
		"positions_after_vs_before":          rep["positions_after_vs_before"]}
}

var cidRe = regexp.MustCompile(`(?i)\bCID\s*[:=]?\s*\d+`)

// redact removes eToro account ids (CID) that appear inside error texts.
func redact(s string) string { return cidRe.ReplaceAllString(s, "CID <redacted>") }

func errString(err error) string {
	if err == nil {
		return ""
	}
	return redact(err.Error())
}

func round(v float64, n int) float64 {
	p := 1.0
	for i := 0; i < n; i++ {
		p *= 10
	}
	return float64(int64(v*p+0.5*sign(v))) / p
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// writeSums writes SHA256SUMS over every other file in dir.
func writeSums(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var lines []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == "SHA256SUMS" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		h := sha256.Sum256(b)
		lines = append(lines, hex.EncodeToString(h[:])+"  "+e.Name())
	}
	sort.Strings(lines)
	return os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
