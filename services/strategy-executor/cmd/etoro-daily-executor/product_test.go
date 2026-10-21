package main

import (
	"context"
	"math"
	"strings"
	"testing"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/etorodaily"
	"bitso-trading-platform/shared/pkg/etoroledger"
)

// An ETF spec prices the paper account with a per-order commission and no
// financing; a CFD spec with the spread and the nightly fee. Same bars,
// same signal, different costs.
func TestPaperCostsByProduct(t *testing.T) {
	rows := bars("2026-10-09", 0)
	_, b, pts, err := decide(rows, monday)
	if err != nil {
		t.Fatal(err)
	}
	cfd := frozenSpecs["NSDQ100"]
	cfd.ForwardStart = "2026-09-01"
	etf := frozenSpecs["QQQ"]
	etf.ForwardStart = "2026-09-01"

	pc, err := paper(b, pts, cfd)
	if err != nil {
		t.Fatal(err)
	}
	pe, err := paper(b, pts, etf)
	if err != nil {
		t.Fatal(err)
	}
	if pc.Product != KindCFD || pe.Product != KindETF {
		t.Fatalf("products %q / %q", pc.Product, pe.Product)
	}
	if !(pc.FinancingPct > 0) || pe.FinancingPct != 0 {
		t.Fatalf("financing cfd %.4f etf %.4f", pc.FinancingPct, pe.FinancingPct)
	}
	if pe.FeeBps != 15 || pe.OvernightBps != 0 || pc.FeeBps != 0 {
		t.Fatalf("cost fields cfd %+v etf %+v", pc, pe)
	}
	if pc.RoundTrips != pe.RoundTrips || pc.Position != pe.Position || pc.Days != pe.Days {
		t.Fatalf("same bars and signal must give the same trips/position/days: %+v vs %+v", pc, pe)
	}
	// The ETF leg is 15 bps + 0.14 bps; the CFD leg is 7.75 bps plus
	// ~2.3 bps a night over the window. On one entry held ~5 weeks the CFD
	// pays more in total; both are below the buy-and-hold of the same bars.
	c := etf.costs()
	if math.Abs(c.FeePerLeg-15e-4) > 1e-12 || c.NightlyRate != nil || math.Abs(c.SpreadPerLeg-0.28/2/1e4) > 1e-15 {
		t.Fatalf("etf costs %+v", c)
	}
	if cc := cfd.costs(); cc.NightlyRate == nil || cc.FeePerLeg != 0 || math.Abs(cc.NightlyRate(b[0].Date)-2.3e-4) > 1e-12 {
		t.Fatalf("cfd costs %+v", cc)
	}
	if !(pe.Equity > pc.Equity) {
		t.Fatalf("over one held entry the ETF paper (%.5f) should end above the CFD paper (%.5f): financing", pe.Equity, pc.Equity)
	}
}

// A draft spec is decided and recorded in dry run but refused in demo mode
// even if main's flag check were bypassed.
func TestDraftSpecDryRunOnly(t *testing.T) {
	h := newHarness(t, false)
	rows := bars("2026-10-09", 0)
	fetch := func() ([]bitsodaily.Row, etorodaily.Report, error) { return rows, etorodaily.Report{}, nil }
	qqq := frozenSpecs["QQQ"]
	qqq.Draft, qqq.Prereg = true, "FORWARD-TEST-PREREGISTRATION-SMA50-ETF-FUTURE.md (DRAFT)"
	if err := runBook(context.Background(), h.o, h.de, qqq, fetch, monday, "test", etoroledger.ModeDryRun); err != nil {
		t.Fatalf("dry run of a draft spec: %v", err)
	}
	rec, ok := h.de.led.Get("qqq", "2026-10-09")
	if !ok || rec.Paper.Product != KindETF || rec.Paper.FeeBps != 15 || !strings.Contains(rec.Prereg, "DRAFT") {
		t.Fatalf("recorded %+v %v", rec, ok)
	}
	h2 := newHarness(t, true)
	err := runBook(context.Background(), h2.o, h2.de, qqq, fetch, monday, "test", etoroledger.ModeDemo)
	if err == nil || !strings.Contains(err.Error(), "draft") {
		t.Fatalf("demo on a draft spec must be refused, got %v", err)
	}
	if _, ok := h2.de.led.Get("qqq", "2026-10-09"); ok {
		t.Fatal("nothing must be recorded when the demo leg is refused")
	}
}
