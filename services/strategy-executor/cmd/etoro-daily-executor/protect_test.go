package main

// Protection plan phase C (2026-10-19): the disaster stop on rule opens,
// the daily ratchet, and venue-close detection through the trading history.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/shared/pkg/stops"
)

// protectedHarness is a demo harness with -protect on and the protected
// policy, as main wires it.
func protectedHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, true)
	h.o.protect = true
	h.o.policy = risk.EtoroDemoProtectedPolicy()
	h.de.policy = h.o.policy
	return h
}

func TestProtectOpenCarriesDisasterStop(t *testing.T) {
	h := protectedHarness(t)
	if err := h.run(monday, bars("2026-10-09", 0)); err != nil {
		t.Fatal(err)
	}
	recs := h.records()
	d := recs[0].Demo
	if d.Action != etoroledger.ActionOpen || d.Order.Status != "executed" || !d.Risk.Allowed {
		t.Fatalf("open %+v", d)
	}
	// The test bars range 1.0 a day around ~130: ATR% < 2 %, so the 8 %
	// floor decides: 130.1 x 0.92, rounded down to the tick.
	pa := d.PositionAfter
	if !pa.Protected() || pa.StopLossRate < 119.6 || pa.StopLossRate > 119.7 || pa.StopType != broker.StopFixed || pa.TakeProfitRate != 0 {
		t.Fatalf("position after %+v: want a fixed stop near 119.69 and no target", pa)
	}
	if d.Protection == nil || d.Protection.Profile != profileDisaster || d.Protection.ATR <= 0 || d.Protection.MaxLossUSD <= 0 {
		t.Fatalf("protection %+v", d.Protection)
	}
	if d.Risk.Order.StopLoss != pa.StopLossRate || d.Risk.PolicyVersion != risk.EtoroDemoProtectedPolicy().Version {
		t.Fatalf("risk check %+v: the stop must go through the protected policy", d.Risk)
	}
	if len(h.fb.positions) != 1 || h.fb.positions[0].StopLoss != pa.StopLossRate {
		t.Fatalf("venue position %+v: the stop was not sent with the order", h.fb.positions)
	}
	if !strings.Contains(strings.Join(h.log, "\n"), "disaster stop") {
		t.Fatalf("log %v", h.log)
	}
}

func TestProtectOffOpensBare(t *testing.T) {
	h := newHarness(t, true) // protect false, EtoroDemoPolicy
	if err := h.run(monday, bars("2026-10-09", 0)); err != nil {
		t.Fatal(err)
	}
	d := h.records()[0].Demo
	if d.PositionAfter.Protected() || d.Protection != nil || h.fb.positions[0].StopLoss != 0 {
		t.Fatalf("protect off must not set a stop: %+v", d)
	}
}

func TestProtectRatchetRaisesNeverLowers(t *testing.T) {
	h := protectedHarness(t)
	if err := h.run(monday, bars("2026-10-09", 0)); err != nil {
		t.Fatal(err)
	}
	first := h.records()[0].Demo.PositionAfter.StopLossRate

	// Tuesday: hold; the market is well above the close - 4 x ATR
	// candidate, which sits ~10 points above the 8 % disaster stop.
	tue := monday.AddDate(0, 0, 1)
	h.fb.bid, h.fb.ask = 140.0, 140.2
	if err := h.run(tue, bars("2026-10-12", 0)); err != nil {
		t.Fatalf("tuesday: %v", err)
	}
	recs := h.records()
	d := recs[1].Demo
	if d.Action != etoroledger.ActionProtect || d.Order == nil || d.Order.Kind != etoroledger.ActionProtect || d.Order.Status != "executed" ||
		d.Order.ClientRef != "sma50-nsdq100-2026-10-13-protect" || d.Order.OrderID == "" {
		t.Fatalf("tuesday record %+v order %+v", d, d.Order)
	}
	raised := d.PositionAfter.StopLossRate
	lastClose := bars("2026-10-12", 0)
	want := lastClose[len(lastClose)-1].Close - stops.DisasterATRMult*1.0 // ATR of the test bars is 1.0
	if raised <= first || raised < want-0.02 || raised > want+0.02 || d.PositionAfter.PositionID != recs[0].Demo.PositionAfter.PositionID {
		t.Fatalf("stop %.2f -> %.2f, want close - 4 x ATR = %.2f", first, raised, want)
	}
	if d.Protection == nil || d.Protection.Profile != profileRatchet || d.Protection.StopLossBefore != first || d.Protection.MaxLossUSD <= 0 {
		t.Fatalf("protection %+v", d.Protection)
	}
	if len(h.fb.protects) != 1 || h.fb.protects[0].StopLoss != raised || h.fb.positions[0].StopLoss != raised || len(h.fb.opens) != 1 || len(h.fb.closes) != 0 {
		t.Fatalf("venue: protects %+v positions %+v opens %v closes %v", h.fb.protects, h.fb.positions, h.fb.opens, h.fb.closes)
	}
	if n := etoroledger.LegsOn(recs, "nsdq100", "2026-10-13"); n != 0 {
		t.Fatalf("a protect is not a leg: %d", n)
	}

	// Wednesday: the close dips (still above its SMA50): the candidate is
	// below the held stop, so nothing moves and nothing is sent.
	wed := monday.AddDate(0, 0, 2)
	if err := h.run(wed, bars("2026-10-13", 128)); err != nil {
		t.Fatalf("wednesday: %v", err)
	}
	recs = h.records()
	d = recs[2].Demo
	if d.Action != etoroledger.ActionNone || d.Order != nil || d.PositionAfter.StopLossRate != raised || len(h.fb.protects) != 1 {
		t.Fatalf("wednesday record %+v (protects %d)", d, len(h.fb.protects))
	}
	if !strings.Contains(strings.Join(d.Notes, "\n"), "unchanged") {
		t.Fatalf("notes %v", d.Notes)
	}
}

func TestProtectRatchetSetsFirstStopOnUnprotectedPosition(t *testing.T) {
	// A rule position opened before phase C (no stop) gets its disaster
	// stop at the next hold run, and the ledger learns the account's rates.
	h := newHarness(t, true)
	if err := h.run(monday, bars("2026-10-09", 0)); err != nil {
		t.Fatal(err)
	}
	h.o.protect, h.o.policy = true, risk.EtoroDemoProtectedPolicy()
	h.de.policy = h.o.policy
	h.fb.bid, h.fb.ask = 140.0, 140.2
	if err := h.run(monday.AddDate(0, 0, 1), bars("2026-10-12", 0)); err != nil {
		t.Fatal(err)
	}
	d := h.records()[1].Demo
	if d.Action != etoroledger.ActionProtect || d.Protection == nil || d.Protection.Profile != profileDisaster || d.Protection.StopLossBefore != 0 || !d.PositionAfter.Protected() {
		t.Fatalf("first stop %+v %+v", d, d.Protection)
	}
}

func TestProtectRatchetFailureIsRecordedAndReported(t *testing.T) {
	h := protectedHarness(t)
	if err := h.run(monday, bars("2026-10-09", 0)); err != nil {
		t.Fatal(err)
	}
	h.fb.bid, h.fb.ask = 140.0, 140.2
	h.fb.protectErr = errors.New("503 try later")
	err := h.run(monday.AddDate(0, 0, 1), bars("2026-10-12", 0))
	if err == nil || !strings.Contains(err.Error(), "not confirmed") {
		t.Fatalf("err %v", err)
	}
	recs := h.records()
	d := recs[1].Demo
	first := recs[0].Demo.PositionAfter.StopLossRate
	if len(recs) != 2 || d.Action != etoroledger.ActionNone || d.Order == nil || d.Order.Error == "" || d.PositionAfter.StopLossRate != first {
		t.Fatalf("failed ratchet %+v order %+v", d, d.Order)
	}
	// The day is recorded: a re-run finds it and does nothing.
	if err := h.run(monday.AddDate(0, 0, 1).Add(time.Hour), bars("2026-10-12", 0)); err != nil || len(h.records()) != 2 {
		t.Fatalf("re-run %v records %d", err, len(h.records()))
	}
}

func TestProtectLeavesCustomConsoleStopAlone(t *testing.T) {
	h := protectedHarness(t)
	// A console open with a custom stop (source ui), as ui-api writes it.
	bo, err := h.fb.Open(context.Background(), broker.OpenRequest{Instrument: broker.Instrument{ID: 28, Symbol: "NSDQ100"}, Side: broker.Long, Amount: 1100, Leverage: 1, ClientRef: "ui-nsdq100-2026-10-09-open-abc123", StopLoss: 127.5})
	if err != nil {
		t.Fatal(err)
	}
	ui := etoroledger.Record{Schema: etoroledger.Schema, RecordedAt: "2026-10-09T17:00:00Z", Mode: etoroledger.ModeDemo, Venue: "etoro",
		Book: "nsdq100", Instrument: etoroledger.Instrument{Symbol: "NSDQ100", ID: 28}, Source: etoroledger.SourceUI,
		Decision: etoroledger.Decision{BarDate: "2026-10-09", FillDate: "2026-10-09", Close: 130, Signal: etoroledger.SignalManual, Action: "buy"},
		Operator: &etoroledger.Operator{By: "diego", Reason: "test"},
		Demo: &etoroledger.Demo{Env: "demo", Target: etoroledger.SignalManual, Action: etoroledger.ActionOpen, PositionBefore: etoroledger.Flat(),
			PositionAfter: etoroledger.Position{State: "long", PositionID: bo.PositionIDs[0], Units: bo.Units, Amount: 1100, OpenRate: bo.AvgPrice, StopLossRate: 127.5, StopType: broker.StopFixed},
			Protection:    &etoroledger.Protection{Profile: string(stops.Custom)},
			Order:         &etoroledger.Order{Kind: etoroledger.ActionOpen, ClientRef: bo.ClientRef, Status: string(bo.Status), PositionIDs: bo.PositionIDs}}}
	if err := h.de.led.Append(ui); err != nil {
		t.Fatal(err)
	}
	// Monday: the rule says long, the ledger (console line) says long ->
	// hold; the custom stop is the operator's and is not ratcheted.
	h.fb.bid, h.fb.ask = 140.0, 140.2
	if err := h.run(monday, bars("2026-10-09", 0)); err != nil {
		t.Fatal(err)
	}
	recs := h.records()
	d := recs[len(recs)-1].Demo
	if d.Action != etoroledger.ActionNone || len(h.fb.protects) != 0 || d.PositionAfter.StopLossRate != 127.5 || !strings.Contains(strings.Join(d.Notes, "\n"), "custom") {
		t.Fatalf("custom stop touched: %+v notes %v protects %v", d, d.Notes, h.fb.protects)
	}
}

func TestVenueCloseStoppedOutIsRecordedThenReopensNextDay(t *testing.T) {
	h := protectedHarness(t)
	if err := h.run(monday, bars("2026-10-09", 0)); err != nil {
		t.Fatal(err)
	}
	opened := h.records()[0].Demo.PositionAfter
	// Overnight eToro fills the stop 40 points through the rate (a gap).
	stoppedAt := time.Date(2026, 10, 12, 20, 15, 0, 0, time.UTC)
	h.fb.venueClose(opened.PositionID, opened.StopLossRate-0.4, -92.1, 0.3, stoppedAt)

	// Tuesday: the rule still says long, the ledger says long, the account
	// is flat -> the history explains it; recorded as venue_close, flat,
	// no order sent this run.
	tue := monday.AddDate(0, 0, 1)
	if err := h.run(tue, bars("2026-10-12", 0)); err != nil {
		t.Fatalf("tuesday: %v", err)
	}
	recs := h.records()
	d := recs[1].Demo
	if len(recs) != 2 || d.Action != etoroledger.ActionVenueClose || d.PositionAfter.State != "flat" || d.VenueClose == nil ||
		d.Order == nil || d.Order.Kind != etoroledger.ActionVenueClose || d.Order.Status != "executed" || d.Order.AvgPrice != opened.StopLossRate-0.4 || d.Order.Fees != 0.3 {
		t.Fatalf("venue close record %+v vc %+v order %+v", d, d.VenueClose, d.Order)
	}
	vc := d.VenueClose
	if vc.Kind != etoroledger.VenueCloseStoppedOut || vc.StopLossRate != opened.StopLossRate || vc.GapBps <= 0 || vc.NetProfit != -92.1 || vc.ClosedAt != stoppedAt.Format(time.RFC3339) {
		t.Fatalf("venue close %+v", vc)
	}
	if len(h.fb.opens) != 1 || len(h.fb.closes) != 0 {
		t.Fatalf("no order may be sent in the run that records the venue close: opens %v closes %v", h.fb.opens, h.fb.closes)
	}
	if n := etoroledger.LegsOn(recs, "nsdq100", "2026-10-13"); n != 0 {
		t.Fatalf("a venue close is not a leg: %d", n)
	}
	// Wednesday: the ledger is flat, the rule is long -> re-open, with a stop.
	if err := h.run(monday.AddDate(0, 0, 2), bars("2026-10-13", 0)); err != nil {
		t.Fatalf("wednesday: %v", err)
	}
	recs = h.records()
	last := recs[len(recs)-1].Demo
	if last.Action != etoroledger.ActionOpen || !last.PositionAfter.Protected() || len(h.fb.opens) != 2 || last.PositionBefore.State != "flat" {
		t.Fatalf("re-open %+v opens %v", last, h.fb.opens)
	}
}

func TestVenueCloseTookProfitAndOther(t *testing.T) {
	seed := func(t *testing.T, h *harness, sl, tp float64) etoroledger.Position {
		t.Helper()
		pos := etoroledger.Position{State: "long", PositionID: "1234", Units: 8.4, Amount: 1100, OpenRate: 131, OpenedAt: "2026-10-08T13:35:00Z", StopLossRate: sl, TakeProfitRate: tp, StopType: broker.StopFixed}
		prev := etoroledger.Record{Mode: etoroledger.ModeDemo, Venue: "etoro", Book: "nsdq100", Instrument: etoroledger.Instrument{Symbol: "NSDQ100", ID: 28},
			Decision: etoroledger.Decision{BarDate: "2026-10-08", FillDate: "2026-10-09", Close: bars("2026-10-08", 0)[len(bars("2026-10-08", 0))-1].Close, Signal: "long"},
			Demo:     &etoroledger.Demo{PositionAfter: pos}}
		if err := h.de.led.Append(prev); err != nil {
			t.Fatal(err)
		}
		return pos
	}
	t.Run("took_profit", func(t *testing.T) {
		h := protectedHarness(t)
		pos := seed(t, h, 120, 150)
		h.fb.closed = []broker.ClosedPosition{{ID: pos.PositionID, Instrument: broker.Instrument{ID: 28}, Units: 8.4, CloseRate: 150.03, Amount: 1100, NetProfit: 159.6, Fees: 0.5, StopLoss: 120, TakeProfit: 150, ClosedAt: monday.Add(-3 * time.Hour)}}
		if err := h.run(monday, bars("2026-10-09", 0)); err != nil {
			t.Fatal(err)
		}
		d := h.records()[1].Demo
		if d.Action != etoroledger.ActionVenueClose || d.VenueClose.Kind != etoroledger.VenueCloseTookProfit || d.VenueClose.TakeProfitRate != 150 || d.PositionAfter.State != "flat" {
			t.Fatalf("%+v %+v", d, d.VenueClose)
		}
	})
	t.Run("other", func(t *testing.T) {
		h := protectedHarness(t)
		pos := seed(t, h, 120, 150)
		h.fb.closed = []broker.ClosedPosition{{ID: pos.PositionID, Instrument: broker.Instrument{ID: 28}, Units: 8.4, CloseRate: 133, Amount: 1100, NetProfit: 16.8, Fees: 0.5, StopLoss: 120, TakeProfit: 150, ClosedAt: monday.Add(-3 * time.Hour)}}
		if err := h.run(monday, bars("2026-10-09", 0)); err != nil {
			t.Fatal(err)
		}
		d := h.records()[1].Demo
		if d.Action != etoroledger.ActionVenueClose || d.VenueClose.Kind != etoroledger.VenueCloseOther || d.VenueClose.GapBps != 0 || !strings.Contains(strings.Join(d.Notes, " "), "between the stop") {
			t.Fatalf("%+v %+v notes %v", d, d.VenueClose, d.Notes)
		}
	})
	t.Run("history rates win over the ledger's", func(t *testing.T) {
		// The ledger never learned the stop (set in the app); the history
		// carries it and the close sits on it.
		h := protectedHarness(t)
		pos := seed(t, h, 0, 0)
		h.fb.closed = []broker.ClosedPosition{{ID: pos.PositionID, Instrument: broker.Instrument{ID: 28}, Units: 8.4, CloseRate: 125, Amount: 1100, StopLoss: 125, ClosedAt: monday.Add(-3 * time.Hour)}}
		if err := h.run(monday, bars("2026-10-09", 0)); err != nil {
			t.Fatal(err)
		}
		d := h.records()[1].Demo
		if d.VenueClose == nil || d.VenueClose.Kind != etoroledger.VenueCloseStoppedOut || d.VenueClose.StopLossRate != 125 {
			t.Fatalf("%+v", d.VenueClose)
		}
	})
}

func TestVenueCloseNotInHistoryYetIsNotRecorded(t *testing.T) {
	h := protectedHarness(t)
	prev := etoroledger.Record{Mode: etoroledger.ModeDemo, Venue: "etoro", Book: "nsdq100",
		Decision: etoroledger.Decision{BarDate: "2026-10-08", FillDate: "2026-10-09", Close: bars("2026-10-08", 0)[len(bars("2026-10-08", 0))-1].Close, Signal: "long"},
		Demo:     &etoroledger.Demo{PositionAfter: etoroledger.Position{State: "long", PositionID: "1234", Units: 8.4, StopLossRate: 120}}}
	if err := h.de.led.Append(prev); err != nil {
		t.Fatal(err)
	}
	err := h.run(monday, bars("2026-10-09", 0))
	var nr errNotRecorded
	if !errors.As(err, &nr) || !strings.Contains(err.Error(), "does not list it yet") || len(h.records()) != 1 || len(h.fb.opens) != 0 {
		t.Fatalf("history lag: %v records %d opens %v", err, len(h.records()), h.fb.opens)
	}
	// Later the same day the history lists it: the re-run records it.
	h.fb.closed = []broker.ClosedPosition{{ID: "1234", Instrument: broker.Instrument{ID: 28}, Units: 8.4, CloseRate: 119.98, Amount: 1100, StopLoss: 120, ClosedAt: monday.Add(-2 * time.Hour)}}
	if err := h.run(monday.Add(2*time.Hour), bars("2026-10-09", 0)); err != nil {
		t.Fatal(err)
	}
	if d := h.records()[1].Demo; d.Action != etoroledger.ActionVenueClose || d.VenueClose.Kind != etoroledger.VenueCloseStoppedOut {
		t.Fatalf("%+v", d)
	}
}
