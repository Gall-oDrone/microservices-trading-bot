// Package prereg holds the pre-registered verdict report written by
// strategy-executor's cmd/prereg-report (schema prereg-report/v1) and read by
// ui-api, so both sides share one definition (plan §6.4.13–§6.4.14).
// Changes are additive only: never rename or remove a field.
package prereg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Schema names the JSON.
const Schema = "prereg-report/v1"

// The frozen evaluation dates (both SMA50 pre-registrations).
const (
	InterimDate = "2027-03-26"
	FinalDate   = "2027-09-26"
)

// Hypothesis is one verdict.
type Hypothesis struct {
	ID       string  `json:"id"`
	Criteria string  `json:"criteria"`
	Trend    float64 `json:"trend"`
	Bench    float64 `json:"benchmark"` // hold value, or the 95% threshold for H3
	Pass     bool    `json:"pass"`
	Note     string  `json:"note,omitempty"`
}

// Scenario is one cost scenario's verdicts for a book.
type Scenario struct {
	Name       string       `json:"name"` // primary | secondary
	LegBps     float64      `json:"leg_bps"`
	From       string       `json:"from"`
	To         string       `json:"to"`
	Bars       int          `json:"bars"`
	RoundTrips int          `json:"round_trips"`
	Hyp        []Hypothesis `json:"hypotheses"`
}

// BookVerdict is one forward test.
type BookVerdict struct {
	Book      string     `json:"book"`
	Prereg    string     `json:"prereg"`
	Scenarios []Scenario `json:"scenarios"`
	Reading   string     `json:"reading"` // the pre-registration's reading of the primary outcome
}

// Report is the JSON written by cmd/prereg-report.
type Report struct {
	Schema      string        `json:"schema"`
	GeneratedAt string        `json:"generated_at"`
	Phase       string        `json:"phase"` // as-of | interim | final
	AsOf        string        `json:"as_of"` // last bar used
	Decides     bool          `json:"decides"`
	Books       []BookVerdict `json:"books"`
	Note        string        `json:"note"`
}

// Book returns the verdict for book, if present.
func (r Report) Book(book string) (BookVerdict, bool) {
	for _, b := range r.Books {
		if b.Book == book {
			return b, true
		}
	}
	return BookVerdict{}, false
}

// Read loads and checks one report file.
func Read(path string) (Report, error) {
	var r Report
	data, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("%s: %w", path, err)
	}
	if r.Schema != Schema {
		return r, fmt.Errorf("%s: schema %q, want %s", path, r.Schema, Schema)
	}
	return r, nil
}

// Patterns are where reports live under a root: the weekly job's output
// (<root>/<date>-<phase>/report.json) and committed evidence
// (<root>/evidence-<date>/prereg-<phase>/report.json).
var Patterns = []string{"*/report.json", "evidence-*/prereg-*/report.json"}

// Latest returns the most recent valid report under roots: the latest data
// (as_of), then the latest phase (final > interim > as-of), then the latest
// generated_at. Missing roots are skipped; unreadable files are skipped and
// counted. found is false when there is none.
func Latest(roots []string) (r Report, path string, found bool, skipped int, err error) {
	type cand struct {
		r    Report
		path string
	}
	var cs []cand
	for _, root := range roots {
		if root == "" {
			continue
		}
		if _, e := os.Stat(root); errors.Is(e, fs.ErrNotExist) {
			continue
		}
		for _, pat := range Patterns {
			matches, e := filepath.Glob(filepath.Join(root, pat))
			if e != nil {
				return r, "", false, skipped, e
			}
			for _, m := range matches {
				rep, e := Read(m)
				if e != nil {
					skipped++
					continue
				}
				cs = append(cs, cand{rep, m})
			}
		}
	}
	if len(cs) == 0 {
		return r, "", false, skipped, nil
	}
	rank := map[string]int{"as-of": 0, "interim": 1, "final": 2}
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i].r, cs[j].r
		if a.AsOf != b.AsOf {
			return a.AsOf > b.AsOf
		}
		if rank[a.Phase] != rank[b.Phase] {
			return rank[a.Phase] > rank[b.Phase]
		}
		return a.GeneratedAt > b.GeneratedAt
	})
	return cs[0].r, cs[0].path, true, skipped, nil
}
