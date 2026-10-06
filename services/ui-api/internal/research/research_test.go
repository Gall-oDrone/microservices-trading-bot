package research

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const first = `# First Study — Baseline

Date: **2026-09-20**  
Repository: ` + "`repo`" + `  

This is the first study. It sets a baseline for everything that follows in this folder.

| a | b |
|---|---|
| 1 | 2 |
`

const second = `# Second Study

Date: **2026-09-21**  
Follows: [` + "`FIRST-STUDY-2026-09-20.md`" + `](FIRST-STUDY-2026-09-20.md)

> At a daily horizon, after costs, does a rule beat holding?

The second study asks a sharper question and links to [the code](../../services/x/main.go) and
[a missing file](MISSING-2026-01-01.md) and [an external page](https://example.com).

> [!IMPORTANT]
> **Frozen.** Do not edit.

## 1. Method

Some text.

### 1.1 Detail

<script>alert(1)</script>

## 2. Result

See [the baseline, section](FIRST-STUDY-2026-09-20.md#a).
`

const third = `# Volume Study (2026-10-03)

**Question.** Can volume beat fees **every week**?

**Short answer.**

1. No strategy makes money every week.
2. Volume spikes lose after fees.

Builds on [second](SECOND-STUDY-2026-09-21.md).
`

func writeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"FIRST-STUDY-2026-09-20.md":                    first,
		"SECOND-STUDY-2026-09-21.md":                   second,
		"VOLUME-STUDY-2026-10-03.md":                   third,
		"FORWARD-TEST-PREREGISTRATION-X-2026-09-27.md": "# Prereg\n\nDate registered: **2026-09-27**  \n\nThis file fixes what will be tested before any forward data exists.\n",
		"README.md": "# Index\n\nNot a study.\n",
		"notes.txt": "ignored",
	}
	for n, s := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ev := filepath.Join(dir, "evidence-2026-09-21")
	if err := os.Mkdir(ev, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"run.txt", ".hidden"} {
		if err := os.WriteFile(filepath.Join(ev, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func byName(t *testing.T, ss []Study, name string) Study {
	t.Helper()
	for _, s := range ss {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no study %s in %v", name, ss)
	return Study{}
}

func TestListParsesMetadata(t *testing.T) {
	x := &Index{Dir: writeDir(t), RepoRel: "docs/br"}
	ss, found, err := x.List()
	if err != nil || !found {
		t.Fatalf("List: %v found=%v", err, found)
	}
	if len(ss) != 4 {
		t.Fatalf("want 4 studies (README and .txt skipped), got %d", len(ss))
	}
	if ss[0].Name != "VOLUME-STUDY-2026-10-03" || ss[3].Name != "FIRST-STUDY-2026-09-20" {
		t.Errorf("want newest first, got %s … %s", ss[0].Name, ss[3].Name)
	}

	f := byName(t, ss, "FIRST-STUDY-2026-09-20")
	if f.Title != "First Study — Baseline" || f.Date != "2026-09-20" || f.Kind != "study" {
		t.Errorf("first: %+v", f)
	}
	if !strings.HasPrefix(f.Summary, "This is the first study.") || f.File != "docs/br/FIRST-STUDY-2026-09-20.md" {
		t.Errorf("first summary/file: %q %q", f.Summary, f.File)
	}
	if f.Evidence != nil {
		t.Errorf("first has no evidence dir, got %+v", f.Evidence)
	}

	s := byName(t, ss, "SECOND-STUDY-2026-09-21")
	if s.Question != "At a daily horizon, after costs, does a rule beat holding?" {
		t.Errorf("question from blockquote: %q", s.Question)
	}
	if strings.Contains(s.Summary, "Frozen") || !strings.HasPrefix(s.Summary, "The second study asks") {
		t.Errorf("summary must skip the question and alerts: %q", s.Summary)
	}
	if strings.Join(s.Follows, ",") != "FIRST-STUDY-2026-09-20" || len(s.References) != 0 {
		t.Errorf("follows %v (missing files dropped), references %v", s.Follows, s.References)
	}
	if s.Evidence == nil || s.Evidence.Dir != "docs/br/evidence-2026-09-21" || strings.Join(s.Evidence.Files, ",") != "run.txt" {
		t.Errorf("evidence: %+v", s.Evidence)
	}

	v := byName(t, ss, "VOLUME-STUDY-2026-10-03")
	if v.Question != "Can volume beat fees every week?" {
		t.Errorf("question from **Question.**: %q", v.Question)
	}
	if v.Summary != "1. No strategy makes money every week. 2. Volume spikes lose after fees." {
		t.Errorf("summary after a short lead-in: %q", v.Summary)
	}
	if strings.Join(v.References, ",") != "SECOND-STUDY-2026-09-21" {
		t.Errorf("references: %v", v.References)
	}

	p := byName(t, ss, "FORWARD-TEST-PREREGISTRATION-X-2026-09-27")
	if p.Kind != "preregistration" || p.Date != "2026-09-27" {
		t.Errorf("prereg: %+v", p)
	}
}

func TestGetRendersSafeHTML(t *testing.T) {
	x := &Index{Dir: writeDir(t), RepoRel: "docs/br"}
	d, ok, err := x.Get("SECOND-STUDY-2026-09-21")
	if err != nil || !ok {
		t.Fatalf("Get: %v ok=%v", err, ok)
	}
	h := d.HTML
	for _, want := range []string{
		`<a href="/research/FIRST-STUDY-2026-09-20" data-study="FIRST-STUDY-2026-09-20">`,
		`<a href="/research/FIRST-STUDY-2026-09-20#a" data-study="FIRST-STUDY-2026-09-20">`,
		`data-local="services/x/main.go"`,
		`data-local="docs/br/MISSING-2026-01-01.md"`,
		`target="_blank" rel="noopener noreferrer"`,
		`<blockquote class="alert important"><p class="alert-title">important</p>`,
		`<h2 id="1-method">`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("HTML lacks %s", want)
		}
	}
	if strings.Contains(h, "<h1") {
		t.Errorf("the leading title must be dropped (the UI shows it from metadata)")
	}
	if strings.Contains(h, "<script") || strings.Contains(h, "[!IMPORTANT]") {
		t.Errorf("raw HTML or alert marker passed through:\n%s", h)
	}
	if len(d.Headings) != 3 || d.Headings[0] != (Heading{2, "1-method", "1. Method"}) || d.Headings[1].Level != 3 {
		t.Errorf("headings: %+v", d.Headings)
	}
	if strings.Join(d.ReferencedBy, ",") != "VOLUME-STUDY-2026-10-03" || len(d.FollowedBy) != 0 {
		t.Errorf("followed_by %v referenced_by %v", d.FollowedBy, d.ReferencedBy)
	}

	f, _, _ := x.Get("FIRST-STUDY-2026-09-20")
	if !strings.Contains(f.HTML, "<table>") {
		t.Errorf("GFM table not rendered")
	}
	if strings.Join(f.FollowedBy, ",") != "SECOND-STUDY-2026-09-21" {
		t.Errorf("first followed_by: %v", f.FollowedBy)
	}
}

func TestGetRejectsUnknownAndTraversal(t *testing.T) {
	x := &Index{Dir: writeDir(t), RepoRel: "docs/br"}
	for _, n := range []string{"README", "notes", "../etc/passwd", "..", "a/b", "", "NOPE-2026-01-01"} {
		if d, ok, err := x.Get(n); ok || d != nil || err != nil {
			t.Errorf("Get(%q) = %v, %v, %v; want not found", n, d != nil, ok, err)
		}
	}
}

func TestCacheRefreshesOnChange(t *testing.T) {
	dir := writeDir(t)
	x := &Index{Dir: dir, RepoRel: "docs/br"}
	if _, _, err := x.Get("FIRST-STUDY-2026-09-20"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "FIRST-STUDY-2026-09-20.md")
	if err := os.WriteFile(p, []byte("# Renamed\n\nNew body text that is long enough to be a summary.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	d, _, _ := x.Get("FIRST-STUDY-2026-09-20")
	if d.Study.Title != "Renamed" || !strings.Contains(d.HTML, "New body text") {
		t.Errorf("cache not refreshed: %q", d.Study.Title)
	}
	if err := os.Remove(filepath.Join(dir, "SECOND-STUDY-2026-09-21.md")); err != nil {
		t.Fatal(err)
	}
	ss, _, _ := x.List()
	if len(ss) != 3 {
		t.Errorf("removed file still listed: %d", len(ss))
	}
	v := byName(t, ss, "VOLUME-STUDY-2026-10-03")
	if len(v.References) != 0 {
		t.Errorf("link to a removed study kept: %v", v.References)
	}
}

func TestMissingDir(t *testing.T) {
	x := &Index{Dir: filepath.Join(t.TempDir(), "nope")}
	ss, found, err := x.List()
	if err != nil || found || ss == nil || len(ss) != 0 {
		t.Errorf("List on a missing dir: %v %v %v", ss, found, err)
	}
	if _, ok, err := x.Get("X-2026-01-01"); ok || err != nil {
		t.Errorf("Get on a missing dir: %v %v", ok, err)
	}
}

func TestRenderDropsDangerousLinks(t *testing.T) {
	dir := t.TempDir()
	src := "# X\n\n[js](javascript:alert(1)) [data](data:text/html,hi) [ok](https://example.com)\n"
	if err := os.WriteFile(filepath.Join(dir, "X-2026-01-01.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	d, ok, err := (&Index{Dir: dir}).Get("X-2026-01-01")
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if strings.Contains(d.HTML, "javascript:") || strings.Contains(d.HTML, "data:text") {
		t.Errorf("dangerous URL rendered: %s", d.HTML)
	}
	if !strings.Contains(d.HTML, `href="https://example.com"`) {
		t.Errorf("safe link lost: %s", d.HTML)
	}
}
