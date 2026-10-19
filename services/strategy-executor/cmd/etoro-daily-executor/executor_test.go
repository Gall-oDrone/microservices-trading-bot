package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etorodaily"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/shared/pkg/risk"
)

// fakeBroker models the eToro adapter's contract: Open is idempotent per
// ClientRef (a repeat returns the first order), positions appear at once.
type fakeBroker struct {
	positions []broker.Position
	closed    []broker.ClosedPosition
	byRef     map[string]broker.Order
	opens     []string // ClientRefs sent to Open
	closes    []string
	nextID    int
	bid, ask  float64
	equity    float64
	// openErr is returned once by the next Open; if landOnErr the order is
	// placed anyway (a timeout that hid an accepted order).
	openErr   error
	landOnErr bool
	closeErr  error
	// protectErr is returned by the next SetProtection; protects logs the
	// edits received.
	protectErr error
	protects   []broker.ProtectionRequest
}

func newFakeBroker() *fakeBroker {
	return &fakeBroker{byRef: map[string]broker.Order{}, nextID: 3616261800, bid: 129.9, ask: 130.1, equity: 99_993.93}
}

func (f *fakeBroker) Venue() string { return "etoro" }
func (f *fakeBroker) Quote(ctx context.Context, in broker.Instrument) (broker.Quote, error) {
	return broker.Quote{Instrument: in, Bid: f.bid, Ask: f.ask, At: time.Date(2026, 10, 12, 13, 40, 0, 0, time.UTC)}, nil
}

func (f *fakeBroker) place(req broker.OpenRequest) broker.Order {
	f.nextID++
	id := strconv.Itoa(f.nextID)
	units := req.Amount / f.ask
	o := broker.Order{OrderID: "o" + id, ClientRef: req.ClientRef, Status: broker.StatusExecuted, PositionIDs: []string{id},
		AvgPrice: f.ask, Units: units, Amount: req.Amount, SpreadCost: 0.85, At: time.Date(2026, 10, 12, 13, 40, 2, 0, time.UTC)}
	f.byRef[req.ClientRef] = o
	f.positions = append(f.positions, broker.Position{ID: id, Instrument: req.Instrument, Side: broker.Long, Units: units,
		OpenRate: f.ask, Amount: req.Amount, Leverage: 1, Exposure: req.Amount, Fees: 0, StopLoss: req.StopLoss, TakeProfit: req.TakeProfit})
	return o
}

func (f *fakeBroker) Open(ctx context.Context, req broker.OpenRequest) (broker.Order, error) {
	f.opens = append(f.opens, req.ClientRef)
	if o, ok := f.byRef[req.ClientRef]; ok {
		return o, nil // duplicate reference resolved to the first order
	}
	if err := f.openErr; err != nil {
		f.openErr = nil
		if f.landOnErr {
			f.place(req)
		}
		return broker.Order{ClientRef: req.ClientRef, Status: broker.StatusUnknown}, err
	}
	return f.place(req), nil
}

func (f *fakeBroker) Close(ctx context.Context, req broker.CloseRequest) (broker.Order, error) {
	f.closes = append(f.closes, req.ClientRef)
	if f.closeErr != nil {
		return broker.Order{ClientRef: req.ClientRef, Status: broker.StatusRejected}, f.closeErr
	}
	for i, p := range f.positions {
		if p.ID == req.PositionID {
			f.positions = append(f.positions[:i], f.positions[i+1:]...)
			f.closed = append(f.closed, broker.ClosedPosition{ID: p.ID, Instrument: p.Instrument, Side: p.Side, Units: p.Units,
				OpenRate: p.OpenRate, CloseRate: f.bid, Amount: p.Amount, StopLoss: p.StopLoss, TakeProfit: p.TakeProfit})
			return broker.Order{OrderID: "c" + p.ID, ClientRef: req.ClientRef, Status: broker.StatusExecuted, PositionIDs: []string{p.ID},
				AvgPrice: f.bid, Units: p.Units, Amount: p.Amount, At: time.Date(2026, 10, 12, 13, 40, 3, 0, time.UTC)}, nil
		}
	}
	return broker.Order{ClientRef: req.ClientRef, Status: broker.StatusRejected, ErrorCode: 741}, broker.ErrPositionNotOpen
}

func (f *fakeBroker) Positions(ctx context.Context) ([]broker.Position, error) {
	return append([]broker.Position(nil), f.positions...), nil
}
func (f *fakeBroker) Account(ctx context.Context) (broker.Account, error) {
	return broker.Account{Currency: "USD", Cash: f.equity, Equity: f.equity}, nil
}
func (f *fakeBroker) PreviewOpen(ctx context.Context, req broker.OpenRequest) (broker.Costs, error) {
	return broker.Costs{Currency: "USD", Spread: 0.85, OvernightFee: 0.25}, nil
}
func (f *fakeBroker) ClosedPositions(ctx context.Context, since time.Time) ([]broker.ClosedPosition, error) {
	return f.closed, nil
}

// SetProtection edits the position's resting rates (broker.ProtectionEditor).
func (f *fakeBroker) SetProtection(ctx context.Context, req broker.ProtectionRequest) (broker.ProtectionResult, error) {
	f.protects = append(f.protects, req)
	if err := f.protectErr; err != nil {
		f.protectErr = nil
		return broker.ProtectionResult{PositionID: req.PositionID, ClientRef: req.ClientRef, Status: broker.StatusRejected, Error: err.Error()}, err
	}
	for i := range f.positions {
		if f.positions[i].ID == req.PositionID {
			if req.StopLoss > 0 {
				f.positions[i].StopLoss, f.positions[i].StopType = req.StopLoss, broker.StopFixed
			}
			if req.TakeProfit > 0 {
				f.positions[i].TakeProfit = req.TakeProfit
			}
			return broker.ProtectionResult{PositionID: req.PositionID, ClientRef: req.ClientRef, OperationID: "op-" + req.ClientRef, Status: broker.StatusExecuted, Position: f.positions[i]}, nil
		}
	}
	return broker.ProtectionResult{PositionID: req.PositionID, ClientRef: req.ClientRef, Status: broker.StatusRejected, ErrorCode: 404}, broker.ErrPositionNotOpen
}

// venueClose simulates eToro closing position id at rate (a stop, a target,
// the app): it leaves the portfolio and enters the trading history with the
// rates it carried.
func (f *fakeBroker) venueClose(id string, rate float64, netProfit, fees float64, at time.Time) {
	for i, p := range f.positions {
		if p.ID == id {
			f.positions = append(f.positions[:i], f.positions[i+1:]...)
			f.closed = append(f.closed, broker.ClosedPosition{ID: p.ID, Instrument: p.Instrument, Side: p.Side, Units: p.Units,
				OpenRate: p.OpenRate, CloseRate: rate, Amount: p.Amount, ClosedAt: at, NetProfit: netProfit, Fees: fees, StopLoss: p.StopLoss, TakeProfit: p.TakeProfit})
			return
		}
	}
}

// rising NYSE-day bars from 2026-05-01 to last; a final close override
// lets a test push the last bar below its SMA50.
func bars(last string, lastClose float64) []bitsodaily.Row {
	from := mktcal.Date(2026, 5, 1)
	to, _ := time.Parse("2006-01-02", last)
	var rows []bitsodaily.Row
	for i, d := range mktcal.TradingDays(from, to) {
		c := 100 + float64(i)*0.3
		rows = append(rows, bitsodaily.Row{Date: d.Format("2006-01-02"), Open: c - 0.1, High: c + 0.5, Low: c - 0.5, Close: c, BucketStartUTC: d})
	}
	if lastClose > 0 {
		r := &rows[len(rows)-1]
		r.Close, r.Low = lastClose, lastClose-0.5
		if r.Open < r.Low {
			r.Open = lastClose
		}
	}
	return rows
}

// Monday 2026-10-12 09:40 New York (EDT): in session; the last closed
// trading day is Friday 2026-10-09 and the fill date is 2026-10-12.
var monday = time.Date(2026, 10, 12, 13, 40, 0, 0, time.UTC)

type harness struct {
	t   *testing.T
	o   options
	de  demoEnv
	fb  *fakeBroker
	log []string
}

func newHarness(t *testing.T, demo bool) *harness {
	t.Helper()
	dir := t.TempDir()
	led, err := etoroledger.Open(filepath.Join(dir, "ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, fb: newFakeBroker()}
	h.o = options{ledgerPath: led.Path(), candlesDir: filepath.Join(dir, "candles"), demo: demo, amount: 1100, policy: risk.EtoroDemoPolicy()}
	h.de = demoEnv{b: h.fb, led: led, intents: etoroledger.IntentDir(led.Path()), policy: h.o.policy, amount: 1100,
		now: func() time.Time { return monday }, logf: func(f string, a ...any) { h.log = append(h.log, fmt.Sprintf(f, a...)) }}
	return h
}

func (h *harness) run(now time.Time, rows []bitsodaily.Row) error {
	h.de.now = func() time.Time { return now }
	fetch := func() ([]bitsodaily.Row, etorodaily.Report, error) { return rows, etorodaily.Report{}, nil }
	mode := etoroledger.ModeDryRun
	if h.o.demo {
		mode = etoroledger.ModeDemo
	}
	return runBook(context.Background(), h.o, h.de, frozenSpecs["NSDQ100"], fetch, now, "test", mode)
}

func (h *harness) records() []etoroledger.Record {
	recs, err := etoroledger.ReadFile(h.o.ledgerPath)
	if err != nil {
		h.t.Fatal(err)
	}
	return recs
}

func TestDecideAndPaper(t *testing.T) {
	rows := bars("2026-10-09", 0)
	d, b, pts, err := decide(rows, monday)
	if err != nil {
		t.Fatal(err)
	}
	if d.BarDate != "2026-10-09" || d.FillDate != "2026-10-12" || d.Signal != "long" || d.Action != "hold" || !(d.Close > d.SMA) {
		t.Fatalf("decision %+v", d)
	}
	p, err := paper(b, pts, frozenSpecs["NSDQ100"])
	if err != nil || p.Started || p.PendingAction != "buy" {
		t.Fatalf("before the forward start: %+v %v", p, err)
	}
	s := frozenSpecs["NSDQ100"]
	s.ForwardStart = "2026-09-01"
	p, err = paper(b, pts, s)
	if err != nil || !p.Started || p.Position != "long" || p.RoundTrips != 1 || p.PendingAction != "hold" ||
		!(p.Equity > 1) || !(p.FinancingPct > 0) || p.Days != len(mktcal.TradingDays(mktcal.Date(2026, 9, 1), mktcal.Date(2026, 10, 9))) {
		t.Fatalf("paper %+v %v", p, err)
	}
	// A run before Friday's bucket closed (Saturday 2026-10-10 00:00 UTC) is stale.
	if _, _, _, err := decide(bars("2026-10-08", 0), monday); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale bars: %v", err)
	}
	// The last bar below its SMA50 flips the rule to flat.
	d, _, _, err = decide(bars("2026-10-09", 90), monday)
	if err != nil || d.Signal != "flat" || d.Action != "sell" {
		t.Fatalf("flat decision %+v %v", d, err)
	}
}

func TestDryRunRecordsOnceAndChecksIntegrity(t *testing.T) {
	h := newHarness(t, false)
	rows := bars("2026-10-09", 0)
	if err := h.run(monday, rows); err != nil {
		t.Fatal(err)
	}
	if err := h.run(monday.Add(time.Hour), rows); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	recs := h.records()
	if len(recs) != 1 || recs[0].Mode != etoroledger.ModeDryRun || recs[0].Demo != nil || recs[0].Venue != "etoro" ||
		recs[0].Instrument.ID != 28 || recs[0].Candles.SHA256Short == "" || recs[0].Schema != etoroledger.Schema {
		t.Fatalf("records %+v", recs)
	}
	if len(h.fb.opens) != 0 {
		t.Fatal("dry run placed an order")
	}
	// eToro revising a recorded close stops the run.
	rows[len(rows)-1].Close += 1
	if err := h.run(monday.Add(2*time.Hour), rows); err == nil || !strings.Contains(err.Error(), "was recorded as") {
		t.Fatalf("revised close: %v", err)
	}
}

// eToro's live daily route serves two versions of recent closes, ~1 bps
// apart (demo, 2026-10-10). The executor reads twice, records the noise and
// tolerates it on recorded days, and refuses reads that disagree more.
func TestTwoVersionHistory(t *testing.T) {
	h := newHarness(t, false)
	a := bars("2026-10-09", 0)
	b := bars("2026-10-09", 0)
	for i := len(b) - 30; i < len(b)-5; i += 3 {
		b[i].Close *= 1 + 1.2e-4
	}
	reads := 0
	fetch := func() ([]bitsodaily.Row, etorodaily.Report, error) {
		reads++
		if reads%2 == 0 {
			return b, etorodaily.Report{}, nil
		}
		return a, etorodaily.Report{}, nil
	}
	if err := runBook(context.Background(), h.o, h.de, frozenSpecs["NSDQ100"], fetch, monday, "test", etoroledger.ModeDryRun); err != nil {
		t.Fatal(err)
	}
	c := h.records()[0].Candles
	if c.Revised != 9 || c.MaxRevisionBps < 1.1 || c.MaxRevisionBps > 1.3 {
		t.Fatalf("revisions %+v", c)
	}
	// Tuesday reads the other version of a recorded day: tolerated.
	tue := bars("2026-10-12", 0)
	tue[len(tue)-2].Close *= 1 + 1e-4
	if err := h.run(monday.AddDate(0, 0, 1), tue); err != nil {
		t.Fatalf("1 bps revision of a recorded day: %v", err)
	}
	// Two reads 20 bps apart: not acted on.
	b2 := bars("2026-10-09", 0)
	b2[len(b2)-10].Close *= 1.002
	reads = 0
	fetch2 := func() ([]bitsodaily.Row, etorodaily.Report, error) {
		reads++
		if reads == 2 {
			return b2, etorodaily.Report{}, nil
		}
		return bars("2026-10-09", 0), etorodaily.Report{}, nil
	}
	h2 := newHarness(t, true)
	if err := runBook(context.Background(), h2.o, h2.de, frozenSpecs["NSDQ100"], fetch2, monday, "test", etoroledger.ModeDemo); err == nil ||
		!strings.Contains(err.Error(), "not acting") || len(h2.fb.opens) != 0 {
		t.Fatalf("disagreeing reads: %v opens %v", err, h2.fb.opens)
	}
}

func TestDemoOpenHoldClose(t *testing.T) {
	h := newHarness(t, true)
	if err := h.run(monday, bars("2026-10-09", 0)); err != nil {
		t.Fatal(err)
	}
	recs := h.records()
	if len(recs) != 1 || recs[0].Demo == nil {
		t.Fatalf("records %+v", recs)
	}
	d := recs[0].Demo
	if d.Action != etoroledger.ActionOpen || d.PositionAfter.State != "long" || d.PositionAfter.PositionID == "" ||
		d.Order == nil || d.Order.Status != "executed" || d.Order.ClientRef != "sma50-nsdq100-2026-10-12-open" ||
		d.Order.RequestID == "" || d.Order.Resumed || d.Risk == nil || !d.Risk.Allowed || d.Costs == nil || d.Quote == nil {
		t.Fatalf("open record %+v", d)
	}
	if want := (130.1 - 130.0) / 130.0 * 1e4; d.Order.FillVsMidBps < want-1e-9 || d.Order.FillVsMidBps > want+1e-9 {
		t.Fatalf("fill vs mid %v, want %v", d.Order.FillVsMidBps, want)
	}
	if in, ok, _ := etoroledger.LoadIntent(h.de.intents, d.Order.ClientRef); !ok || !in.IntentAt.Equal(monday) {
		t.Fatalf("intent %+v %v", in, ok)
	}

	// Tuesday: still long, the account agrees -> recorded, nothing sent.
	tue := monday.AddDate(0, 0, 1)
	h.fb.positions[0].Fees = 0.25
	if err := h.run(tue, bars("2026-10-12", 0)); err != nil {
		t.Fatal(err)
	}
	recs = h.records()
	if d := recs[1].Demo; len(recs) != 2 || d.Action != etoroledger.ActionNone || d.PositionAfter.State != "long" || d.FinancingToDate != 0.25 || d.Order != nil {
		t.Fatalf("hold record %+v", recs[1].Demo)
	}

	// Wednesday: the last close is below its SMA50 -> close the position.
	wed := monday.AddDate(0, 0, 2)
	h.fb.bid, h.fb.ask = 90.4, 90.6
	if err := h.run(wed, bars("2026-10-13", 90)); err != nil {
		t.Fatal(err)
	}
	recs = h.records()
	d = recs[2].Demo
	if d.Action != etoroledger.ActionClose || d.PositionAfter.State != "flat" || d.Order.Status != "executed" ||
		d.Order.ClientRef != "sma50-nsdq100-2026-10-14-close" || len(h.fb.positions) != 0 {
		t.Fatalf("close record %+v (positions %v)", d, h.fb.positions)
	}
	if len(h.fb.opens) != 1 || len(h.fb.closes) != 1 {
		t.Fatalf("orders: opens %v closes %v", h.fb.opens, h.fb.closes)
	}
}

func TestDemoCrashResumeOpensOnce(t *testing.T) {
	h := newHarness(t, true)
	h.fb.openErr, h.fb.landOnErr = fmt.Errorf("%w: context deadline exceeded", broker.ErrOutcomeUnknown), true
	err := h.run(monday, bars("2026-10-09", 0))
	var nr errNotRecorded
	if !errors.As(err, &nr) || len(h.records()) != 0 || len(h.fb.positions) != 1 {
		t.Fatalf("first run: %v, records %d, positions %d", err, len(h.records()), len(h.fb.positions))
	}
	// The re-run sees an untracked position, but the intent says it is this
	// leg's own order: it resends the SAME ref and records the first order.
	if err := h.run(monday.Add(10*time.Minute), bars("2026-10-09", 0)); err != nil {
		t.Fatal(err)
	}
	recs := h.records()
	if len(recs) != 1 || !recs[0].Demo.Order.Resumed || !recs[0].Demo.Order.IntentAt.Equal(monday) || len(h.fb.positions) != 1 ||
		recs[0].Demo.PositionAfter.PositionID != h.fb.positions[0].ID {
		t.Fatalf("resume: %+v positions %v", recs, h.fb.positions)
	}
	if len(h.fb.opens) != 2 || h.fb.opens[0] != h.fb.opens[1] {
		t.Fatalf("opens %v: want the same ref twice", h.fb.opens)
	}
}

func TestDemoRefusesUntrackedPosition(t *testing.T) {
	h := newHarness(t, true)
	h.fb.positions = []broker.Position{{ID: "999", Instrument: broker.Instrument{ID: 28}, Side: broker.Long, Units: 1, Amount: 130}}
	err := h.run(monday, bars("2026-10-09", 0))
	if err == nil || !strings.Contains(err.Error(), "resolve by hand") || len(h.records()) != 0 || len(h.fb.opens) != 0 {
		t.Fatalf("untracked: %v records %d opens %v", err, len(h.records()), h.fb.opens)
	}
}

// P5d: an operator closes the executor's position from the console on
// Monday afternoon. The console writes a ledger line with source "ui" (bar
// date = fill date = Monday, close = the reference price it checked
// against). Tuesday's run plans from that line (flat), re-opens because the
// rule still says long, and the ui line's close never trips the
// data-integrity check even though it is not a bar close.
func TestDemoPlansFromConsoleTrade(t *testing.T) {
	h := newHarness(t, true)
	if err := h.run(monday, bars("2026-10-09", 0)); err != nil {
		t.Fatal(err)
	}
	opened := h.records()[0].Demo.PositionAfter
	// The operator's close, 13:00 New York, through the console.
	h.fb.closes = nil
	bo, err := h.fb.Close(context.Background(), broker.CloseRequest{Instrument: broker.Instrument{ID: 28}, PositionID: opened.PositionID, ClientRef: "ui-nsdq100-2026-10-12-close-3f9a1c"})
	if err != nil {
		t.Fatal(err)
	}
	ui := etoroledger.Record{Schema: etoroledger.Schema, RecordedAt: "2026-10-12T17:00:00Z", Mode: etoroledger.ModeDemo, Venue: "etoro",
		Book: "nsdq100", Instrument: etoroledger.Instrument{Symbol: "NSDQ100", ID: 28}, Source: etoroledger.SourceUI,
		Decision: etoroledger.Decision{BarDate: "2026-10-12", FillDate: "2026-10-12", Close: 999, Signal: etoroledger.SignalManual, Action: "sell"},
		Operator: &etoroledger.Operator{By: "diego", Reason: "closing by hand before the long weekend"},
		Demo: &etoroledger.Demo{Env: "demo", Target: etoroledger.SignalManual, Action: etoroledger.ActionClose, PositionBefore: opened, PositionAfter: etoroledger.Flat(),
			Order: &etoroledger.Order{Kind: etoroledger.ActionClose, ClientRef: bo.ClientRef, Status: string(bo.Status), PositionIDs: bo.PositionIDs}}}
	if err := h.de.led.Append(ui); err != nil {
		t.Fatal(err)
	}
	if p := h.de.led.LastDemoPosition("nsdq100"); p.State != "flat" {
		t.Fatalf("after the console close the executor must see flat: %+v", p)
	}
	// Tuesday: the rule still says long, the ledger says flat, the account is
	// flat -> open again with Tuesday's reference.
	tue := monday.AddDate(0, 0, 1)
	if err := h.run(tue, bars("2026-10-12", 0)); err != nil {
		t.Fatalf("tuesday: %v", err)
	}
	recs := h.records()
	if len(recs) != 3 {
		t.Fatalf("records %d", len(recs))
	}
	last := recs[len(recs)-1]
	if last.IsUI() || last.Decision.BarDate != "2026-10-12" || last.Demo.Action != etoroledger.ActionOpen ||
		last.Demo.PositionBefore.State != "flat" || last.Demo.Order.ClientRef != "sma50-nsdq100-2026-10-13-open" {
		t.Fatalf("tuesday record %+v", last)
	}
	if n := etoroledger.LegsOn(recs, "nsdq100", "2026-10-12"); n != 2 {
		t.Fatalf("legs on monday %d (the executor's open and the console's close)", n)
	}
	// The executor's bar-10-12 line and the console's 10-12 line coexist:
	// Get finds the executor's, a re-run finds the day recorded.
	if r, ok := h.de.led.Get("nsdq100", "2026-10-12"); !ok || r.IsUI() {
		t.Fatalf("get %+v %v", r, ok)
	}
	if err := h.run(tue.Add(time.Hour), bars("2026-10-12", 0)); err != nil || len(h.records()) != 3 {
		t.Fatalf("re-run: %v records %d", err, len(h.records()))
	}
}

func TestDemoHaltBlocksAndRecords(t *testing.T) {
	h := newHarness(t, true)
	hs := risk.HaltState{Halted: true, Reason: "Kill switch drill", By: "drill", At: "2026-10-12T13:00:00Z"}
	h.o.policy = risk.ApplyHalt(h.o.policy, hs)
	h.de.policy, h.de.halt = h.o.policy, &hs
	err := h.run(monday, bars("2026-10-09", 0))
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("halted run: %v", err)
	}
	recs := h.records()
	if len(recs) != 1 || recs[0].Demo.Action != etoroledger.ActionBlocked || recs[0].Demo.Risk.Halt == nil || recs[0].Demo.Order != nil || len(h.fb.opens) != 0 {
		t.Fatalf("blocked record %+v opens %v", recs, h.fb.opens)
	}
	if _, ok, _ := etoroledger.LoadIntent(h.de.intents, "sma50-nsdq100-2026-10-12-open"); ok {
		t.Fatal("blocked order left an intent")
	}
}

func TestDemoOutsideSessionNotRecorded(t *testing.T) {
	h := newHarness(t, true)
	early := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) // 08:00 New York
	err := h.run(early, bars("2026-10-09", 0))
	var nr errNotRecorded
	if !errors.As(err, &nr) || !strings.Contains(err.Error(), "cash session") || len(h.records()) != 0 || len(h.fb.opens) != 0 {
		t.Fatalf("pre-market: %v", err)
	}
	// The in-session re-run trades.
	if err := h.run(monday, bars("2026-10-09", 0)); err != nil || len(h.fb.opens) != 1 {
		t.Fatalf("in session: %v opens %v", err, h.fb.opens)
	}
}

func TestDemoRejectedOpenIsRecordedFlat(t *testing.T) {
	h := newHarness(t, true)
	h.fb.openErr = errors.New("400 insufficient funds")
	err := h.run(monday, bars("2026-10-09", 0))
	recs := h.records()
	if err == nil || len(recs) != 1 || recs[0].Demo.PositionAfter.State != "flat" || recs[0].Demo.Order.Error == "" {
		t.Fatalf("rejected: %v %+v", err, recs)
	}
}

func TestDemoResumedCloseReadsHistory(t *testing.T) {
	h := newHarness(t, true)
	// Ledger: long position 1234 since Friday.
	prev := etoroledger.Record{Mode: etoroledger.ModeDemo, Venue: "etoro", Book: "nsdq100",
		Decision: etoroledger.Decision{BarDate: "2026-10-08", FillDate: "2026-10-09", Close: bars("2026-10-08", 0)[len(bars("2026-10-08", 0))-1].Close, Signal: "long"},
		Demo:     &etoroledger.Demo{PositionAfter: etoroledger.Position{State: "long", PositionID: "1234", Units: 8.4}}}
	if err := h.de.led.Append(prev); err != nil {
		t.Fatal(err)
	}
	// An earlier run sent the close and died; eToro closed the position.
	ref := etoroledger.ClientRef("nsdq100", "2026-10-12", etoroledger.ActionClose)
	if _, _, err := etoroledger.RecordIntent(h.de.intents, etoroledger.Intent{ClientRef: ref, Kind: etoroledger.ActionClose, IntentAt: monday.Add(-5 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	h.fb.closed = []broker.ClosedPosition{{ID: "1234", Instrument: broker.Instrument{ID: 28}, Units: 8.4, CloseRate: 129.95, Amount: 1100, Fees: 0.75}}
	if err := h.run(monday, bars("2026-10-09", 90)); err != nil {
		t.Fatal(err)
	}
	recs := h.records()
	d := recs[len(recs)-1].Demo
	if d.Action != etoroledger.ActionClose || d.PositionAfter.State != "flat" || !d.Order.Resumed || d.Order.AvgPrice != 129.95 ||
		d.Order.Fees != 0.75 || len(h.fb.closes) != 0 {
		t.Fatalf("resumed close %+v closes %v", d, h.fb.closes)
	}
}

func TestDemoLongButPositionGoneIsRefused(t *testing.T) {
	h := newHarness(t, true)
	prev := etoroledger.Record{Mode: etoroledger.ModeDemo, Venue: "etoro", Book: "nsdq100",
		Decision: etoroledger.Decision{BarDate: "2026-10-08", FillDate: "2026-10-09", Close: bars("2026-10-08", 0)[len(bars("2026-10-08", 0))-1].Close, Signal: "long"},
		Demo:     &etoroledger.Demo{PositionAfter: etoroledger.Position{State: "long", PositionID: "1234", Units: 8.4}}}
	if err := h.de.led.Append(prev); err != nil {
		t.Fatal(err)
	}
	err := h.run(monday, bars("2026-10-09", 0))
	if err == nil || !strings.Contains(err.Error(), "no longer holds") || len(h.records()) != 1 {
		t.Fatalf("missing position: %v", err)
	}
}

func TestMain(m *testing.M) {
	os.Setenv("TZ", "UTC")
	os.Exit(m.Run())
}
