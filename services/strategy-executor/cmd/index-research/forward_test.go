package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"bitso-trading-platform/shared/pkg/yahoodaily"
)

// TestForwardDryRunGolden pins the procedure of
// FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md §5 on committed
// data: the already-seen year 2025-10-10..2026-10-09, eToro bars from
// docs/etoro/evidence-2026-10-10, Yahoo ETFs from
// docs/backtest-readiness/evidence-2026-10-10/data, provisional costs. The
// numbers are the dry run quoted in the pre-registration §4. If this test
// changes, the forward evaluation no longer does what was registered.
func TestForwardDryRunGolden(t *testing.T) {
	data := filepath.Join("..", "..", "..", "..", "docs", "backtest-readiness", "evidence-2026-10-10", "data")
	etoroDir := filepath.Join("..", "..", "..", "..", "docs", "etoro", "evidence-2026-10-10")
	if _, err := os.Stat(data); err != nil {
		t.Skipf("evidence not present: %v", err)
	}
	load := func(s string) []yahoodaily.Row {
		rows, err := yahoodaily.ReadCSV(filepath.Join(data, fileFor(s)))
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	irx := yahoodaily.NewSeries(load("^IRX"), func(r yahoodaily.Row) float64 { return r.Close / 100 })
	p := params{SMA: 50, OvernightBps: 2.3, SizeUSD: 1100, ETFFeeUSD: 1.5, Sims: 2000, Seed: 1, CostsStatus: "test",
		Pairs: []pair{
			{CFD: "NSDQ100", Index: "^NDX", ETF: "QQQ", SpreadRTBps: 15.5, ETFSpreadRTBps: 0.13},
			{CFD: "SPX500", Index: "^GSPC", ETF: "SPY", SpreadRTBps: 14.8, ETFSpreadRTBps: 0.26},
		}}
	out := t.TempDir()
	devnull, _ := os.Open(os.DevNull)
	stdout := os.Stdout
	os.Stdout = devnull
	err := runForward("2025-10-10:2026-10-09", etoroDir, out, load, irx, p)
	os.Stdout = stdout
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "forward.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep forwardReport
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}

	type want struct {
		trend, hold, etf float64 // primary-cost returns, %
		h1               bool
	}
	wants := map[string]want{
		"NSDQ100": {1.11, 13.81, 23.12, true},
		"SPX500":  {-5.21, 6.97, 16.76, false},
	}
	if len(rep.Pairs) != 2 {
		t.Fatalf("pairs = %d", len(rep.Pairs))
	}
	for _, pr := range rep.Pairs {
		w := wants[pr.CFD]
		if len(pr.Scenarios) != 2 || pr.Scenarios[0].Name != "primary" || pr.Scenarios[1].Name != "pessimistic" {
			t.Fatalf("%s: scenarios %+v", pr.CFD, pr.Scenarios)
		}
		sc := pr.Scenarios[0]
		if len(sc.Hyp) != 4 {
			t.Fatalf("%s: hypotheses %+v (note %q)", pr.CFD, sc.Hyp, sc.Note)
		}
		got := map[string]forwardHyp{}
		for _, h := range sc.Hyp {
			got[h.ID] = h
		}
		near := func(a, b float64) bool { return math.Abs(a-b) < 0.006 }
		if !near(got["H2"].Trend, w.trend) || !near(got["H2"].Bench, w.hold) || !near(got["H2b"].Bench, w.etf) {
			t.Errorf("%s: trend %.4f hold %.4f etf %.4f, want %.2f %.2f %.2f",
				pr.CFD, got["H2"].Trend, got["H2"].Bench, got["H2b"].Bench, w.trend, w.hold, w.etf)
		}
		if got["H1"].Pass != w.h1 || got["H2"].Pass || got["H2b"].Pass || got["H3"].Pass {
			t.Errorf("%s: verdicts %+v", pr.CFD, sc.Hyp)
		}
		// The pessimistic scenario must cost more than the primary one.
		pe := pr.Scenarios[1]
		if len(pe.Hyp) != 4 || pe.Hyp[1].Trend >= got["H2"].Trend || pe.Hyp[1].Bench >= got["H2"].Bench {
			t.Errorf("%s: pessimistic not costlier: %+v", pr.CFD, pe.Hyp)
		}
	}
}
