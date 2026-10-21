package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/etorodaily"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/shared/pkg/risk"
)

// etfHarness is newHarness under the policy main selects for ETF runs.
func etfHarness(t *testing.T, demo bool) *harness {
	h := newHarness(t, demo)
	h.o.policy = risk.EtoroDemoETFPolicy()
	h.de.policy = h.o.policy
	return h
}

// The SMA50 ETF cell's demo leg: an ETF open under the ETF policy goes
// through without a stop (the CFD policies would refuse qqq outright, and
// the protected one would demand a stop the cell does not set).
func TestETFDemoOpenUnderETFPolicy(t *testing.T) {
	h := etfHarness(t, true)
	h.de.protect = true // as the default -protect; must be ignored for an ETF
	fetch := func() ([]bitsodaily.Row, etorodaily.Report, error) {
		return bars("2026-10-09", 0), etorodaily.Report{}, nil
	}
	if err := runBook(context.Background(), h.o, h.de, frozenSpecs["QQQ"], fetch, monday, "test", etoroledger.ModeDemo); err != nil {
		t.Fatalf("ETF demo open: %v", err)
	}
	rec, ok := h.de.led.Get("qqq", "2026-10-09")
	if !ok || rec.Demo == nil || rec.Demo.Action != etoroledger.ActionOpen || rec.Demo.PositionAfter.State != "long" {
		t.Fatalf("record %+v", rec)
	}
	if rec.Demo.Risk.PolicyVersion != "etoro-demo-etf-2026-10-21" || !rec.Demo.Risk.Allowed || rec.Demo.PositionAfter.StopLossRate != 0 || rec.Demo.Protection != nil {
		t.Fatalf("risk %+v protection %+v stop %v", rec.Demo.Risk, rec.Demo.Protection, rec.Demo.PositionAfter.StopLossRate)
	}
	// Under the CFD policy the same leg is blocked: the gap this test guards.
	h2 := newHarness(t, true)
	err := runBook(context.Background(), h2.o, h2.de, frozenSpecs["QQQ"], fetch, monday, "test", etoroledger.ModeDemo)
	if err == nil || !strings.Contains(err.Error(), "blocked by risk policy") {
		t.Fatalf("CFD policy must block qqq, got %v", err)
	}
}

// rotBars builds NYSE-day bars from 2025-06-02 to last with close = f(i).
func rotBars(last string, f func(i int) float64) []bitsodaily.Row {
	to, _ := time.Parse("2006-01-02", last)
	var rows []bitsodaily.Row
	for i, d := range mktcal.TradingDays(mktcal.Date(2025, 6, 2), to) {
		c := f(i)
		rows = append(rows, bitsodaily.Row{Date: d.Format("2006-01-02"), Open: c - 0.1, High: c + 0.5, Low: c - 0.6, Close: c, BucketStartUTC: d})
	}
	return rows
}

// Asset A (QQQ) rises 0.8/day for 250 bars then falls 1.5/day; asset B
// (SPY) rises 0.2/day. At the 2026-08-31 decision A's 12-1 momentum still
// wins; at the 2026-09-30 decision B's does: a switch A -> B.
func aClose(i int) float64 {
	if i < 250 {
		return 100 + 0.8*float64(i)
	}
	return 300 - 1.5*float64(i-250)
}
func bClose(i int) float64 { return 100 + 0.2*float64(i) }

func rotFetchers(last string, spyMissingLast bool) func(instSpec) func() ([]bitsodaily.Row, etorodaily.Report, error) {
	return func(s instSpec) func() ([]bitsodaily.Row, etorodaily.Report, error) {
		return func() ([]bitsodaily.Row, etorodaily.Report, error) {
			var rows []bitsodaily.Row
			if s.Symbol == "QQQ" {
				rows = rotBars(last, aClose)
			} else {
				rows = rotBars(last, bClose)
				if spyMissingLast {
					rows = rows[:len(rows)-1]
				}
			}
			return rows, etorodaily.Report{}, nil
		}
	}
}

func testRotation() rotationSpec {
	rs := frozenRotations["dm_12_1"]
	rs.ForwardStart = "2026-06-01" // so the paper block is exercised
	for i := range rs.Books {
		rs.Books[i].ForwardStart = rs.ForwardStart
	}
	return rs
}

func at(date string) time.Time {
	d, _ := time.Parse("2006-01-02", date)
	return time.Date(d.Year(), d.Month(), d.Day(), 9, 40, 0, 0, mktcal.NewYork).UTC() // 09:40 New York, in session (DST-safe)
}

func TestRotationDecisionAndSwitch(t *testing.T) {
	rs := testRotation()
	// Fixture sanity: the rule must switch at the 2026-09-30 month end.
	d, err := rs.decideRotation([][]bitsodaily.Row{rotBars("2026-09-30", aClose), rotBars("2026-09-30", bClose)}, at("2026-10-01"))
	if err != nil {
		t.Fatal(err)
	}
	if !d.decisionDay || d.prev != 0 || d.target != 1 {
		t.Fatalf("fixture: want decision day A->B, got decision=%v prev=%d target=%d momentum=%v", d.decisionDay, d.prev, d.target, d.momentum)
	}
	if d.barDate != "2026-09-30" || d.fillDate != "2026-10-01" || d.momentum["QQQ"] >= d.momentum["SPY"] {
		t.Fatalf("decision %+v", d)
	}

	h := etfHarness(t, true)
	// One fake quote serves both books; 170 is within the 1,000 bps
	// price-deviation guard of QQQ (~174) and SPY (~167) at these dates.
	h.fb.bid, h.fb.ask = 169.9, 170.1
	ctx := context.Background()

	// Day 1 (bars to 09-29, no decision): the rule's target A is in force
	// and the ledger is flat, so the executor brings QQQ to long.
	h.de.now = func() time.Time { return at("2026-09-30") }
	if err := runRotation(ctx, h.o, h.de, rs, rotFetchers("2026-09-29", false), at("2026-09-30"), "test", etoroledger.ModeDemo); err != nil {
		t.Fatalf("day 1: %v", err)
	}
	if len(h.fb.opens) != 1 || !strings.Contains(h.fb.opens[0], "qqq") || len(h.fb.closes) != 0 {
		t.Fatalf("day 1 orders: opens %v closes %v", h.fb.opens, h.fb.closes)
	}
	q, ok := h.de.led.Get("qqq", "2026-09-29")
	if !ok || q.Decision.Rule != "dm_12_1" || q.Decision.Target != "QQQ" || q.Decision.DecisionDay || q.Decision.Signal != "long" || q.Decision.Action != "hold" {
		t.Fatalf("day 1 qqq line %+v", q.Decision)
	}
	s, ok := h.de.led.Get("spy", "2026-09-29")
	if !ok || s.Decision.Signal != "flat" || s.Demo == nil || s.Demo.Action != etoroledger.ActionNone {
		t.Fatalf("day 1 spy line %+v", s)
	}
	if !q.Paper.Started || q.Paper.Product != KindETF || q.Paper.Position != "QQQ" || q.Paper != s.Paper {
		t.Fatalf("paper: qqq %+v spy %+v", q.Paper, s.Paper)
	}

	// Day 2 (bars to 09-30, the month end): switch A -> B: close QQQ, open SPY.
	h.de.now = func() time.Time { return at("2026-10-01") }
	if err := runRotation(ctx, h.o, h.de, rs, rotFetchers("2026-09-30", false), at("2026-10-01"), "test", etoroledger.ModeDemo); err != nil {
		t.Fatalf("day 2: %v", err)
	}
	if len(h.fb.closes) != 1 || !strings.Contains(h.fb.closes[0], "qqq") || len(h.fb.opens) != 2 || !strings.Contains(h.fb.opens[1], "spy") {
		t.Fatalf("day 2 orders: opens %v closes %v", h.fb.opens, h.fb.closes)
	}
	if len(h.fb.positions) != 1 || h.fb.positions[0].Instrument.Symbol != "SPY" {
		t.Fatalf("day 2 positions %+v", h.fb.positions)
	}
	q2, _ := h.de.led.Get("qqq", "2026-09-30")
	s2, _ := h.de.led.Get("spy", "2026-09-30")
	if !q2.Decision.DecisionDay || q2.Decision.Action != "sell" || q2.Decision.Target != "SPY" || s2.Decision.Action != "buy" || len(s2.Decision.Momentum) != 2 {
		t.Fatalf("day 2 lines qqq %+v spy %+v", q2.Decision, s2.Decision)
	}
	if q2.Demo.PositionAfter.State != "flat" || s2.Demo.PositionAfter.State != "long" || s2.Paper.Position != "QQQ" || s2.Paper.PendingAction != "switch QQQ -> SPY" {
		t.Fatalf("day 2 positions qqq %+v spy %+v paper %+v", q2.Demo.PositionAfter, s2.Demo.PositionAfter, s2.Paper)
	}
	// The closing book was processed before the opening one.
	if !(q2.RecordedAt <= s2.RecordedAt) {
		t.Fatalf("close must be recorded before the open: %s vs %s", q2.RecordedAt, s2.RecordedAt)
	}

	// Day 3 (bars to 10-01, no decision): nothing to do on either book.
	h.de.now = func() time.Time { return at("2026-10-02") }
	if err := runRotation(ctx, h.o, h.de, rs, rotFetchers("2026-10-01", false), at("2026-10-02"), "test", etoroledger.ModeDemo); err != nil {
		t.Fatalf("day 3: %v", err)
	}
	if len(h.fb.opens) != 2 || len(h.fb.closes) != 1 {
		t.Fatalf("day 3 must send nothing: opens %v closes %v", h.fb.opens, h.fb.closes)
	}
	// A re-run the same day is a no-op (already recorded).
	if err := runRotation(ctx, h.o, h.de, rs, rotFetchers("2026-10-01", false), at("2026-10-02"), "test", etoroledger.ModeDemo); err != nil {
		t.Fatalf("day 3 re-run: %v", err)
	}
	if n := len(h.de.led.Records()); n != 6 {
		t.Fatalf("expected 6 ledger lines (3 days x 2 books), got %d", n)
	}
}

func TestRotationRefusesMisalignedAndForeignLedger(t *testing.T) {
	rs := testRotation()
	h := etfHarness(t, false)
	ctx := context.Background()
	// SPY missing the latest day: stale on SPY, nothing recorded.
	err := runRotation(ctx, h.o, h.de, rs, rotFetchers("2026-09-30", true), at("2026-10-01"), "test", etoroledger.ModeDryRun)
	if err == nil || !strings.Contains(err.Error(), "SPY") {
		t.Fatalf("misaligned books must be refused, got %v", err)
	}
	if n := len(h.de.led.Records()); n != 0 {
		t.Fatalf("nothing must be recorded, got %d lines", n)
	}
	// A ledger holding qqq under the SMA50 ETF cell refuses the rotation, and vice versa.
	fetch := func() ([]bitsodaily.Row, etorodaily.Report, error) {
		return bars("2026-10-09", 0), etorodaily.Report{}, nil
	}
	if err := runBook(ctx, h.o, h.de, frozenSpecs["QQQ"], fetch, monday, "test", etoroledger.ModeDryRun); err != nil {
		t.Fatal(err)
	}
	err = runRotation(ctx, h.o, h.de, rs, rotFetchers("2026-10-09", false), monday, "test", etoroledger.ModeDryRun)
	if err == nil || !strings.Contains(err.Error(), "separate -ledger") {
		t.Fatalf("rotation into the ETF cell's ledger must be refused, got %v", err)
	}
	h2 := etfHarness(t, false)
	if err := runRotation(ctx, h2.o, h2.de, rs, rotFetchers("2026-10-09", false), monday, "test", etoroledger.ModeDryRun); err != nil {
		t.Fatal(err)
	}
	err = runBook(ctx, h2.o, h2.de, frozenSpecs["QQQ"], fetch, monday, "test", etoroledger.ModeDryRun)
	if err == nil || !strings.Contains(err.Error(), "separate -ledger") {
		t.Fatalf("SMA50 into the rotation's ledger must be refused, got %v", err)
	}
}

// Before the forward window a demo run opens nothing even though the rule
// has a target in force (pre-registration §2: the paper account is in cash
// until the window's first fill). On the first in-window day it opens.
func TestRotationHoldsCashBeforeForwardWindow(t *testing.T) {
	rs := frozenRotations["dm_12_1"] // ForwardStart 2026-11-02
	h := etfHarness(t, true)
	h.fb.bid, h.fb.ask = 169.9, 170.1
	ctx := context.Background()
	h.de.now = func() time.Time { return at("2026-10-30") }
	if err := runRotation(ctx, h.o, h.de, rs, rotFetchers("2026-10-29", false), at("2026-10-30"), "test", etoroledger.ModeDemo); err != nil {
		t.Fatal(err)
	}
	if len(h.fb.opens) != 0 {
		t.Fatalf("pre-window run must open nothing, sent %v", h.fb.opens)
	}
	for _, b := range []string{"qqq", "spy"} {
		r, ok := h.de.led.Get(b, "2026-10-29")
		if !ok || r.Decision.Signal != "flat" || r.Decision.Action != "hold" || r.Decision.Target == "" || r.Demo.Action != etoroledger.ActionNone {
			t.Fatalf("%s pre-window line %+v", b, r.Decision)
		}
	}
	// 2026-10-30 is the month-end decision; its fill 2026-11-02 is in the window.
	h.de.now = func() time.Time { return at("2026-11-02") }
	if err := runRotation(ctx, h.o, h.de, rs, rotFetchers("2026-10-30", false), at("2026-11-02"), "test", etoroledger.ModeDemo); err != nil {
		t.Fatal(err)
	}
	if len(h.fb.opens) != 1 || len(h.fb.positions) != 1 {
		t.Fatalf("first in-window day must open the target: opens %v positions %d", h.fb.opens, len(h.fb.positions))
	}
	r, _ := h.de.led.Get("spy", "2026-10-30")
	q, _ := h.de.led.Get("qqq", "2026-10-30")
	if !(r.Decision.DecisionDay && q.Decision.DecisionDay) || (r.Decision.Signal == "long") == (q.Decision.Signal == "long") {
		t.Fatalf("exactly one book long on the decision day: qqq %+v spy %+v", q.Decision, r.Decision)
	}
}
