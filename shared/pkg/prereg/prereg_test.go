package prereg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path string, r Report) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(r)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLatest(t *testing.T) {
	jobs, docs := t.TempDir(), t.TempDir()
	write(t, filepath.Join(docs, "evidence-2026-10-09", "prereg-as-of", "report.json"),
		Report{Schema: Schema, Phase: "as-of", AsOf: "2026-10-08", GeneratedAt: "2026-10-09T21:31:27Z", Books: []BookVerdict{{Book: "btc_mxn"}}})
	write(t, filepath.Join(jobs, "2026-10-12-as-of", "report.json"),
		Report{Schema: Schema, Phase: "as-of", AsOf: "2026-10-11", GeneratedAt: "2026-10-12T07:30:00Z", Books: []BookVerdict{{Book: "btc_mxn"}, {Book: "btc_usd"}}})
	write(t, filepath.Join(jobs, "2026-10-12-other", "report.json"), Report{Schema: "other/v1", AsOf: "2099-01-01"})
	os.WriteFile(filepath.Join(jobs, "broken.json"), []byte("{"), 0o644)

	r, path, found, skipped, err := Latest([]string{jobs, docs, filepath.Join(jobs, "missing")})
	if err != nil || !found || r.AsOf != "2026-10-11" || skipped != 1 || filepath.Base(filepath.Dir(path)) != "2026-10-12-as-of" {
		t.Fatalf("latest %+v %s found=%v skipped=%d err=%v", r, path, found, skipped, err)
	}
	if _, ok := r.Book("btc_usd"); !ok {
		t.Fatal("btc_usd verdict")
	}
	if _, ok := r.Book("eth_mxn"); ok {
		t.Fatal("no eth_mxn")
	}
	// On the same data, a final outranks an as-of run.
	write(t, filepath.Join(jobs, "2026-10-12-final", "report.json"), Report{Schema: Schema, Phase: "final", AsOf: "2026-10-11", GeneratedAt: "2026-10-12T07:00:00Z"})
	if r, _, _, _, _ := Latest([]string{jobs}); r.Phase != "final" {
		t.Fatalf("phase %s", r.Phase)
	}
	if _, _, found, _, err := Latest([]string{t.TempDir()}); found || err != nil {
		t.Fatalf("empty root: found=%v err=%v", found, err)
	}
}
