package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"bitso-trading-platform/shared/pkg/prereg"
)

func TestPreregReport(t *testing.T) {
	root := t.TempDir()
	rep := prereg.Report{Schema: prereg.Schema, Phase: "as-of", AsOf: "2026-10-08", GeneratedAt: "2026-10-09T21:31:27Z",
		Note: "progress", Books: []prereg.BookVerdict{{Book: "btc_mxn", Reading: "r", Scenarios: []prereg.Scenario{{Name: "primary",
			Hyp: []prereg.Hypothesis{{ID: "H1"}, {ID: "H2"}, {ID: "H3"}}}}}}}
	dir := filepath.Join(root, "evidence-2026-10-09", "prereg-as-of")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(rep)
	if err := os.WriteFile(filepath.Join(dir, "report.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, fixedNow, func(s *Server) { s.PreregDirs = []string{filepath.Join(root, "missing"), root} })
	p := get[PreregResponse](t, ts, "/api/ui/forward-tests/btc_mxn/prereg", 200)
	if !p.Found || p.Source != "evidence-2026-10-09/prereg-as-of/report.json" || p.AsOf != "2026-10-08" || p.Decides || p.Verdict == nil || len(p.Verdict.Scenarios[0].Hyp) != 3 {
		t.Fatalf("prereg %+v", p)
	}
	// fixedNow is 2026-10-02: 175 days to the interim look, 359 to the evaluation.
	if p.InterimDate != "2027-03-26" || p.DaysToInterim != 175 || p.DaysToFinal != 359 {
		t.Fatalf("dates %+v", p)
	}
	if u := get[PreregResponse](t, ts, "/api/ui/forward-tests/btc_usd/prereg", 200); !u.Found || u.Verdict != nil {
		t.Fatalf("btc_usd: %+v", u)
	}
	if n := get[PreregResponse](t, newTestServer(t, fixedNow, nil), "/api/ui/forward-tests/btc_mxn/prereg", 200); n.Found || n.Verdict != nil {
		t.Fatalf("no dirs: %+v", n)
	}
	get[map[string]any](t, ts, "/api/ui/forward-tests/BAD/prereg", 400)
}
