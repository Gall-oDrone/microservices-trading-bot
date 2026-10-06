package api

import (
	"strings"
	"testing"

	"bitso-trading-platform/ui-api/internal/research"
)

func withStudies(dir string) func(*Server) {
	return func(s *Server) { s.Research = &research.Index{Dir: dir, RepoRel: "docs/test"} }
}

func TestStudiesList(t *testing.T) {
	ts := newTestServer(t, fixedNow, withStudies("testdata/studies"))
	resp := get[StudiesResponse](t, ts, "/api/ui/research/studies", 200)
	if !resp.Found || resp.Dir != "docs/test" || len(resp.Studies) != 2 {
		t.Fatalf("resp: %+v", resp)
	}
	b := resp.Studies[0]
	if b.Name != "BETA-REPORT-2026-09-21" || b.Kind != "report" || strings.Join(b.Follows, ",") != "ALPHA-STUDY-2026-09-20" {
		t.Errorf("newest first with follows: %+v", b)
	}
}

func TestStudyDetail(t *testing.T) {
	ts := newTestServer(t, fixedNow, withStudies("testdata/studies"))
	d := get[research.Doc](t, ts, "/api/ui/research/studies/ALPHA-STUDY-2026-09-20", 200)
	if d.Study.Title != "Alpha Study" || strings.Join(d.FollowedBy, ",") != "BETA-REPORT-2026-09-21" {
		t.Errorf("doc: %+v", d.Study)
	}
	if strings.Contains(d.HTML, "onerror") || !strings.Contains(d.HTML, "baseline study") {
		t.Errorf("raw HTML must be dropped: %s", d.HTML)
	}
	b := get[research.Doc](t, ts, "/api/ui/research/studies/BETA-REPORT-2026-09-21", 200)
	if len(b.Headings) != 1 || b.Headings[0].ID != "result" {
		t.Errorf("headings: %+v", b.Headings)
	}
}

func TestStudyErrors(t *testing.T) {
	ts := newTestServer(t, fixedNow, withStudies("testdata/studies"))
	get[errorBody](t, ts, "/api/ui/research/studies/NOPE-2026-01-01", 404)
	get[errorBody](t, ts, "/api/ui/research/studies/..%2Fledger", 400)
	get[errorBody](t, ts, "/api/ui/research/studies/a..b", 400)

	// No folder configured, or a missing folder: an empty list, not an error.
	none := newTestServer(t, fixedNow, nil)
	if r := get[StudiesResponse](t, none, "/api/ui/research/studies", 200); r.Found || r.Studies == nil || len(r.Studies) != 0 {
		t.Errorf("nil index: %+v", r)
	}
	get[errorBody](t, none, "/api/ui/research/studies/ALPHA-STUDY-2026-09-20", 404)
	missing := newTestServer(t, fixedNow, withStudies("testdata/nope"))
	if r := get[StudiesResponse](t, missing, "/api/ui/research/studies", 200); r.Found || len(r.Studies) != 0 {
		t.Errorf("missing dir: %+v", r)
	}
}

func TestRunsListAndDetail(t *testing.T) {
	ts := newTestServer(t, fixedNow, withStudies("testdata/studies"))
	resp := get[RunsResponse](t, ts, "/api/ui/research/runs", 200)
	if !resp.Found || len(resp.Runs) != 1 || len(resp.Skipped) != 0 {
		t.Fatalf("resp: %+v", resp)
	}
	r := resp.Runs[0]
	if r.ID != "2026-09-20/alpha-run" || r.Tool != "daily-research" || len(r.Windows) != 7 || strings.Join(r.Studies, ",") != "ALPHA-STUDY-2026-09-20" {
		t.Errorf("run: %+v", r)
	}
	d := get[research.RunDoc](t, ts, "/api/ui/research/runs/2026-09-20/alpha-run", 200)
	if d.Run.ID != r.ID || !strings.Contains(string(d.Report), `"research-run/v1"`) {
		t.Errorf("doc: %+v", d.Run)
	}
	get[errorBody](t, ts, "/api/ui/research/runs/2026-09-20/nope", 404)
	get[errorBody](t, ts, "/api/ui/research/runs/2026-9-20/alpha-run", 400)
	get[errorBody](t, ts, "/api/ui/research/runs/2026-09-20/a..b", 400)

	none := newTestServer(t, fixedNow, nil)
	if r := get[RunsResponse](t, none, "/api/ui/research/runs", 200); r.Found || r.Runs == nil || r.Skipped == nil {
		t.Errorf("nil index: %+v", r)
	}
	get[errorBody](t, none, "/api/ui/research/runs/2026-09-20/alpha-run", 404)
}
