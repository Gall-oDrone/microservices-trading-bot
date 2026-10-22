package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
)

// venue is the read-only slice of the eToro client the observer uses. The
// interface names every call it may make; nothing here can send an order.
type venue interface {
	GetPortfolio(ctx context.Context) (*etoro.Portfolio, error)
	TradingHistory(ctx context.Context, minDate time.Time, page, pageSize int) ([]etoro.ClosedTrade, error)
	Costs(ctx context.Context, req etoro.OrderRequest) (etoro.CostPreview, error)
	GetRates(ctx context.Context, instrumentIDs ...int64) ([]etoro.Rate, error)
	Eligibility(ctx context.Context, instrumentIDs []int64) ([]etoro.Eligibility, error)
}

var _ venue = (*etoro.Client)(nil)

// input is what one observation needs besides the venue.
type input struct {
	recs      []etoroledger.Record
	prev      *etoroledger.Observation
	now       time.Time
	env       string
	ledger    string
	sinceDays int
	// The preview when no position is held: the canary's planned order.
	previewInstrument int64
	previewAmount     float64
	previewLeverage   int
}

const historyPage = 200

// observe performs the reads and builds the line. A failed read is an
// entry in Errors and the rest of the line is still filled; the caller
// writes the line either way and exits 1 on errors.
func observe(ctx context.Context, v venue, in input) etoroledger.Observation {
	o := etoroledger.Observation{Schema: etoroledger.ObservationSchema, At: in.now.UTC().Format(time.RFC3339),
		NYDate: in.now.In(mktcal.NewYork).Format("2006-01-02"), Env: in.env, Ledger: in.ledger,
		Positions: []etoroledger.ObservedPosition{}, HistoryNew: []etoroledger.ObservedClosed{}}
	if in.prev != nil {
		o.PreviousAt = in.prev.At
	}
	known := ledgerPositions(in.recs)
	symbols := ledgerSymbols(in.recs)

	// 1. The account and the open positions.
	p, err := v.GetPortfolio(ctx)
	if err != nil {
		o.Errors = append(o.Errors, "portfolio: "+err.Error())
	} else {
		o.Account = etoroledger.Account{Currency: "USD", Cash: p.Credit, Equity: p.Equity()}
		for _, pos := range p.Positions {
			op := etoroledger.ObservedPosition{PositionID: strconv.FormatInt(pos.PositionID, 10), InstrumentID: pos.InstrumentID, Symbol: symbols[pos.InstrumentID],
				Units: pos.Units, Amount: pos.Amount, Leverage: pos.Leverage, OpenRate: pos.OpenRate,
				Exposure: pos.UnrealizedPnL.ExposureInAccountCurrency, Mark: pos.UnrealizedPnL.CloseRate, Unrealized: pos.UnrealizedPnL.PnL,
				FeesToDate: pos.TotalFees, InLedger: known[strconv.FormatInt(pos.PositionID, 10)]}
			if t := pos.OpenedAt(); !t.IsZero() {
				op.OpenedAt = t.UTC().Format(time.RFC3339)
				op.Nights = etoroledger.NightsHeld(t, in.now, mktcal.NewYork)
			}
			if !pos.IsNoStopLoss && pos.StopLossRate > 0.01 {
				op.StopLossRate = pos.StopLossRate
			}
			if !pos.IsNoTakeProfit && pos.TakeProfitRate > 0 {
				op.TakeProfitRate = pos.TakeProfitRate
			}
			o.Account.Exposure += op.Exposure
			// The cost preview at the held size and leverage: what eToro says
			// one night and the weekend cost now, next to what it charged.
			lev := pos.Leverage
			if lev < 1 {
				lev = 1
			}
			if c, err := v.Costs(ctx, etoro.MarketBuyByAmount(pos.InstrumentID, pos.Amount, lev)); err != nil {
				o.Errors = append(o.Errors, fmt.Sprintf("costs for position %s: %v", op.PositionID, err))
			} else {
				op.Preview = costsOf(c)
			}
			o.Positions = append(o.Positions, op)
		}
		if len(p.Positions) == 0 && in.previewInstrument > 0 && in.previewAmount > 0 {
			// Nothing held: preview the canary's planned order so the series
			// has the venue's quote of the night fee before day 0 too.
			if c, err := v.Costs(ctx, etoro.MarketBuyByAmount(in.previewInstrument, in.previewAmount, in.previewLeverage)); err != nil {
				o.Errors = append(o.Errors, fmt.Sprintf("costs preview %d x%d %.0f USD: %v", in.previewInstrument, in.previewLeverage, in.previewAmount, err))
			} else {
				pc := costsOf(c)
				o.Notes = append(o.Notes, fmt.Sprintf("no position held; preview for instrument %d, %.0f USD at x%d: overnight %.4f, weekend %.4f, spread %.4f",
					in.previewInstrument, in.previewAmount, in.previewLeverage, pc.OvernightFee, pc.WeekendFee, pc.Spread))
			}
		}
	}
	sort.Slice(o.Positions, func(i, j int) bool { return o.Positions[i].PositionID < o.Positions[j].PositionID })

	// 2. The trading history since the canary's first open (or sinceDays).
	since := in.now.AddDate(0, 0, -in.sinceDays)
	if t, ok := firstOpen(in.recs); ok && t.Before(since) {
		since = t.AddDate(0, 0, -1)
	} else if ok {
		since = t.AddDate(0, 0, -1)
	}
	seen := map[string]bool{}
	if in.prev != nil {
		for _, id := range in.prev.HistoryIDs {
			seen[id] = true
		}
	}
	rows, err := readHistory(ctx, v, since)
	if err != nil {
		o.Errors = append(o.Errors, "history: "+err.Error())
		o.HistoryIDs = in.prevHistoryIDs()
	} else {
		ids := map[string]bool{}
		for _, r := range rows {
			id := strconv.FormatInt(r.PositionID, 10)
			ids[id] = true
			if seen[id] {
				continue
			}
			oc := etoroledger.ObservedClosed{PositionID: id, InstrumentID: r.InstrumentID, Units: r.Units, Leverage: r.Leverage, OpenRate: r.OpenRate, CloseRate: r.CloseRate,
				NetProfit: r.NetProfit, Fees: r.Fees, SeenAt: o.At}
			if t := parseTime(r.OpenTimestamp); !t.IsZero() {
				oc.OpenedAt = t.UTC().Format(time.RFC3339)
			}
			if t := parseTime(r.CloseTimestamp); !t.IsZero() {
				oc.ClosedAt = t.UTC().Format(time.RFC3339)
			}
			if r.StopLossRate > 0.01 {
				oc.StopLossRate = r.StopLossRate
			}
			if r.TakeProfitRate > 0 {
				oc.TakeProfitRate = r.TakeProfitRate
			}
			o.HistoryNew = append(o.HistoryNew, oc)
		}
		sort.Slice(o.HistoryNew, func(i, j int) bool { return o.HistoryNew[i].ClosedAt < o.HistoryNew[j].ClosedAt })
		for id := range seen {
			ids[id] = true
		}
		for id := range ids {
			o.HistoryIDs = append(o.HistoryIDs, id)
		}
		sort.Strings(o.HistoryIDs)
	}

	// 3. The deltas against the previous observation.
	applyDeltas(in.prev, &o)
	return o
}

// prevHistoryIDs keeps the known history ids when the history read failed,
// so the next successful observation still tells new rows apart.
func (in input) prevHistoryIDs() []string {
	if in.prev == nil {
		return nil
	}
	return append([]string(nil), in.prev.HistoryIDs...)
}

// applyDeltas fills FeesDelta per position from the previous observation
// (the night's charge) and notes a position that appeared or vanished.
func applyDeltas(prev *etoroledger.Observation, cur *etoroledger.Observation) {
	if prev == nil {
		return
	}
	before := map[string]etoroledger.ObservedPosition{}
	for _, p := range prev.Positions {
		before[p.PositionID] = p
	}
	now := map[string]bool{}
	for i := range cur.Positions {
		p := &cur.Positions[i]
		now[p.PositionID] = true
		if b, ok := before[p.PositionID]; ok {
			d := p.FeesToDate - b.FeesToDate
			p.FeesDelta = &d
			if p.Nights == b.Nights && d != 0 {
				cur.Notes = append(cur.Notes, fmt.Sprintf("position %s: fees moved by %.4f without a new night (%d nights at both observations)", p.PositionID, d, p.Nights))
			}
		} else {
			cur.Notes = append(cur.Notes, fmt.Sprintf("position %s appeared since %s", p.PositionID, prev.At))
		}
	}
	for id := range before {
		if !now[id] {
			cur.Notes = append(cur.Notes, fmt.Sprintf("position %s held at %s is gone: look for it in history_new (now or at a later observation)", id, prev.At))
		}
	}
	sort.Strings(cur.Notes)
}

func costsOf(c etoro.CostPreview) *etoroledger.Costs {
	return &etoroledger.Costs{Spread: c.Get(etoro.CostMarketSpread), Markup: c.Get(etoro.CostMarkup), TransactionFee: c.Get(etoro.CostTransactionFee),
		OvernightFee: c.Get(etoro.CostOvernightFee), WeekendFee: c.Get(etoro.CostOverWeekendFee)}
}

// readHistory pages through the trading history since a date.
func readHistory(ctx context.Context, v venue, since time.Time) ([]etoro.ClosedTrade, error) {
	var all []etoro.ClosedTrade
	for page := 1; page <= 50; page++ {
		rows, err := v.TradingHistory(ctx, since, page, historyPage)
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
		if len(rows) < historyPage {
			break
		}
	}
	return all, nil
}

// ledgerPositions is every position id the ledger ever recorded as held.
func ledgerPositions(recs []etoroledger.Record) map[string]bool {
	out := map[string]bool{}
	for _, r := range recs {
		if r.Demo == nil {
			continue
		}
		for _, p := range []etoroledger.Position{r.Demo.PositionBefore, r.Demo.PositionAfter} {
			if p.PositionID != "" {
				out[p.PositionID] = true
			}
		}
		if r.Demo.Order != nil {
			for _, id := range r.Demo.Order.PositionIDs {
				out[id] = true
			}
		}
	}
	return out
}

// ledgerSymbols maps the ledger's instrument ids to their symbols.
func ledgerSymbols(recs []etoroledger.Record) map[int64]string {
	out := map[int64]string{28: "NSDQ100", 27: "SPX500", 3006: "QQQ", 3000: "SPY"}
	for _, r := range recs {
		if r.Instrument.ID > 0 && r.Instrument.Symbol != "" {
			out[r.Instrument.ID] = r.Instrument.Symbol
		}
	}
	return out
}

// firstOpen is when the ledger's first executed open was placed.
func firstOpen(recs []etoroledger.Record) (time.Time, bool) {
	var first time.Time
	for _, r := range recs {
		if r.Demo == nil || r.Demo.Order == nil || r.Demo.Order.Kind != etoroledger.ActionOpen {
			continue
		}
		t := r.Demo.Order.IntentAt
		if t.IsZero() {
			t = parseTime(r.RecordedAt)
		}
		if !t.IsZero() && (first.IsZero() || t.Before(first)) {
			first = t
		}
	}
	return first, !first.IsZero()
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// captureDay0 writes the read-only captures of plan §8.1 like etoro-spike
// does: eligibility, the cost preview at the canary's size, the rates and
// the account, each as JSON, with SHA256SUMS. With dry it prints what it
// would write.
func captureDay0(ctx context.Context, v venue, dir string, instrument int64, amount float64, leverage int, dry bool) []string {
	var errs []string
	files := map[string]any{}
	if els, err := v.Eligibility(ctx, []int64{instrument}); err != nil {
		errs = append(errs, "capture eligibility: "+err.Error())
	} else {
		files["eligibility.json"] = els
	}
	req := etoro.MarketBuyByAmount(instrument, amount, leverage)
	if c, err := v.Costs(ctx, req); err != nil {
		errs = append(errs, "capture costs: "+err.Error())
	} else {
		files["costs.json"] = map[string]any{"request": req, "preview": c, "exposure_usd": amount * float64(leverage),
			"overnight_fee_bps_of_exposure": c.Get(etoro.CostOvernightFee) / (amount * float64(leverage)) * 1e4}
	}
	if rs, err := v.GetRates(ctx, instrument); err != nil {
		errs = append(errs, "capture rates: "+err.Error())
	} else {
		files["rates.json"] = rs
	}
	if p, err := v.GetPortfolio(ctx); err != nil {
		errs = append(errs, "capture account: "+err.Error())
	} else {
		files["account.json"] = map[string]any{"credit_usd": p.Credit, "equity_usd": p.Equity(), "unrealized_pnl_usd": p.UnrealizedPnL,
			"open_positions": len(p.Positions), "pending_open_orders": len(p.OrdersForOpen), "captured_at": time.Now().UTC().Format(time.RFC3339)}
	}
	if dry {
		names := make([]string, 0, len(files))
		for n := range files {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Fprintf(os.Stderr, "etoro-canary-observe: -dry: would write %s to %s\n", strings.Join(names, ", "), dir)
		return errs
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return append(errs, "capture: "+err.Error())
	}
	var sums []string
	for name, v := range files {
		b, _ := json.MarshalIndent(v, "", "  ")
		b = append(b, '\n')
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			errs = append(errs, "capture "+name+": "+err.Error())
			continue
		}
		h := sha256.Sum256(b)
		sums = append(sums, hex.EncodeToString(h[:])+"  "+name)
	}
	sort.Strings(sums)
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"), 0o644); err != nil {
		errs = append(errs, "capture SHA256SUMS: "+err.Error())
	}
	fmt.Fprintf(os.Stderr, "etoro-canary-observe: wrote %d capture file(s) to %s\n", len(sums), dir)
	return errs
}
