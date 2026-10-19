package main

import (
	"math"
	"strings"
	"testing"

	"bitso-trading-platform/shared/pkg/cfdrisk"
	"bitso-trading-platform/shared/pkg/etoroledger"
)

// Protection plan phase C6 (2026-10-19): the section from a ledger with a
// protected rule open, a console protect, an executor ratchet, a stop-out
// through a gap and a take-profit.
func TestProtectionSection(t *testing.T) {
	long := func(id string, open, sl, tp float64) etoroledger.Position {
		return etoroledger.Position{State: "long", PositionID: id, Units: 0.036, Amount: 1100, OpenRate: open, StopLossRate: sl, TakeProfitRate: tp, StopType: "fixed"}
	}
	rec := func(book, bar, fill, action, src string, before, after etoroledger.Position, o *etoroledger.Order) etoroledger.Record {
		return etoroledger.Record{Mode: etoroledger.ModeDemo, Book: book, Source: src, RecordedAt: fill + "T13:35:00Z",
			Decision: etoroledger.Decision{BarDate: bar, FillDate: fill},
			Demo:     &etoroledger.Demo{Action: action, PositionBefore: before, PositionAfter: after, Order: o}}
	}
	exec := func(kind, status string) *etoroledger.Order {
		return &etoroledger.Order{Kind: kind, Status: status, OrderID: "op1"}
	}

	openN := rec("nsdq100", "2026-10-09", "2026-10-12", etoroledger.ActionOpen, "", etoroledger.Flat(), long("11", 30000, 27600, 0), exec("open", "executed"))
	openN.Demo.Protection = &etoroledger.Protection{Profile: "disaster", ATR: 450, ATRPct: 0.015, MaxLossUSD: 86.4}
	ratchet := rec("nsdq100", "2026-10-12", "2026-10-13", etoroledger.ActionProtect, "", long("11", 30000, 27600, 0), long("11", 30000, 28200, 0), exec("protect", "executed"))
	ratchet.Demo.Protection = &etoroledger.Protection{Profile: "ratchet", ATR: 450, StopLossBefore: 27600, MaxLossUSD: 64.8, Reason: "daily ratchet"}
	stopOut := rec("nsdq100", "2026-10-13", "2026-10-14", etoroledger.ActionVenueClose, "", long("11", 30000, 28200, 0), etoroledger.Flat(), &etoroledger.Order{Kind: "venue_close", Status: "executed", AvgPrice: 28150, Units: 0.036, Fees: 0.4})
	stopOut.Demo.VenueClose = &etoroledger.VenueClose{Kind: etoroledger.VenueCloseStoppedOut, StopLossRate: 28200, CloseRate: 28150, GapBps: 17.7, ClosedAt: "2026-10-13T20:10:00Z", NetProfit: -66.6, Fees: 0.4}

	openS := rec("spx500", "2026-10-12", "2026-10-12", etoroledger.ActionOpen, etoroledger.SourceUI, etoroledger.Flat(), long("22", 7800, 7644, 8112), exec("open", "executed"))
	openS.Demo.Protection = &etoroledger.Protection{Profile: "moderate", ATR: 72}
	protectS := rec("spx500", "2026-10-12", "2026-10-12", etoroledger.ActionProtect, etoroledger.SourceUI, long("22", 7800, 7644, 8112), long("22", 7800, 7700, 8112), exec("protect", "executed"))
	protectS.RecordedAt = "2026-10-12T15:00:00Z"
	protectS.Demo.Protection = &etoroledger.Protection{Profile: "custom", StopLossBefore: 7644}
	tp := rec("spx500", "2026-10-13", "2026-10-14", etoroledger.ActionVenueClose, "", long("22", 7800, 7700, 8112), etoroledger.Flat(), &etoroledger.Order{Kind: "venue_close", Status: "executed", AvgPrice: 8112.5})
	tp.Demo.VenueClose = &etoroledger.VenueClose{Kind: etoroledger.VenueCloseTookProfit, StopLossRate: 7700, TakeProfitRate: 8112, CloseRate: 8112.5, GapBps: -0.6, NetProfit: 11.2, Fees: 0.2}
	// A third book still held and protected, to exercise the positions table.
	heldOpen := rec("nsdq100", "2026-10-14", "2026-10-15", etoroledger.ActionOpen, "", etoroledger.Flat(), long("33", 29000, 26680, 0), exec("open", "executed"))

	recs := []etoroledger.Record{openN, ratchet, stopOut, openS, protectS, tp, heldOpen}
	sec := protectionSection(recs, map[string]cfdrisk.Mark{"nsdq100": {Close: 29500}}, map[string]float64{"nsdq100": 450})

	if len(sec.Positions) != 1 || sec.Positions[0].PositionID != "33" || !sec.Positions[0].Protected || sec.Positions[0].Source != "rule" {
		t.Fatalf("positions %+v", sec.Positions)
	}
	p := sec.Positions[0]
	if math.Abs(p.StopPctOfOpen-0.08) > 1e-6 || math.Abs(p.MaxLossUSD-83.52) > 1e-6 || math.Abs(p.StopATRs-6.2667) > 1e-3 {
		t.Fatalf("position figures %+v", p)
	}
	if len(sec.Edits) != 2 || sec.Edits[0].Source != "manual" || sec.Edits[0].Profile != "custom" || sec.Edits[0].StopBefore != 7644 || sec.Edits[0].StopAfter != 7700 ||
		sec.Edits[1].Source != "rule" || sec.Edits[1].Profile != "ratchet" || sec.Edits[1].StopAfter != 28200 {
		t.Fatalf("edits %+v", sec.Edits)
	}
	if len(sec.VenueCloses) != 2 {
		t.Fatalf("venue closes %+v", sec.VenueCloses)
	}
	so := sec.VenueCloses[0]
	if so.Kind != etoroledger.VenueCloseStoppedOut || so.Realized != -67 || math.Abs(so.ModelledAtStop-(-0.036*1800)) > 1e-9 || math.Abs(so.VsModelled-(-67+0.036*1800)) > 1e-9 || so.GapBps != 17.7 {
		t.Fatalf("stop-out %+v", so)
	}
	if sec.VenueCloses[1].Kind != etoroledger.VenueCloseTookProfit || sec.VenueCloses[1].ModelledAtStop != 0 || sec.VenueCloses[1].Realized != 11 {
		t.Fatalf("take profit %+v", sec.VenueCloses[1])
	}
	s := sec.Summary
	if s.Held != 1 || s.HeldProtected != 1 || s.Edits != 2 || s.Ratchets != 1 || s.StopOuts != 1 || s.TakeProfits != 1 || s.OtherCloses != 0 ||
		math.Abs(s.RealizedUSD-(-67+11)) > 1e-9 || s.WorstGapBps != 17.7 {
		t.Fatalf("summary %+v", s)
	}
	md := renderMarkdown(&Report{Protection: sec, Notes: []string{}})
	for _, want := range []string{"## 6. Protection (M15)", "| nsdq100 | 33 | rule |", "27600.00 → 28200.00", "**stopped_out**", "**took_profit**", "Held 1 (protected 1)", "executor ratchets 1"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
}
