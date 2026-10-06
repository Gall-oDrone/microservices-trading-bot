package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// ReportSchema names the JSON written by -json. ui-api reads these files from
// the docs/backtest-readiness/evidence-*/ directories. Changes are additive
// only: never rename or remove a field; a breaking change gets a new version.
const ReportSchema = "research-run/v1"

// Report is the machine-readable twin of the text output: the same numbers,
// unrounded. It never replaces the text evidence files.
type Report struct {
	Schema      string            `json:"schema"`
	Tool        string            `json:"tool"`
	GeneratedAt string            `json:"generated_at"`
	Commit      string            `json:"commit,omitempty"` // vcs.revision when built with `go build`
	Flags       map[string]string `json:"flags"`            // flags set on the command line; paths reduced to base names
	Data        ReportData        `json:"data"`
	Costs       ReportCosts       `json:"costs"`
	Params      ReportParams      `json:"params"`
	Windows     []WindowReport    `json:"windows"`
}

// ReportData describes the loaded history (the DATA line).
type ReportData struct {
	Prices   string `json:"prices"`         // base name of -prices
	Book     string `json:"book,omitempty"` // only meaningful with a .parquet -prices file
	Bars     int    `json:"bars"`
	First    string `json:"first"`
	Last     string `json:"last"`
	News     string `json:"news,omitempty"` // base name of -news
	NewsDays int    `json:"news_days"`
}

// ReportCosts is the cost model (the COSTS line), in basis points.
type ReportCosts struct {
	BuyBPS       float64 `json:"buy_bps"`
	SellBPS      float64 `json:"sell_bps"`
	SlippageBPS  float64 `json:"slippage_bps"`
	RoundTripBPS float64 `json:"round_trip_bps"`
}

// ReportParams are the fixed rule parameters (the RULES line) and the random baseline.
type ReportParams struct {
	SMA           int     `json:"sma"`
	NewsWindow    int     `json:"news_window"`
	NewsThreshold float64 `json:"news_threshold"`
	Sims          int     `json:"sims"`
	Seed          int64   `json:"seed"`
}

// WindowReport is one -windows entry.
type WindowReport struct {
	// Label is the CLI's convention: the first window is IN-SAMPLE, the rest
	// OUT-OF-SAMPLE. A window that spans every year (a "full span" run) is
	// labelled OUT-OF-SAMPLE but is not; read the study for the design.
	Label   string       `json:"label"`
	From    string       `json:"from"`
	To      string       `json:"to"`
	Bars    int          `json:"bars"`
	Gaps    string       `json:"gaps,omitempty"` // missing days inside the window
	Note    string       `json:"note,omitempty"` // e.g. "not enough bars in window"
	Results []RuleResult `json:"results"`
}

// RuleResult is one row of a window's table.
type RuleResult struct {
	Rule        string          `json:"rule"`
	ReturnPct   float64         `json:"return_pct"`
	RoundTrips  int             `json:"round_trips"`
	ExposurePct float64         `json:"exposure_pct"`
	MaxDDPct    float64         `json:"max_dd_pct"`
	CostPct     float64         `json:"cost_pct"`
	VsHoldPP    float64         `json:"vs_hold_pp"`
	Random      *RandomBaseline `json:"random"` // null for buy_and_hold and for rules with no round trip
}

// RandomBaseline compares a rule with random strategies making the same number of round trips.
type RandomBaseline struct {
	Sims    int     `json:"sims"`
	BeatPct float64 `json:"beat_pct"`
}

// setFlags returns the flags given on the command line. Path flags keep only
// their base name, so committed reports don't carry local directory layouts.
func setFlags(paths ...string) map[string]string {
	isPath := map[string]bool{}
	for _, p := range paths {
		isPath[p] = true
	}
	out := map[string]string{}
	flag.Visit(func(f *flag.Flag) {
		v := f.Value.String()
		if isPath[f.Name] && v != "" {
			v = filepath.Base(v)
		}
		out[f.Name] = v
	})
	return out
}

// buildRevision overrides the VCS stamp when set at link time:
//
//	go build -ldflags "-X main.buildRevision=$(git rev-parse HEAD)" ./cmd/daily-research
//
// Useful for builds Go doesn't stamp, such as from a clean git worktree.
var buildRevision string

func buildCommit() string {
	if buildRevision != "" {
		return buildRevision
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "-dirty"
			}
		}
	}
	if rev == "" {
		return ""
	}
	return rev + dirty
}

// writeReport writes r as indented JSON, via a temp file so a reader never sees a partial file.
func writeReport(path string, r Report) error {
	r.Schema = ReportSchema
	r.Tool = "daily-research"
	if r.GeneratedAt == "" {
		r.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
