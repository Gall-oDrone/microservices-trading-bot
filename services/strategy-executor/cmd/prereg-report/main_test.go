package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type tw struct{ from, to, last string }

// writeRun writes a research-run/v1 file with one window.
func writeRun(t *testing.T, dir, name string, w tw, legBps float64, trend, hold ruleResult) string {
	t.Helper()
	r := map[string]any{
		"schema": "research-run/v1",
		"data":   map[string]any{"prices": "x", "last": w.last},
		"costs":  map[string]any{"buy_bps": legBps - 10, "sell_bps": legBps - 10, "slippage_bps": 10},
		"windows": []any{map[string]any{"from": w.from, "to": w.to, "bars": 10,
			"results": []ruleResult{hold, trend}}},
	}
	data, _ := json.Marshal(r)
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeCSV(t *testing.T, dir, name, book string, closes map[string]float64) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("date,book,open,high,low,close,volume,vwap,trade_count,bucket_start_utc\n")
	for _, d := range []string{"2026-09-28", "2026-09-29", "2026-09-30", "2026-10-01"} {
		c := closes[d]
		b.WriteString(d + "," + book + "," + ftoa(c) + "," + ftoa(c) + "," + ftoa(c) + "," + ftoa(c) + ",1,1,1," + d + "T06:00:00Z\n")
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func rnd(beat float64) *struct {
	Sims    int     `json:"sims"`
	BeatPct float64 `json:"beat_pct"`
} {
	return &struct {
		Sims    int     `json:"sims"`
		BeatPct float64 `json:"beat_pct"`
	}{Sims: 2000, BeatPct: beat}
}

func TestBuild_VerdictsAndMXNTerms(t *testing.T) {
	dir := t.TempDir()
	wm := tw{"2026-09-27", "2027-09-26", "2026-10-01"}
	wu := tw{"2026-09-29", "2027-09-26", "2026-10-01"}
	hold := ruleResult{Rule: "buy_and_hold", ReturnPct: 2, MaxDDPct: 10, RoundTrips: 1}
	in := inputs{
		// btc_mxn: smaller drawdown and higher return, but no completed round trip.
		mxnP: writeRun(t, dir, "mp.json", wm, 70, ruleResult{Rule: "trend_sma50", ReturnPct: 3, MaxDDPct: 5}, hold),
		mxnS: writeRun(t, dir, "ms.json", wm, 88, ruleResult{Rule: "trend_sma50", ReturnPct: 1, MaxDDPct: 5}, hold),
		// btc_usd: +10 % in USD, 96th percentile.
		usdP:   writeRun(t, dir, "up.json", wu, 40, ruleResult{Rule: "trend_sma50", ReturnPct: 10, MaxDDPct: 12, RoundTrips: 2, Random: rnd(96)}, ruleResult{Rule: "buy_and_hold", ReturnPct: 5, MaxDDPct: 11}),
		benchP: writeRun(t, dir, "bp.json", wu, 70, ruleResult{Rule: "trend_sma50"}, ruleResult{Rule: "buy_and_hold", ReturnPct: 8, MaxDDPct: 9}),
	}
	// USD/MXN from 18 (the close before 2026-09-29) to 19.8 (+10 %).
	usd := writeCSV(t, dir, "u.csv", "btc_usd", map[string]float64{"2026-09-28": 100, "2026-09-29": 100, "2026-09-30": 100, "2026-10-01": 100})
	mxn := writeCSV(t, dir, "m.csv", "btc_mxn", map[string]float64{"2026-09-28": 1800, "2026-09-29": 1850, "2026-09-30": 1900, "2026-10-01": 1980})
	fx, err := impliedFX(usd, mxn)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := build("as-of", in, fx, 60, 78)
	if err != nil {
		t.Fatal(err)
	}
	if rep.AsOf != "2026-10-01" || rep.Decides || !strings.Contains(rep.Note, "Nothing is decided") {
		t.Fatalf("report %+v", rep)
	}
	mx := rep.Books[0]
	if len(mx.Scenarios) != 2 || mx.Scenarios[0].LegBps != 70 {
		t.Fatalf("btc_mxn scenarios %+v", mx.Scenarios)
	}
	p := passes(mx.Scenarios[0])
	if !p["H1"] || !p["H2"] || p["H3"] || !strings.Contains(mx.Scenarios[0].Hyp[2].Note, "no completed round trip") {
		t.Fatalf("btc_mxn verdicts %+v", mx.Scenarios[0].Hyp)
	}
	if !strings.Contains(mx.Reading, "does not name") {
		t.Fatalf("btc_mxn reading %q", mx.Reading)
	}
	us := rep.Books[1]
	if len(us.Scenarios) != 1 { // no secondary inputs given
		t.Fatalf("btc_usd scenarios %d", len(us.Scenarios))
	}
	h := us.Scenarios[0].Hyp
	// 1.10 × 1.10 × (1 − 0.006)² − 1 = 19.55 % in MXN against 8 % holding btc_mxn.
	want := (1.1*1.1*0.994*0.994 - 1) * 100
	if h[0].Pass || !(h[1].Pass && h[1].Trend > want-1e-9 && h[1].Trend < want+1e-9) || !h[2].Pass {
		t.Fatalf("btc_usd verdicts %+v (want H2 %.4f)", h, want)
	}
	if !strings.Contains(us.Reading, "does not name") { // H2 without H1 is not one of the registered readings
		t.Fatalf("btc_usd reading %q", us.Reading)
	}
	var buf bytes.Buffer
	writeMarkdown(&buf, rep)
	for _, s := range []string{"progress report", "## btc_mxn", "### Primary costs (70 bps per leg, decides)", "### Secondary costs", "not a decision: window incomplete"} {
		if !strings.Contains(buf.String(), s) {
			t.Fatalf("markdown lacks %q:\n%s", s, buf.String())
		}
	}
	// A final run before the end date still decides nothing.
	if f, _ := build("final", in, fx, 60, 78); f.Decides || !strings.Contains(f.Note, "before 2027-09-26") {
		t.Fatalf("final before the date: %+v", f.Note)
	}
}

func TestBuild_RejectsMismatchedBenchmarkWindowAndBadSchema(t *testing.T) {
	dir := t.TempDir()
	w := tw{"2026-09-29", "2027-09-26", "2026-10-01"}
	tr := ruleResult{Rule: "trend_sma50", ReturnPct: 1}
	bh := ruleResult{Rule: "buy_and_hold"}
	usd := writeCSV(t, dir, "u.csv", "btc_usd", map[string]float64{"2026-09-28": 1, "2026-09-29": 1, "2026-09-30": 1, "2026-10-01": 1})
	fx, _ := impliedFX(usd, writeCSV(t, dir, "m.csv", "btc_mxn", map[string]float64{"2026-09-28": 18, "2026-09-29": 18, "2026-09-30": 18, "2026-10-01": 18}))
	in := inputs{mxnP: writeRun(t, dir, "m.json", w, 70, tr, bh), usdP: writeRun(t, dir, "u.json", w, 40, tr, bh),
		benchP: writeRun(t, dir, "b.json", tw{"2026-09-27", "2027-09-26", "2026-10-01"}, 70, tr, bh)}
	if _, err := build("as-of", in, fx, 60, 78); err == nil || !strings.Contains(err.Error(), "benchmark window") {
		t.Fatalf("want a window mismatch, got %v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{"schema":"other/v1","windows":[{}]}`), 0o644)
	in.benchP = bad
	if _, err := build("as-of", in, fx, 60, 78); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("want a schema error, got %v", err)
	}
}
