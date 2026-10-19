package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoroledger"
)

// uiLine builds a console ledger line (source ui) for the positions tests:
// an open, a protect or a close of position id on book, recorded at at.
func uiLine(book, action, id string, at time.Time, mut func(d *etoroledger.Demo)) etoroledger.Record {
	in := etoroledger.Instrument{Symbol: strings.ToUpper(book), ID: 28}
	if book == "spx500" {
		in.ID = 27
	}
	day := at.Format("2006-01-02")
	pos := etoroledger.Position{State: "long", PositionID: id, Units: 0.0335, Amount: 1000, OpenRate: 29850.5, OpenedAt: at.Format(time.RFC3339), StopLossRate: 29253.49, StopType: broker.StopFixed}
	d := &etoroledger.Demo{Env: "demo", Target: "long", Action: action, PositionAfter: pos,
		Order: &etoroledger.Order{Kind: action, ClientRef: "ui-" + book + "-" + day + "-" + action + "-abc123", Status: "executed", PositionIDs: []string{id},
			AvgPrice: 29850.5, Units: 0.0335, Amount: 1000, Finished: at, FillVsMidBps: 1.7},
		Costs: &etoroledger.Costs{Spread: 0.04, OvernightFee: 0.26}}
	switch action {
	case etoroledger.ActionOpen:
		d.PositionBefore = etoroledger.Flat()
	case etoroledger.ActionProtect:
		d.PositionBefore = pos
		d.Order.AvgPrice, d.Order.FillVsMidBps = 0, 0
	case etoroledger.ActionClose:
		d.PositionBefore = pos
		d.PositionAfter = etoroledger.Flat()
		d.Order.AvgPrice = 30100.25
		d.FinancingToDate = 0.52
	}
	if mut != nil {
		mut(d)
	}
	return etoroledger.Record{Schema: etoroledger.Schema, RecordedAt: at.Format(time.RFC3339), CodeVersion: "test", Mode: etoroledger.ModeDemo, Venue: "etoro",
		Book: book, Instrument: in, Decision: etoroledger.Decision{BarDate: day, FillDate: day, Close: 29868.7, Signal: etoroledger.SignalManual, PrevSignal: "flat", Action: action},
		Demo: d, Source: etoroledger.SourceUI, Operator: &etoroledger.Operator{By: "diego", Reason: "testing manual order placement"}}
}

func appendLedger(t *testing.T, dir string, recs ...etoroledger.Record) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, "ledger.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, r := range recs {
		b, _ := json.Marshal(r)
		if _, err := f.Write(append(b, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

func findTrade(ts []EtoroTrade, id string) *EtoroTrade {
	for i := range ts {
		if ts[i].PositionID == id {
			return &ts[i]
		}
	}
	return nil
}

// The ledger alone pairs the run's open and close of NSDQ100 into a closed
// round trip (gross from the fills, closed by the rule), keeps SPX500 open,
// and follows a console open -> protect -> close with the operator.
func TestBuildEtoroTradesPairsLedgerLines(t *testing.T) {
	recs, err := etoroledger.ReadFile(filepath.Join(etoroTestdata, "ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	d19 := time.Date(2026, 10, 19, 14, 0, 0, 0, time.UTC)
	recs = append(recs,
		uiLine("nsdq100", etoroledger.ActionOpen, "900", d19, nil),
		uiLine("nsdq100", etoroledger.ActionProtect, "900", d19.Add(20*time.Minute), func(d *etoroledger.Demo) {
			d.PositionAfter.StopLossRate, d.PositionAfter.TakeProfitRate = 29400, 31000
			d.Protection = &etoroledger.Protection{Profile: "custom", StopLossBefore: 29253.49}
		}),
		uiLine("nsdq100", etoroledger.ActionClose, "900", d19.Add(2*time.Hour), nil),
	)
	by := map[string][]etoroledger.Record{}
	for _, r := range recs {
		by[r.Book] = append(by[r.Book], r)
	}
	ts := buildEtoroTrades(by, tradeNow)
	if len(ts) != 3 {
		t.Fatalf("trades: %d %+v", len(ts), ts)
	}
	// Newest open first.
	if ts[0].PositionID != "900" || ts[1].PositionID != "3616683295" || ts[2].PositionID != "3616683146" {
		t.Fatalf("order: %s %s %s", ts[0].PositionID, ts[1].PositionID, ts[2].PositionID)
	}

	rule := findTrade(ts, "3616683146")
	if rule.Status != TradeStatusClosed || rule.Source != TradeSourceRule || !rule.RecordedOpen || !rule.RecordedClose || rule.ClosedBy != ClosedByRule {
		t.Fatalf("rule round trip: %+v", rule)
	}
	if rule.OpenRate != 30726.899999999998 || rule.CloseRate != 29770.5 || rule.Units != 0.035799 || rule.OpenBarDate != "2026-10-09" || rule.CloseBarDate != "2026-10-15" {
		t.Fatalf("rule rates: %+v", rule)
	}
	if rule.GrossPnL == nil || !near(*rule.GrossPnL, 0.035799*(29770.5-30726.9), 1e-6) || rule.ReturnPct == nil || !near(*rule.ReturnPct, *rule.GrossPnL/1099.99, 1e-9) {
		t.Fatalf("rule gross: %+v", rule)
	}
	if rule.Net != nil || rule.VenueConfirmed || rule.HeldDays != 4 || !strings.Contains(rule.CloseReason, "the rule said flat at the 2026-10-15 close") {
		t.Fatalf("rule venue / held / reason: %+v", rule)
	}

	spx := findTrade(ts, "3616683295")
	if spx.Status != TradeStatusOpen || spx.OpenRate != 7816.069999999999 || spx.OpenCosts == nil || spx.OpenCosts.OvernightFee != 0.25 || spx.HeldDays != 7 {
		t.Fatalf("spx: %+v", spx)
	}

	man := findTrade(ts, "900")
	if man.Source != TradeSourceManual || man.Operator == nil || man.Operator.By != "diego" || man.Status != TradeStatusClosed || man.ClosedBy != ClosedByOperator {
		t.Fatalf("manual: %+v", man)
	}
	if man.StopLossRate != 29400 || man.TakeProfitRate != 31000 || man.ProtectEdits != 1 || man.CloseReason != "testing manual order placement" || man.FinancingToDate != 0.52 {
		t.Fatalf("manual protection / close: %+v", man)
	}
	if man.GrossPnL == nil || !near(*man.GrossPnL, 0.0335*(30100.25-29850.5), 1e-6) || man.OpenFillVsMidBps != 1.7 {
		t.Fatalf("manual gross: %+v", man)
	}

	sum := summarize(ts)
	if sum.Open != 1 || sum.Closed != 2 || sum.OpenExposure != 1099.99 || !near(sum.RealizedGross, *rule.GrossPnL+*man.GrossPnL, 1e-6) || sum.RealizedConfirmed != 0 {
		t.Fatalf("summary: %+v", sum)
	}
}

// A position seen held before the ledger recorded its open becomes an
// unrecorded row; a venue_close line closes it with eToro's figures.
func TestBuildEtoroTradesUnrecordedAndVenueClose(t *testing.T) {
	d := time.Date(2026, 10, 20, 13, 35, 0, 0, time.UTC)
	held := etoroledger.Position{State: "long", PositionID: "555", Units: 0.03, Amount: 900, OpenRate: 30000, OpenedAt: "2026-10-01T13:35:00Z", StopLossRate: 29000}
	none := etoroledger.Record{Schema: etoroledger.Schema, RecordedAt: d.Format(time.RFC3339), Mode: etoroledger.ModeDemo, Book: "nsdq100", Instrument: etoroledger.Instrument{Symbol: "NSDQ100", ID: 28},
		Decision: etoroledger.Decision{BarDate: "2026-10-19", Signal: "long", Action: "hold"},
		Demo:     &etoroledger.Demo{Env: "demo", Action: etoroledger.ActionNone, PositionBefore: held, PositionAfter: held, FinancingToDate: 3.2}}
	vc := etoroledger.Record{Schema: etoroledger.Schema, RecordedAt: d.Add(24 * time.Hour).Format(time.RFC3339), Mode: etoroledger.ModeDemo, Book: "nsdq100", Instrument: etoroledger.Instrument{Symbol: "NSDQ100", ID: 28},
		Decision: etoroledger.Decision{BarDate: "2026-10-20", Signal: "long", Action: "hold"},
		Demo: &etoroledger.Demo{Env: "demo", Action: etoroledger.ActionVenueClose, PositionBefore: held, PositionAfter: etoroledger.Flat(),
			Order:      &etoroledger.Order{Kind: etoroledger.ActionVenueClose, Status: "executed", AvgPrice: 28995, Units: 0.03, Finished: d.Add(20 * time.Hour)},
			VenueClose: &etoroledger.VenueClose{Kind: etoroledger.VenueCloseStoppedOut, StopLossRate: 29000, CloseRate: 28995, ClosedAt: "2026-10-20T15:10:00Z", NetProfit: -30.15, Fees: 3.4}}}
	ts := buildEtoroTrades(map[string][]etoroledger.Record{"nsdq100": {none, vc}}, d.Add(48*time.Hour))
	if len(ts) != 1 {
		t.Fatalf("trades: %+v", ts)
	}
	x := ts[0]
	if x.RecordedOpen || x.Source != TradeSourceUnknown || x.Status != TradeStatusClosed || x.ClosedBy != ClosedByVenue || x.CloseKind != etoroledger.VenueCloseStoppedOut {
		t.Fatalf("venue close: %+v", x)
	}
	if !x.VenueConfirmed || x.Net == nil || !near(*x.Net, -33.55, 1e-9) || x.CloseRate != 28995 || x.ClosedAt != "2026-10-20T15:10:00Z" || x.GrossPnL == nil || !near(*x.GrossPnL, 0.03*(28995-30000), 1e-6) {
		t.Fatalf("venue figures: %+v", x)
	}
	if x.HeldDays != 19 || len(x.Notes) == 0 || !strings.Contains(x.Notes[0], "held before the ledger recorded its open") {
		t.Fatalf("held / notes: %+v", x)
	}
}

// With the demo broker the view adds the account: unrealised P&L and fees
// on the open row, eToro's net on the closed one, the account totals; the
// live layer is cached; a book filter keeps the live rows; trading off
// means the ledger's figures with a reason.
func TestEtoroPositionsLiveLayer(t *testing.T) {
	h := tradeServer(t, tradeNow)
	h.fb.positions[0].UnrealizedPnL = -10.9
	h.fb.positions[0].StopLoss = 7500 // the account holds a stop the ledger never recorded
	h.fb.closed = []broker.ClosedPosition{{ID: "3616683146", Instrument: broker.Instrument{ID: 28}, Side: broker.Long, Units: 0.035799,
		OpenRate: 30726.9, CloseRate: 29770.5, OpenedAt: tradeNow.Add(-7 * 24 * time.Hour), ClosedAt: tradeNow.Add(-3 * 24 * time.Hour), Amount: 1099.99, NetProfit: -34.24, Fees: 1.1}}

	resp := get[EtoroPositionsResponse](t, h.ts, "/api/ui/etoro/positions?ledger=etoro-demo", 200)
	if !resp.Live || resp.Cached || resp.Account == nil || resp.Account.Cash != 97793.95 || len(resp.Trades) != 2 || len(resp.Warnings) != 0 {
		t.Fatalf("resp: live=%v cached=%v account=%+v trades=%d warnings=%v", resp.Live, resp.Cached, resp.Account, len(resp.Trades), resp.Warnings)
	}
	spx := findTrade(resp.Trades, "3616683295")
	if spx.AccountHolds == nil || !*spx.AccountHolds || spx.MarkSource != "account" || spx.UnrealizedPnL != -10.9 || spx.FeesToDate != 0.75 {
		t.Fatalf("spx live: %+v", spx)
	}
	if !near(spx.MarkRate, 7816.07-10.9/0.140735, 1e-6) || !near(spx.UnrealizedPct, -10.9/1099.99, 1e-9) {
		t.Fatalf("spx mark: %+v", spx)
	}
	if !spx.ProtectionDrift || spx.AccountStopLossRate != 7500 || len(spx.Notes) != 1 || !strings.Contains(spx.Notes[0], "the account holds stop 7500.00") {
		t.Fatalf("spx drift: %+v", spx)
	}
	nd := findTrade(resp.Trades, "3616683146")
	if !nd.VenueConfirmed || nd.NetProfit == nil || *nd.NetProfit != -34.24 || nd.Fees == nil || *nd.Fees != 1.1 || nd.Net == nil || !near(*nd.Net, -35.34, 1e-9) || nd.VenueCloseRate != 29770.5 || len(nd.Notes) != 0 {
		t.Fatalf("nsdq venue: %+v", nd)
	}
	if resp.Summary.Open != 1 || resp.Summary.Closed != 1 || resp.Summary.UnrealizedPnL != -10.9 || resp.Summary.FeesToDate != 0.75 || resp.Summary.RealizedConfirmed != 1 || !near(resp.Summary.RealizedNet, -35.34, 1e-9) {
		t.Fatalf("summary: %+v", resp.Summary)
	}
	if h.fb.histCalls != 1 {
		t.Fatalf("history reads: %d", h.fb.histCalls)
	}

	// Cached within the TTL, including a book filter; refreshed after it.
	h.now = h.now.Add(5 * time.Second)
	again := get[EtoroPositionsResponse](t, h.ts, "/api/ui/etoro/positions?ledger=etoro-demo&book=spx500", 200)
	if !again.Cached || again.CachedAt != tradeNow.Format(time.RFC3339) || len(again.Trades) != 1 || again.Trades[0].UnrealizedPnL != -10.9 || again.Summary.Closed != 0 {
		t.Fatalf("cached: %+v", again)
	}
	if h.fb.histCalls != 1 {
		t.Fatalf("history reads after cache hit: %d", h.fb.histCalls)
	}
	h.now = h.now.Add(positionsCacheTTL)
	h.fb.positions[0].UnrealizedPnL = 4.2
	fresh := get[EtoroPositionsResponse](t, h.ts, "/api/ui/etoro/positions?ledger=etoro-demo", 200)
	if fresh.Cached || findTrade(fresh.Trades, "3616683295").UnrealizedPnL != 4.2 || h.fb.histCalls != 2 {
		t.Fatalf("refreshed: cached=%v calls=%d", fresh.Cached, h.fb.histCalls)
	}

	// Trading off: the ledger's rows, no account, a reason.
	h.srv.Trading, h.srv.TradingOffReason = nil, "-etoro-trading not set"
	off := get[EtoroPositionsResponse](t, h.ts, "/api/ui/etoro/positions?ledger=etoro-demo", 200)
	if off.Live || off.Account != nil || !strings.Contains(off.LiveReason, "-etoro-trading not set") || len(off.Trades) != 2 || findTrade(off.Trades, "3616683295").AccountHolds != nil {
		t.Fatalf("off: %+v", off)
	}
	nd = findTrade(off.Trades, "3616683146")
	if nd.VenueConfirmed || nd.GrossPnL == nil {
		t.Fatalf("off nsdq: %+v", nd)
	}

	// Not an eToro ledger.
	get[map[string]any](t, h.ts, "/api/ui/etoro/positions?ledger=stage", 400)
}

// A ledger-open position the account no longer holds: found in the history
// at its stop it is a venue close (unrecorded); not found it is unknown; a
// history failure is a warning and the row stays open.
func TestEtoroPositionsClosedOutsideTheLedger(t *testing.T) {
	h := tradeServer(t, tradeNow)
	d := tradeNow.Add(-2 * time.Hour)
	appendLedger(t, h.dir,
		uiLine("nsdq100", etoroledger.ActionOpen, "901", d, nil),
	)
	h.fb.closed = []broker.ClosedPosition{{ID: "901", Instrument: broker.Instrument{ID: 28}, Side: broker.Long, Units: 0.0335,
		OpenRate: 29850.5, CloseRate: 29260.1, OpenedAt: d, ClosedAt: d.Add(50 * time.Minute), Amount: 1000, NetProfit: -19.78, Fees: 0, StopLoss: 29253.49}}

	resp := get[EtoroPositionsResponse](t, h.ts, "/api/ui/etoro/positions?ledger=etoro-demo", 200)
	x := findTrade(resp.Trades, "901")
	if x == nil || x.Status != TradeStatusClosed || x.RecordedClose || x.ClosedBy != ClosedByVenue || x.CloseKind != etoroledger.VenueCloseStoppedOut {
		t.Fatalf("stopped out: %+v", x)
	}
	if x.AccountHolds == nil || *x.AccountHolds || x.CloseRate != 29260.1 || x.Net == nil || *x.Net != -19.78 || x.GrossPnL == nil || !near(*x.GrossPnL, 0.0335*(29260.1-29850.5), 1e-6) {
		t.Fatalf("stopped out figures: %+v", x)
	}
	if len(x.Notes) != 1 || !strings.Contains(x.Notes[0], "not in the ledger") || !strings.Contains(x.CloseReason, "stop loss") {
		t.Fatalf("stopped out notes: %+v", x)
	}
	if resp.Summary.Closed != 2 || resp.Summary.Open != 1 {
		t.Fatalf("summary: %+v", resp.Summary)
	}

	// Closed away from the stop: outside the ledger.
	h.fb.closed[0].CloseRate = 29700
	h.now = h.now.Add(positionsCacheTTL)
	resp = get[EtoroPositionsResponse](t, h.ts, "/api/ui/etoro/positions?ledger=etoro-demo", 200)
	if x = findTrade(resp.Trades, "901"); x.ClosedBy != ClosedByOutside || x.CloseKind != etoroledger.VenueCloseOther {
		t.Fatalf("outside: %+v", x)
	}

	// Not in the history yet.
	h.fb.closed = nil
	h.now = h.now.Add(positionsCacheTTL)
	resp = get[EtoroPositionsResponse](t, h.ts, "/api/ui/etoro/positions?ledger=etoro-demo", 200)
	if x = findTrade(resp.Trades, "901"); x.Status != TradeStatusUnknown || len(x.Notes) != 1 || !strings.Contains(x.Notes[0], "does not list it yet") || resp.Summary.Unknown != 1 {
		t.Fatalf("unknown: %+v %+v", x, resp.Summary)
	}

	// History down: a warning, the row stays open and the closed rows
	// keep the ledger's figures.
	h.fb.histErr = errors.New("429 too many requests")
	h.now = h.now.Add(positionsCacheTTL)
	resp = get[EtoroPositionsResponse](t, h.ts, "/api/ui/etoro/positions?ledger=etoro-demo", 200)
	if x = findTrade(resp.Trades, "901"); x.Status != TradeStatusOpen || !resp.Live || len(resp.Warnings) != 1 || !strings.Contains(resp.Warnings[0], "trading history: 429") {
		t.Fatalf("history down: %+v %v", x, resp.Warnings)
	}

	// Positions down: a warning, the open rows carry no account figures,
	// the history is not consulted for them.
	h.fb.histErr, h.fb.posErr = nil, errors.New("503")
	h.now = h.now.Add(positionsCacheTTL)
	resp = get[EtoroPositionsResponse](t, h.ts, "/api/ui/etoro/positions?ledger=etoro-demo", 200)
	if x = findTrade(resp.Trades, "901"); x.AccountHolds != nil || x.Status != TradeStatusOpen || len(resp.Warnings) != 1 || !strings.Contains(resp.Warnings[0], "positions: 503") {
		t.Fatalf("positions down: %+v %v", x, resp.Warnings)
	}
}
