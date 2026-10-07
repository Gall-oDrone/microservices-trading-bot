package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// ReportSchema is the same schema cmd/daily-research writes, so ui-api and
// the web app read both tools' reports with one contract. The types below
// mirror cmd/daily-research/report.go field for field; everything this tool
// adds is marked "additive" and optional for readers. Never rename or remove
// a v1 field here: a breaking change gets a new schema version in both tools.
const ReportSchema = "research-run/v1"

// Report is the machine-readable twin of one cost level's output. With
// -stress-leg-bps the tool writes a second report for the stress costs.
type Report struct {
	Schema      string            `json:"schema"`
	Tool        string            `json:"tool"`
	GeneratedAt string            `json:"generated_at"`
	Commit      string            `json:"commit,omitempty"`
	Flags       map[string]string `json:"flags"`
	Data        ReportData        `json:"data"`
	Costs       ReportCosts       `json:"costs"`
	Params      ReportParams      `json:"params"`
	Windows     []WindowReport    `json:"windows"`
}

// ReportData describes the loaded candles.
type ReportData struct {
	Prices   string `json:"prices"` // base name of -csv
	Book     string `json:"book,omitempty"`
	Bars     int    `json:"bars"`
	First    string `json:"first"`
	Last     string `json:"last"`
	NewsDays int    `json:"news_days"` // always 0: this tool uses no news
}

// ReportCosts is the cost model in basis points. This tool charges one cost
// per leg that already includes slippage, so buy_bps = sell_bps = the leg
// cost and slippage_bps is 0; Note says so for readers.
type ReportCosts struct {
	BuyBPS       float64 `json:"buy_bps"`
	SellBPS      float64 `json:"sell_bps"`
	SlippageBPS  float64 `json:"slippage_bps"`
	RoundTripBPS float64 `json:"round_trip_bps"`
	Level        string  `json:"level,omitempty"` // additive: "base" or "stress"
	Note         string  `json:"note,omitempty"`  // additive
}

// ReportParams keeps the v1 fields (news and random baseline are unused here,
// so 0) and adds this tool's fixed parameters.
type ReportParams struct {
	SMA           int     `json:"sma"`
	NewsWindow    int     `json:"news_window"`
	NewsThreshold float64 `json:"news_threshold"`
	Sims          int     `json:"sims"`
	Seed          int64   `json:"seed"`

	HoldoutStart    string  `json:"holdout_start,omitempty"`     // additive
	End             string  `json:"end,omitempty"`               // additive
	VolumeRatioDays int     `json:"volume_ratio_days,omitempty"` // additive
	VolTarget       float64 `json:"vol_target,omitempty"`        // additive: annualized, for the vol-target variants
}

// WindowReport is the DEVELOPMENT or the HOLDOUT window.
type WindowReport struct {
	Label   string       `json:"label"`
	From    string       `json:"from"`
	To      string       `json:"to"`
	Bars    int          `json:"bars"`
	Note    string       `json:"note,omitempty"`
	Results []RuleResult `json:"results"`

	// Additive, base-cost report only (the text prints them once, at base costs).
	Events      []EventRow       `json:"events,omitempty"`
	Sensitivity *SensitivityRows `json:"sensitivity,omitempty"`
}

// RuleResult is one strategy row. RoundTrips counts entries from flat (a
// partial-weight strategy can trade many times inside one round trip; Trades
// counts every rebalance).
type RuleResult struct {
	Rule        string          `json:"rule"`
	ReturnPct   float64         `json:"return_pct"`
	RoundTrips  int             `json:"round_trips"`
	ExposurePct float64         `json:"exposure_pct"`
	MaxDDPct    float64         `json:"max_dd_pct"`
	CostPct     float64         `json:"cost_pct"`
	VsHoldPP    float64         `json:"vs_hold_pp"`
	Random      *RandomBaseline `json:"random"` // always null: no random baseline in this tool

	Label             string  `json:"label,omitempty"`      // additive: the name the text output prints
	Trades            int     `json:"trades"`               // additive
	TurnoverX         float64 `json:"turnover_x"`           // additive: traded notional / equity, summed
	ReturnZeroCostPct float64 `json:"return_zero_cost_pct"` // additive: same trades, no costs
	CAGRPct           float64 `json:"cagr_pct"`             // additive
	Sharpe            float64 `json:"sharpe"`               // additive: daily, annualized with 365
	WeeksUp           int     `json:"weeks_up"`             // additive
	WeeksDown         int     `json:"weeks_down"`           // additive
	WeeksFlat         int     `json:"weeks_flat"`           // additive
	MedianWeekPct     float64 `json:"median_week_pct"`      // additive
	WorstWeekPct      float64 `json:"worst_week_pct"`       // additive
}

// RandomBaseline mirrors daily-research's type; this tool never fills it.
type RandomBaseline struct {
	Sims    int     `json:"sims"`
	BeatPct float64 `json:"beat_pct"`
}

// EventRow is one line of the volume-spike event study.
type EventRow struct {
	Condition         string  `json:"condition"`
	H                 int     `json:"h"`
	N                 int     `json:"n"`
	MeanPct           float64 `json:"mean_pct"`
	MedianPct         float64 `json:"median_pct"`
	HitPct            float64 `json:"hit_pct"`
	T                 float64 `json:"t"`
	MeanAfterCostsPct float64 `json:"mean_after_costs_pct"` // mean minus one round trip at base costs
}

// SensitivityRows is the post-hoc volume-threshold check on the SMA50 entry.
type SensitivityRows struct {
	PostHoc bool             `json:"post_hoc"` // always true: added after the main tables were seen
	Chosen  float64          `json:"chosen"`   // the pre-declared k
	Rows    []SensitivityRow `json:"rows"`
}

// SensitivityRow is one k. K = 0 means no volume condition (plain SMA50).
type SensitivityRow struct {
	K         float64 `json:"k"`
	ReturnPct float64 `json:"return_pct"`
	MaxDDPct  float64 `json:"max_dd_pct"`
	Sharpe    float64 `json:"sharpe"`
	Trades    int     `json:"trades"`
}

// ruleResult converts a strategy row; hold is buy-and-hold's return in the same window and costs.
func ruleResult(s strategy, r, r0 result, hold float64) RuleResult {
	return RuleResult{
		Rule:              s.id,
		Label:             s.name,
		ReturnPct:         100 * r.ret,
		RoundTrips:        r.entries,
		ExposurePct:       100 * r.exposure,
		MaxDDPct:          100 * r.maxDD,
		CostPct:           100 * r.cost,
		VsHoldPP:          100 * (r.ret - hold),
		Trades:            r.trades,
		TurnoverX:         r.turnover,
		ReturnZeroCostPct: 100 * r0.ret,
		CAGRPct:           100 * r.cagr,
		Sharpe:            r.sharpe,
		WeeksUp:           r.weeksUp,
		WeeksDown:         r.weeksDown,
		WeeksFlat:         r.weeksFlat,
		MedianWeekPct:     100 * r.medianWeek,
		WorstWeekPct:      100 * r.worstWeek,
	}
}

// setFlags returns the flags given on the command line; path flags keep only
// their base name so committed reports don't carry local directory layouts.
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

// stressPath is where the stress-cost report goes: "x.json" -> "x-stress-88bps.json".
func stressPath(path string, bps float64) string {
	return fmt.Sprintf("%s-stress-%sbps.json", strings.TrimSuffix(path, ".json"), strconv.FormatFloat(bps, 'f', -1, 64))
}

// buildRevision overrides the VCS stamp when set at link time (see cmd/daily-research).
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

// writeReport writes r as indented JSON via a temp file, so a reader never sees a partial file.
func writeReport(path string, r Report) error {
	r.Schema = ReportSchema
	r.Tool = "weekly-research"
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
