package research

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const runJSON = `{
  "schema": "research-run/v1",
  "tool": "daily-research",
  "generated_at": "2026-10-06T19:00:00Z",
  "commit": "abc123",
  "flags": {"windows": "2025-01-01:2025-12-31,2026-01-01:2026-09-26"},
  "data": {"prices": "bitso", "bars": 3406, "first": "2017-05-31", "last": "2026-09-26", "news_days": 0},
  "costs": {"buy_bps": 78, "sell_bps": 78, "slippage_bps": 10, "round_trip_bps": 176},
  "params": {"sma": 50, "news_window": 3, "news_threshold": 0, "sims": 2000, "seed": 1},
  "future_field": {"kept": true},
  "windows": [
    {"label": "IN-SAMPLE", "from": "2025-01-01", "to": "2025-12-31", "bars": 365, "results": [
      {"rule": "buy_and_hold", "return_pct": -20.7, "round_trips": 1, "exposure_pct": 100, "max_dd_pct": 30, "cost_pct": 1.7, "vs_hold_pp": 0, "random": null},
      {"rule": "trend_sma50", "return_pct": -10.1, "round_trips": 9, "exposure_pct": 40, "max_dd_pct": 20, "cost_pct": 15, "vs_hold_pp": 10.6, "random": {"sims": 2000, "beat_pct": 61.2}}
    ]},
    {"label": "OUT-OF-SAMPLE", "from": "2026-01-01", "to": "2026-09-26", "bars": 269, "results": [
      {"rule": "buy_and_hold", "return_pct": 5, "round_trips": 1, "exposure_pct": 100, "max_dd_pct": 25, "cost_pct": 1.8, "vs_hold_pp": 0, "random": null},
      {"rule": "trend_sma50", "return_pct": 2, "round_trips": 7, "exposure_pct": 45, "max_dd_pct": 12, "cost_pct": 12, "vs_hold_pp": -3, "random": {"sims": 2000, "beat_pct": 40}}
    ]}
  ]
}`

// citer links to one run's text twin; first (dated 2026-09-20) does not.
const citer = `# Citer

Date: **2026-09-22**

Numbers from [the run](evidence-2026-09-20/run-a.txt).
`

func writeRunsFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("FIRST-STUDY-2026-09-20.md", first)
	write("SECOND-STUDY-2026-09-21.md", second)
	write("CITER-2026-09-22.md", citer)
	write("evidence-2026-09-20/run-a.json", runJSON)
	write("evidence-2026-09-20/run-a.txt", "DATA ...\n")
	runB := strings.Replace(runJSON, `"commit": "abc123"`, `"commit": "def456"`, 1)
	runB = strings.Replace(runB, `"round_trip_bps": 176}`, `"round_trip_bps": 176, "level": "stress", "note": "per leg"}`, 1)
	write("evidence-2026-09-20/run-b.json", runB)
	write("evidence-2026-09-20/notes.json", `{"hello": "not a report"}`)
	write("evidence-2026-09-20/broken.json", `{"schema": "research-run/v1", "windows": "nope"}`)
	write("evidence-2026-09-20/empty.json", `{"schema": "research-run/v1", "windows": []}`)
	write("evidence-2026-09-21/run-c.json", runJSON)
	write("evidence-oops/run-d.json", runJSON) // not a dated evidence dir
	return dir
}

func TestRuns_ListSummariesCitationsAndSkips(t *testing.T) {
	x := &Index{Dir: writeRunsFixture(t), RepoRel: "docs/r"}
	runs, skipped, found, err := x.Runs()
	if err != nil || !found {
		t.Fatalf("runs: %v found=%v", err, found)
	}
	var ids []string
	for _, r := range runs {
		ids = append(ids, r.ID)
	}
	if got := strings.Join(ids, ","); got != "2026-09-21/run-c,2026-09-20/run-a,2026-09-20/run-b" {
		t.Fatalf("ids (newest date first, then name) = %s", got)
	}
	a := runs[1]
	if a.File != "docs/r/evidence-2026-09-20/run-a.json" || a.Text != "docs/r/evidence-2026-09-20/run-a.txt" {
		t.Errorf("paths: %+v", a)
	}
	if a.Commit != "abc123" || a.Costs.RoundTripBPS != 176 || a.Data.Bars != 3406 || len(a.Windows) != 2 || a.Windows[1].Label != "OUT-OF-SAMPLE" {
		t.Errorf("summary: %+v", a)
	}
	if fmt.Sprint(a.Scores) != "[{buy_and_hold 2 0} {trend_sma50 2 1}]" {
		t.Errorf("scores: %v", a.Scores)
	}
	if strings.Join(a.Studies, ",") != "CITER-2026-09-22" {
		t.Errorf("run-a is cited by CITER: %v", a.Studies)
	}
	if b := runs[2]; b.Text != "" || strings.Join(b.Studies, ",") != "CITER-2026-09-22" {
		t.Errorf("run-b: no twin and not cited; CITER cites its folder: %+v", b)
	}
	if b := runs[2]; b.Costs.Level != "stress" || b.Costs.Note != "per leg" || a.Costs.Level != "" {
		t.Errorf("additive cost level/note pass through: a %+v b %+v", a.Costs, b.Costs)
	}
	if c := runs[0]; strings.Join(c.Studies, ",") != "SECOND-STUDY-2026-09-21" {
		t.Errorf("run-c: folder not cited at all, falls back to the study with its date: %v", c.Studies)
	}
	var sk []string
	for _, s := range skipped {
		sk = append(sk, filepath.Base(s.File))
	}
	if strings.Join(sk, ",") != "broken.json,empty.json" {
		t.Errorf("skipped = %v (notes.json is not a report and is ignored)", skipped)
	}
}

func TestGetRun_PassesTheReportThrough(t *testing.T) {
	x := &Index{Dir: writeRunsFixture(t), RepoRel: "docs/r"}
	d, ok, err := x.GetRun("2026-09-20", "run-a")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	var rep map[string]any
	if err := json.Unmarshal(d.Report, &rep); err != nil {
		t.Fatal(err)
	}
	if rep["future_field"] == nil {
		t.Errorf("fields unknown to ui-api must reach the UI: %v", rep)
	}
	for _, bad := range [][2]string{{"2026-09-20", "nope"}, {"2026-9-20", "run-a"}, {"2026-09-20", "../run-a"}, {"..", "run-a"}, {"2026-09-20", "broken"}, {"2026-09-20", "notes"}} {
		if _, ok, err := x.GetRun(bad[0], bad[1]); ok || err != nil {
			t.Errorf("GetRun(%q, %q) = ok %v, err %v", bad[0], bad[1], ok, err)
		}
	}
}

func TestRuns_CacheRefreshAndMissingDir(t *testing.T) {
	dir := writeRunsFixture(t)
	x := &Index{Dir: dir, RepoRel: "docs/r"}
	if _, _, _, err := x.Runs(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "evidence-2026-09-20", "run-a.json")
	if err := os.WriteFile(p, []byte(strings.Replace(runJSON, "abc123", "fff999", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(p, future, future)
	runs, _, _, _ := x.Runs()
	if runs[1].Commit != "fff999" {
		t.Errorf("changed file not re-read: %+v", runs[1])
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if runs, _, _, _ := x.Runs(); len(runs) != 2 {
		t.Errorf("deleted file still listed: %d runs", len(runs))
	}

	missing := &Index{Dir: filepath.Join(dir, "nope")}
	runs, skipped, found, err := missing.Runs()
	if err != nil || found || runs == nil || skipped == nil {
		t.Errorf("missing dir: runs=%v skipped=%v found=%v err=%v", runs, skipped, found, err)
	}
}
