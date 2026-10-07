package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Round trips count entries from flat; partial-weight changes are trades only.
func TestEntriesCountRoundTripsNotRebalances(t *testing.T) {
	d := mkDays([]float64{100, 100, 100, 100, 100, 100, 100}, nil)
	w := []float64{0.25, 0.5, 0, 0.75, 0.25, 0, 0} // fills days 1..6: two entries from flat, six trades
	r := simulate(d, w, 1, 6, 0.001)
	if r.entries != 2 || r.trades != 6 {
		t.Fatalf("entries %d trades %d, want 2 and 6", r.entries, r.trades)
	}
}

func TestStressPath(t *testing.T) {
	for _, c := range []struct {
		in   string
		bps  float64
		want string
	}{
		{"dir/weekly-research-btc-mxn.json", 88, "dir/weekly-research-btc-mxn-stress-88bps.json"},
		{"x", 87.5, "x-stress-87.5bps.json"},
	} {
		if got := stressPath(c.in, c.bps); got != c.want {
			t.Errorf("stressPath(%q, %v) = %q, want %q", c.in, c.bps, got, c.want)
		}
	}
}

// The report is built from the same rows as the text table, and vs_hold_pp is
// measured against buy-and-hold at the same costs.
func TestWindowReportFromRows(t *testing.T) {
	d := mkDays([]float64{100, 110, 121, 99, 130, 125, 140}, nil)
	strats := []strategy{
		{holdID, "buy-and-hold", []float64{1, 1, 1, 1, 1, 1, 1}},
		{"flat", "always flat", make([]float64, 7)},
	}
	rows := table(d, strats, 1, 6, 0.005)
	w := windowReport("HOLDOUT", d, 1, 6, rows)
	if w.Label != "HOLDOUT" || w.Bars != 6 || w.From != "2024-01-02" || w.To != "2024-01-07" {
		t.Fatalf("header %+v", w)
	}
	hold, flat := w.Results[0], w.Results[1]
	if hold.Rule != holdID || hold.VsHoldPP != 0 || hold.RoundTrips != 1 || hold.Random != nil {
		t.Fatalf("hold %+v", hold)
	}
	if !near(hold.ReturnPct, 100*rows[0].r.ret) || !near(hold.ReturnZeroCostPct, 100*rows[0].r0.ret) {
		t.Fatalf("hold returns %+v vs rows %+v", hold, rows[0])
	}
	if flat.ReturnPct != 0 || !near(flat.VsHoldPP, -hold.ReturnPct) || flat.Trades != 0 || flat.Label != "always flat" {
		t.Fatalf("flat %+v", flat)
	}
}

// The JSON carries every field research-run/v1 readers require (ui-api and
// web/src/api/schemas.ts), so a weekly report parses like a daily one.
func TestReportHasV1Fields(t *testing.T) {
	d := mkDays([]float64{100, 110, 121, 99, 130}, nil)
	rep := newReport(d, "btc_mxn", "/abs/path/btc_mxn_daily.csv", "2024-01-03", "2024-01-05", map[string]string{"csv": "btc_mxn_daily.csv"}, 70, "base")
	rep.Windows = append(rep.Windows, windowReport("DEVELOPMENT", d, 1, 4, table(d, []strategy{{holdID, "buy-and-hold", []float64{1, 1, 1, 1, 1}}}, 1, 4, 0.007)))
	p := filepath.Join(t.TempDir(), "r.json")
	if err := writeReport(p, rep); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schema", "tool", "generated_at", "flags", "data", "costs", "params", "windows"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %q", k)
		}
	}
	if m["schema"] != ReportSchema || m["tool"] != "weekly-research" {
		t.Errorf("schema %v tool %v", m["schema"], m["tool"])
	}
	data := m["data"].(map[string]any)
	if data["prices"] != "btc_mxn_daily.csv" || data["news_days"] != 0.0 {
		t.Errorf("data %v", data)
	}
	costs := m["costs"].(map[string]any)
	if costs["round_trip_bps"] != 140.0 || costs["slippage_bps"] != 0.0 || costs["level"] != "base" {
		t.Errorf("costs %v", costs)
	}
	for _, k := range []string{"sma", "news_window", "news_threshold", "sims", "seed"} {
		if _, ok := m["params"].(map[string]any)[k]; !ok {
			t.Errorf("params missing %q", k)
		}
	}
	res := m["windows"].([]any)[0].(map[string]any)["results"].([]any)[0].(map[string]any)
	for _, k := range []string{"rule", "return_pct", "round_trips", "exposure_pct", "max_dd_pct", "cost_pct", "vs_hold_pp", "random"} {
		if _, ok := res[k]; !ok {
			t.Errorf("result missing %q", k)
		}
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind")
	}
}
