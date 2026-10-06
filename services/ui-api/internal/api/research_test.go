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
