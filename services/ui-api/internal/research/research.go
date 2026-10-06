// Package research indexes the study write-ups in docs/backtest-readiness
// (one markdown file per study) for the UI: metadata parsed from each file's
// header, and the body rendered to HTML.
//
// Read-only: files are read from disk, never written. Raw HTML in the
// markdown is not passed through (goldmark's default), so a document cannot
// inject markup into the UI.
package research

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Study is one document's metadata.
type Study struct {
	Name       string    `json:"name"`  // file name without .md; the URL id
	File       string    `json:"file"`  // repo-relative path
	Title      string    `json:"title"` // first "# " heading
	Date       string    `json:"date"`  // YYYY-MM-DD from "Date: **…**" or the file name; "" if none
	Kind       string    `json:"kind"`  // preregistration | assessment | study | report
	Question   string    `json:"question,omitempty"`
	Summary    string    `json:"summary"`
	Follows    []string  `json:"follows"`    // studies named in the Follows:/Context: header
	References []string  `json:"references"` // other studies linked from the body
	Evidence   *Evidence `json:"evidence"`
	Bytes      int64     `json:"bytes"`
	Modified   string    `json:"modified"`
}

// Evidence is the evidence-<date>/ directory next to a study, when present.
type Evidence struct {
	Dir   string   `json:"dir"`
	Files []string `json:"files"`
}

// Heading is an entry of a document's table of contents.
type Heading struct {
	Level int    `json:"level"`
	ID    string `json:"id"`
	Text  string `json:"text"`
}

// Doc is a rendered study.
type Doc struct {
	Study        Study     `json:"study"`
	HTML         string    `json:"html"`
	Headings     []Heading `json:"headings"`
	FollowedBy   []string  `json:"followed_by"`   // studies whose Follows:/Context: header names this one
	ReferencedBy []string  `json:"referenced_by"` // other studies that link to this one from their body
}

// Index reads studies from Dir. Entries are cached by file size and mtime.
type Index struct {
	Dir     string // e.g. docs/backtest-readiness
	RepoRel string // how File is reported, e.g. "docs/backtest-readiness"

	mu       sync.Mutex
	cache    map[string]*entry
	namesKey string // the set of names the cached HTML was rendered with
}

type entry struct {
	size  int64
	mod   time.Time
	study Study
	src   []byte
	doc   *Doc // rendered lazily
}

var (
	nameRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,150}$`)
	dateLineRe = regexp.MustCompile(`^Date(?: registered)?:\s*\*\*(\d{4}-\d{2}-\d{2})\*\*`)
	fileDateRe = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})$`)
	metaLineRe = regexp.MustCompile(`^[A-Z][A-Za-z ]{1,30}:\s`)
	mdLinkRe   = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)\)`)
	inlineRe   = strings.NewReplacer("**", "", "__", "", "`", "")
	listRe     = regexp.MustCompile(`^\d+\.\s`)
	schemeRe   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
	alertRe    = regexp.MustCompile(`<blockquote>\s*<p>\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*`)
)

// ValidName reports whether s can be a study name (no path separators).
func ValidName(s string) bool { return nameRe.MatchString(s) && !strings.Contains(s, "..") }

// List returns every study, newest first. README.md (the folder's index) is skipped.
// A missing directory is not an error: it returns found=false.
func (x *Index) List() (studies []Study, found bool, err error) {
	ents, err := x.load()
	if os.IsNotExist(err) {
		return []Study{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	out := make([]Study, 0, len(ents))
	x.mu.Lock()
	for _, e := range ents {
		out = append(out, e.study)
	}
	x.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date > out[j].Date
		}
		return out[i].Name < out[j].Name
	})
	return out, true, nil
}

// Get renders one study. ok is false when no such study exists.
func (x *Index) Get(name string) (doc *Doc, ok bool, err error) {
	if !ValidName(name) {
		return nil, false, nil
	}
	ents, err := x.load()
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	var e *entry
	followedBy, referencedBy := []string{}, []string{}
	for _, c := range ents {
		if c.study.Name == name {
			e = c
		}
		for _, f := range c.study.Follows {
			if f == name {
				followedBy = append(followedBy, c.study.Name)
			}
		}
		for _, f := range c.study.References {
			if f == name {
				referencedBy = append(referencedBy, c.study.Name)
			}
		}
	}
	if e == nil {
		return nil, false, nil
	}
	sort.Strings(followedBy)
	sort.Strings(referencedBy)
	if e.doc == nil {
		names := map[string]bool{}
		for _, c := range ents {
			names[c.study.Name] = true
		}
		html, heads, err := render(e.src, names, x.RepoRel)
		if err != nil {
			return nil, false, fmt.Errorf("render %s: %w", name, err)
		}
		e.doc = &Doc{Study: e.study, HTML: html, Headings: heads}
	}
	d := *e.doc
	d.Study = e.study // links and evidence are refreshed by load
	d.FollowedBy = followedBy
	d.ReferencedBy = referencedBy
	return &d, true, nil
}

// load refreshes the cache from disk and returns the current entries.
func (x *Index) load() ([]*entry, error) {
	des, err := os.ReadDir(x.Dir)
	if err != nil {
		return nil, err
	}
	evidence := map[string][]string{} // date -> files
	var mds []os.DirEntry
	for _, de := range des {
		n := de.Name()
		switch {
		case de.IsDir() && strings.HasPrefix(n, "evidence-"):
			fs, err := os.ReadDir(filepath.Join(x.Dir, n))
			if err != nil {
				continue
			}
			files := []string{}
			for _, f := range fs {
				if !f.IsDir() && !strings.HasPrefix(f.Name(), ".") {
					files = append(files, f.Name())
				}
			}
			evidence[strings.TrimPrefix(n, "evidence-")] = files
		case de.Type().IsRegular() && strings.HasSuffix(n, ".md") && n != "README.md":
			if ValidName(strings.TrimSuffix(n, ".md")) {
				mds = append(mds, de)
			}
		}
	}

	x.mu.Lock()
	defer x.mu.Unlock()
	if x.cache == nil {
		x.cache = map[string]*entry{}
	}
	names := map[string]bool{}
	for _, de := range mds {
		names[strings.TrimSuffix(de.Name(), ".md")] = true
	}
	keys := make([]string, 0, len(names))
	for n := range names {
		keys = append(keys, n)
	}
	sort.Strings(keys)
	if k := strings.Join(keys, ","); k != x.namesKey {
		// Study links in rendered HTML depend on which studies exist.
		x.namesKey = k
		for _, e := range x.cache {
			e.doc = nil
		}
	}
	out := make([]*entry, 0, len(mds))
	for _, de := range mds {
		name := strings.TrimSuffix(de.Name(), ".md")
		info, err := de.Info()
		if err != nil {
			continue
		}
		e := x.cache[name]
		if e == nil || e.size != info.Size() || !e.mod.Equal(info.ModTime()) {
			src, err := os.ReadFile(filepath.Join(x.Dir, de.Name()))
			if err != nil {
				return nil, err
			}
			e = &entry{size: info.Size(), mod: info.ModTime(), src: src}
			e.study = parseStudy(name, src)
			e.study.File = path.Join(x.RepoRel, de.Name())
			e.study.Bytes = info.Size()
			e.study.Modified = info.ModTime().UTC().Format(time.RFC3339)
			x.cache[name] = e
		}
		// Links and evidence depend on the other files, so recompute them each time.
		e.study.Follows = keep(e.study.Follows, names)
		e.study.References = keep(e.study.References, names)
		e.study.Evidence = nil
		if files, ok := evidence[e.study.Date]; ok && e.study.Date != "" {
			e.study.Evidence = &Evidence{Dir: path.Join(x.RepoRel, "evidence-"+e.study.Date), Files: files}
		}
		out = append(out, e)
	}
	for n := range x.cache {
		if !names[n] {
			delete(x.cache, n)
		}
	}
	return out, nil
}

// keep returns the members of xs that are known study names; never nil.
func keep(xs []string, names map[string]bool) []string {
	out := []string{}
	for _, x := range xs {
		if names[x] {
			out = append(out, x)
		}
	}
	return out
}

// kindOf classifies a study by its file name.
func kindOf(name string) string {
	switch {
	case strings.Contains(name, "PREREGISTRATION"):
		return "preregistration"
	case strings.Contains(name, "ASSESSMENT"):
		return "assessment"
	case strings.Contains(name, "STUDY"), strings.Contains(name, "CHECK"):
		return "study"
	}
	return "report"
}

// studyLinks returns the study names linked from s ("X.md" in the same dir), in order, unique.
func studyLinks(s, self string) []string {
	out := []string{}
	seen := map[string]bool{self: true}
	for _, m := range mdLinkRe.FindAllStringSubmatch(s, -1) {
		dst := strings.SplitN(m[2], "#", 2)[0]
		if !strings.HasSuffix(dst, ".md") || strings.Contains(dst, "/") {
			continue
		}
		n := strings.TrimSuffix(dst, ".md")
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// plain turns one markdown paragraph into plain text.
func plain(s string) string {
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = inlineRe.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[:n])
	if i := strings.LastIndex(cut, " "); i > n/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:") + "…"
}

// parseStudy reads the metadata a study's markdown carries by convention:
// a "# " title, header lines ("Date: **…**", "Follows: […](….md)"), then prose.
func parseStudy(name string, src []byte) Study {
	st := Study{Name: name, Kind: kindOf(name), Follows: []string{}, References: []string{}}
	if m := fileDateRe.FindStringSubmatch(name); m != nil {
		st.Date = m[1]
	}
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	var blocks [][]string
	var cur []string
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if strings.TrimSpace(line) == "" && !inFence {
			if len(cur) > 0 {
				blocks = append(blocks, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		blocks = append(blocks, cur)
	}

	var followsSrc strings.Builder
	takeNext := false
	for _, b := range blocks {
		first := strings.TrimSpace(b[0])
		joined := strings.Join(b, "\n")
		switch {
		case st.Title == "" && strings.HasPrefix(first, "# "):
			st.Title = plain(strings.TrimPrefix(first, "# "))
			continue
		case strings.HasPrefix(first, "#"), strings.HasPrefix(first, "```"), strings.HasPrefix(first, "|"),
			strings.HasPrefix(first, "---"):
			continue
		}
		// Header lines: "Key: value" lines right under the title.
		if metaLineRe.MatchString(first) && !strings.HasPrefix(first, "**") {
			for _, l := range b {
				l = strings.TrimSpace(l)
				if m := dateLineRe.FindStringSubmatch(l); m != nil {
					st.Date = m[1]
				}
				if strings.HasPrefix(l, "Follows:") || strings.HasPrefix(l, "Context:") {
					followsSrc.WriteString(l + "\n")
				}
			}
			continue
		}
		if strings.HasPrefix(first, ">") {
			if st.Question == "" && !strings.HasPrefix(first, "> [!") {
				var q []string
				for _, l := range b {
					q = append(q, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), ">")))
				}
				st.Question = clip(plain(strings.Join(q, " ")), 300)
			}
			continue
		}
		p := plain(joined)
		if strings.HasPrefix(first, "**Question.**") {
			if st.Question == "" {
				st.Question = clip(strings.TrimSpace(strings.TrimPrefix(p, "Question.")), 300)
			}
			continue
		}
		if st.Summary != "" {
			continue
		}
		isList := strings.HasPrefix(first, "- ") || strings.HasPrefix(first, "* ") || listRe.MatchString(first)
		switch {
		case takeNext:
			st.Summary = clip(p, 400)
		case isList:
			// Lists are rarely a summary on their own.
		case len(p) < 40 && strings.HasSuffix(p, "."):
			// A lead-in such as "**Short answer.**": the next block is the summary.
			takeNext = true
		default:
			st.Summary = clip(p, 400)
		}
	}
	st.Follows = studyLinks(followsSrc.String(), name)
	follows := map[string]bool{}
	for _, f := range st.Follows {
		follows[f] = true
	}
	for _, r := range studyLinks(text, name) {
		if !follows[r] {
			st.References = append(st.References, r)
		}
	}
	if st.Title == "" {
		st.Title = name
	}
	return st
}

// linkRewriter points links to other studies at the UI route /research/<name>
// and marks other relative links as local files (the UI does not serve them).
// It also drops the document's leading "# Title": the UI shows the title from
// the metadata, and a page has one h1.
type linkRewriter struct {
	names   map[string]bool
	repoRel string
}

func (t linkRewriter) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	if h, ok := doc.FirstChild().(*ast.Heading); ok && h.Level == 1 {
		doc.RemoveChild(doc, h)
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		l, ok := n.(*ast.Link)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}
		dst := string(l.Destination)
		switch {
		case strings.HasPrefix(dst, "#"):
		case strings.HasPrefix(dst, "http://"), strings.HasPrefix(dst, "https://"), strings.HasPrefix(dst, "mailto:"):
			l.SetAttributeString("target", []byte("_blank"))
			l.SetAttributeString("rel", []byte("noopener noreferrer"))
		case schemeRe.MatchString(dst):
			// Any other scheme (javascript:, data:, file:): goldmark blanks the
			// dangerous ones; never copy them into an attribute.
		default:
			file, frag, _ := strings.Cut(dst, "#")
			n := strings.TrimSuffix(file, ".md")
			if strings.HasSuffix(file, ".md") && !strings.Contains(file, "/") && t.names[n] {
				dst = "/research/" + n
				if frag != "" {
					dst += "#" + frag
				}
				l.Destination = []byte(dst)
				l.SetAttributeString("data-study", []byte(n))
			} else {
				l.SetAttributeString("data-local", []byte(path.Join(t.repoRel, file)))
			}
		}
		return ast.WalkContinue, nil
	})
}

func render(src []byte, names map[string]bool, repoRel string) (string, []Heading, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithAttribute(),
			parser.WithASTTransformers(
				// After the default transformers, so link destinations are final.
				util.Prioritized(linkRewriter{names: names, repoRel: repoRel}, 1000),
			),
		),
	)
	reader := text.NewReader(src)
	doc := md.Parser().Parse(reader)
	var heads []Heading
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !ok || !entering || h.Level < 2 || h.Level > 3 {
			return ast.WalkContinue, nil
		}
		id, _ := h.AttributeString("id")
		idb, _ := id.([]byte)
		heads = append(heads, Heading{Level: h.Level, ID: string(idb), Text: plain(nodeText(h, src))})
		return ast.WalkSkipChildren, nil
	})
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, src, doc); err != nil {
		return "", nil, err
	}
	html := alertRe.ReplaceAllStringFunc(buf.String(), func(m string) string {
		kind := strings.ToLower(alertRe.FindStringSubmatch(m)[1])
		return `<blockquote class="alert ` + kind + `"><p class="alert-title">` + kind + `</p><p>`
	})
	if heads == nil {
		heads = []Heading{}
	}
	return html, heads, nil
}

// nodeText concatenates the text segments under n.
func nodeText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(src))
		case *ast.String:
			b.Write(t.Value)
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}
