package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite testdata/golden/risk.json from the current run")

// goldenOptions runs the command on testdata/: the real demo ledger of
// 2026-10-13 (4 lines), the last 400 bars of each book's 2026-10-12
// candles, the three reconcile reports, no Yahoo history (embedded episode
// paths, eToro-only resampling), 200 paths.
func goldenOptions() options {
	return options{
		ledger: filepath.Join("testdata", "ledger.jsonl"), candles: filepath.Join("testdata", "candles"),
		reconcile: filepath.Join("testdata", "reconcile"), asOf: "2026-10-13",
		size: 1100, capital: 100000, paths: 200, horizon: 252, block: 20, seed: 1,
		nsdqSpread: 15.5, spxSpread: 14.8, overnight: 2.3, measuredSpread: 0.5, etfFee: 1.5,
		costsStatus: "FROZEN (test)",
	}
}

var goldenNow = time.Date(2026, 10, 13, 21, 0, 0, 0, time.UTC)

// TestGoldenRiskJSON diffs risk.json with the committed golden. Regenerate
// with: go test ./cmd/etoro-risk-report -run TestGoldenRiskJSON -update
func TestGoldenRiskJSON(t *testing.T) {
	rep, err := run(goldenOptions(), goldenNow)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden", "risk.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "risk.json"), got, 0o644)
		t.Fatalf("risk.json differs from %s (actual written to %s; rerun with -update if the change is intended)", path, dir)
	}
}

// The report's shape and a few facts pinned on the fixture: both books
// long, the 2026-10-12 fills with the ledger's own fill-vs-mid figures, the
// portfolio lag of 2026-10-12, embedded episodes, four Monte Carlo runs,
// and the markdown mentions each section.
func TestRunOnFixture(t *testing.T) {
	o := goldenOptions()
	rep, err := run(o, goldenNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Schema != Schema || rep.AsOf != "2026-10-13" || len(rep.Positions) != 2 || len(rep.VaR.Books) != 2 {
		t.Fatalf("%+v", rep)
	}
	for i, p := range rep.Positions {
		if p.State != "long" || p.Units <= 0 || p.LastDate != "2026-10-12" || rep.VaR.Books[i].Error != "" || rep.VaR.Books[i].Backtest.Observations != 250 {
			t.Fatalf("position %+v var %+v", p, rep.VaR.Books[i])
		}
	}
	if rep.Positions[0].Units != 0.035799 || rep.Positions[1].Units != 0.140735 || rep.Positions[0].LastClose != 30649.5 {
		t.Fatalf("positions %+v", rep.Positions)
	}
	if !rep.VaR.CorrelationOK || !(rep.VaR.ImpliedCorr > 0.5 && rep.VaR.ImpliedCorr < 1) || !(rep.VaR.VaR < rep.VaR.VaRUndiversified) || !rep.VaR.HistOK {
		t.Fatalf("portfolio %+v", rep.VaR)
	}
	if !strings.HasPrefix(rep.Stress.Source, "embedded") || rep.Stress.Worst != "2008-09-lehman" || len(rep.Stress.Episodes) != 9 || rep.Stress.ReverseStressOK {
		t.Fatalf("stress %+v", rep.Stress)
	}
	if len(rep.MonteCarlo) != 4 || rep.MonteCarlo[0].Scenario != "frozen" || rep.MonteCarlo[1].Scenario != "measured" || rep.MonteCarlo[0].HistorySrc != "etoro bars" {
		t.Fatalf("monte carlo %d runs", len(rep.MonteCarlo))
	}
	for _, m := range rep.MonteCarlo {
		if m.Summary.Config.Paths != 200 || m.Summary.HoldFinance.P50 <= 0 || m.Summary.PBeatsETFHold.P > m.Summary.PBeatsCFDHold.P {
			t.Fatalf("%s %s: %+v", m.Book, m.Scenario, m.Summary)
		}
	}
	// Same seed: the measured scenario's paths are the frozen one's, so the
	// ETF hold (same costs in both scenarios) is identical.
	if rep.MonteCarlo[0].Summary.ETFHoldReturn != rep.MonteCarlo[1].Summary.ETFHoldReturn {
		t.Fatal("scenarios on different paths")
	}
	a := rep.Attribution
	if len(a.Books) != 2 || a.Total.Net >= 0 || a.Realized.Net != 0 || a.Books[0].Lots[0].Nights != 1 || a.Books[0].FinancingCharged == nil {
		t.Fatalf("attribution %+v", a)
	}
	q := rep.FillQuality
	if len(q.Fills) != 2 || q.Fills[0].FillVsMidBps != -5.220709138192423 || !q.Fills[0].PortfolioLag || q.Fills[0].FillVsOpenBps == nil || q.All.PortfolioLags != 2 {
		t.Fatalf("fills %+v", q.Fills)
	}
	if q.Books[0].FrozenSpreadPerLegBps != 7.75 || q.Books[1].FrozenSpreadPerLegBps != 7.4 || q.All.MeasuredVsFrozen >= 0.1 {
		t.Fatalf("cost check %+v", q.All)
	}
	md := renderMarkdown(rep)
	for _, s := range []string{"## 1. Positions and VaR", "## 2. Stress", "## 3. Monte Carlo", "## 4. Attribution", "## 5. Fill quality", "## 6. Protection (M15)", "Held 2 (protected 0)", "2008-09-lehman", "3616683146", "FROZEN (test)"} {
		if !strings.Contains(md, s) {
			t.Errorf("markdown lacks %q", s)
		}
	}
	// Outputs land where asked.
	dir := t.TempDir()
	if err := writeOutputs(dir, rep, md); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"risk.json", "risk.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Error(err)
		}
	}
	// An older as-of: the bars after it are not used and the positions were
	// flat (the ledger's first demo day is 2026-10-09's decision, filled
	// 2026-10-12).
	o.asOf = "2026-10-09"
	old, err := run(o, goldenNow)
	if err != nil {
		t.Fatal(err)
	}
	if old.Positions[0].LastDate != "2026-10-09" || old.Positions[0].Exposure != 0 || old.VaR.VaR != 0 || len(old.FillQuality.Fills) != 0 {
		t.Fatalf("as of 2026-10-09: %+v", old.Positions)
	}
}

// Unreadable inputs are errors (the only non-zero exit).
func TestRunErrors(t *testing.T) {
	o := goldenOptions()
	o.ledger = filepath.Join("testdata", "missing.jsonl")
	if _, err := run(o, goldenNow); err == nil {
		t.Fatal("missing ledger accepted")
	}
	o = goldenOptions()
	o.candles = t.TempDir()
	if _, err := run(o, goldenNow); err == nil {
		t.Fatal("no candles accepted")
	}
	o = goldenOptions()
	o.yahoo = t.TempDir()
	if _, err := run(o, goldenNow); err == nil {
		t.Fatal("empty -yahoo dir accepted")
	}
}
