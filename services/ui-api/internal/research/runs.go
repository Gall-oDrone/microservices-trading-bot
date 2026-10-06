package research

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// RunSchemaPrefix marks the research tools' JSON reports (daily-research
// -json writes "research-run/v1"). Other JSON files in an evidence directory
// are ignored.
const RunSchemaPrefix = "research-run/"

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ValidRunID reports whether date/name can identify a run.
func ValidRunID(date, name string) bool { return dateRe.MatchString(date) && ValidName(name) }

// Run summarises one report for the list. The full report is passed through
// unchanged by GetRun, so fields added to the schema reach the UI.
type Run struct {
	ID          string      `json:"id"`             // "<date>/<name>"
	Date        string      `json:"date"`           // the evidence directory's date
	Name        string      `json:"name"`           // file name without .json
	File        string      `json:"file"`           // repo-relative path
	Text        string      `json:"text,omitempty"` // repo-relative path of the .txt twin, when present
	Schema      string      `json:"schema"`
	Tool        string      `json:"tool"`
	GeneratedAt string      `json:"generated_at"`
	Commit      string      `json:"commit,omitempty"`
	Data        RunData     `json:"data"`
	Costs       RunCosts    `json:"costs"`
	Windows     []RunWindow `json:"windows"`
	Scores      []RuleScore `json:"scores"`  // per rule, in table order
	Studies     []string    `json:"studies"` // studies that cite this run's files (else its folder, else its date)
	report      []byte      // raw JSON, for GetRun
}

// RuleScore counts, for one rule, the windows it ran in and those where it
// returned more than buy-and-hold (vs_hold_pp > 0).
type RuleScore struct {
	Rule      string `json:"rule"`
	Windows   int    `json:"windows"`
	BeatsHold int    `json:"beats_hold"`
}

// RunData is the report's "data" object.
type RunData struct {
	Prices   string `json:"prices"`
	Book     string `json:"book,omitempty"`
	Bars     int    `json:"bars"`
	First    string `json:"first"`
	Last     string `json:"last"`
	News     string `json:"news,omitempty"`
	NewsDays int    `json:"news_days"`
}

// RunCosts is the report's "costs" object, in basis points.
type RunCosts struct {
	BuyBPS       float64 `json:"buy_bps"`
	SellBPS      float64 `json:"sell_bps"`
	SlippageBPS  float64 `json:"slippage_bps"`
	RoundTripBPS float64 `json:"round_trip_bps"`
}

// RunWindow is a window's header (no results) for the list.
type RunWindow struct {
	Label string `json:"label"`
	From  string `json:"from"`
	To    string `json:"to"`
	Bars  int    `json:"bars"`
}

// SkippedRun is an evidence JSON file that looked like a report but could not be read.
type SkippedRun struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

// RunDoc is one run with its full report.
type RunDoc struct {
	Run    Run             `json:"run"`
	Report json.RawMessage `json:"report"`
}

// runReportRaw is the subset of research-run/v1 the index needs.
type runReportRaw struct {
	Schema      string   `json:"schema"`
	Tool        string   `json:"tool"`
	GeneratedAt string   `json:"generated_at"`
	Commit      string   `json:"commit"`
	Data        RunData  `json:"data"`
	Costs       RunCosts `json:"costs"`
	Windows     []struct {
		RunWindow
		Results []struct {
			Rule     string  `json:"rule"`
			VsHoldPP float64 `json:"vs_hold_pp"`
		} `json:"results"`
	} `json:"windows"`
}

type runEntry struct {
	size int64
	mod  time.Time
	run  *Run
	err  error // not a report, or unreadable
}

// Runs lists every research-run report under the evidence-*/ directories,
// newest evidence date first. Files that are not reports (no research-run
// schema) are ignored; reports that fail to parse are returned as skipped.
func (x *Index) Runs() (runs []Run, skipped []SkippedRun, found bool, err error) {
	ents, err := x.load()
	if os.IsNotExist(err) {
		return []Run{}, []SkippedRun{}, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	runs, skipped, err = x.scanRuns(ents)
	return runs, skipped, true, err
}

// GetRun returns one report. ok is false when there is no such run.
func (x *Index) GetRun(date, name string) (doc *RunDoc, ok bool, err error) {
	if !ValidRunID(date, name) {
		return nil, false, nil
	}
	runs, _, found, err := x.Runs()
	if err != nil || !found {
		return nil, false, err
	}
	for _, r := range runs {
		if r.Date == date && r.Name == name {
			return &RunDoc{Run: r, Report: json.RawMessage(r.report)}, true, nil
		}
	}
	return nil, false, nil
}

func (x *Index) scanRuns(ents []*entry) ([]Run, []SkippedRun, error) {
	des, err := os.ReadDir(x.Dir)
	if err != nil {
		return nil, nil, err
	}
	type file struct{ date, name, abs string }
	var files []file
	txt := map[string]bool{} // "<date>/<name>" with a .txt twin
	for _, de := range des {
		date := strings.TrimPrefix(de.Name(), "evidence-")
		if !de.IsDir() || !strings.HasPrefix(de.Name(), "evidence-") || !dateRe.MatchString(date) {
			continue
		}
		fs, err := os.ReadDir(filepath.Join(x.Dir, de.Name()))
		if err != nil {
			continue
		}
		for _, f := range fs {
			n := f.Name()
			if !f.Type().IsRegular() {
				continue
			}
			if strings.HasSuffix(n, ".txt") {
				txt[date+"/"+strings.TrimSuffix(n, ".txt")] = true
			}
			if base := strings.TrimSuffix(n, ".json"); strings.HasSuffix(n, ".json") && ValidName(base) {
				files = append(files, file{date, base, filepath.Join(x.Dir, de.Name(), n)})
			}
		}
	}

	x.mu.Lock()
	defer x.mu.Unlock()
	if x.runCache == nil {
		x.runCache = map[string]*runEntry{}
	}
	seen := map[string]bool{}
	runs := []Run{}
	skipped := []SkippedRun{}
	for _, f := range files {
		seen[f.abs] = true
		info, err := os.Stat(f.abs)
		if err != nil {
			continue
		}
		e := x.runCache[f.abs]
		if e == nil || e.size != info.Size() || !e.mod.Equal(info.ModTime()) {
			e = &runEntry{size: info.Size(), mod: info.ModTime()}
			e.run, e.err = readRun(f.abs)
			x.runCache[f.abs] = e
		}
		rel := path.Join(x.RepoRel, "evidence-"+f.date, f.name+".json")
		if e.err != nil {
			skipped = append(skipped, SkippedRun{File: rel, Error: e.err.Error()})
			continue
		}
		if e.run == nil {
			continue // some other JSON file
		}
		r := *e.run
		r.ID, r.Date, r.Name, r.File = f.date+"/"+f.name, f.date, f.name, rel
		r.Text = ""
		if txt[r.ID] {
			r.Text = path.Join(x.RepoRel, "evidence-"+f.date, f.name+".txt")
		}
		r.Studies = citing(ents, f.date, f.name)
		runs = append(runs, r)
	}
	for k := range x.runCache {
		if !seen[k] {
			delete(x.runCache, k)
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].Date != runs[j].Date {
			return runs[i].Date > runs[j].Date
		}
		return runs[i].Name < runs[j].Name
	})
	return runs, skipped, nil
}

// readRun parses a report. It returns (nil, nil) for JSON that is not a
// research-run report, and an error for a report that cannot be used.
func readRun(abs string) (*Run, error) {
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var head struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(b, &head) != nil || !strings.HasPrefix(head.Schema, RunSchemaPrefix) {
		return nil, nil
	}
	var raw runReportRaw
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if len(raw.Windows) == 0 {
		return nil, fmt.Errorf("no windows")
	}
	r := &Run{
		Schema: raw.Schema, Tool: raw.Tool, GeneratedAt: raw.GeneratedAt, Commit: raw.Commit,
		Data: raw.Data, Costs: raw.Costs, Windows: []RunWindow{}, Scores: []RuleScore{},
		report: bytes.TrimSpace(b),
	}
	idx := map[string]int{}
	for _, w := range raw.Windows {
		r.Windows = append(r.Windows, w.RunWindow)
		for _, res := range w.Results {
			i, ok := idx[res.Rule]
			if !ok {
				i = len(r.Scores)
				idx[res.Rule] = i
				r.Scores = append(r.Scores, RuleScore{Rule: res.Rule})
			}
			r.Scores[i].Windows++
			if res.VsHoldPP > 0 {
				r.Scores[i].BeatsHold++
			}
		}
	}
	return r, nil
}

// citing returns the studies whose markdown links to the run's .json or .txt
// file; if none do, the studies that link to any file in the same evidence
// directory; failing that, the studies dated like the evidence directory.
func citing(ents []*entry, date, name string) []string {
	dir := "evidence-" + date + "/"
	base := dir + name
	match := func(ok func(e *entry) bool) []string {
		out := []string{}
		for _, e := range ents {
			if ok(e) {
				out = append(out, e.study.Name)
			}
		}
		sort.Strings(out)
		return out
	}
	if out := match(func(e *entry) bool {
		return bytes.Contains(e.src, []byte(base+".txt")) || bytes.Contains(e.src, []byte(base+".json"))
	}); len(out) > 0 {
		return out
	}
	if out := match(func(e *entry) bool { return bytes.Contains(e.src, []byte(dir)) }); len(out) > 0 {
		return out
	}
	return match(func(e *entry) bool { return e.study.Date == date })
}
