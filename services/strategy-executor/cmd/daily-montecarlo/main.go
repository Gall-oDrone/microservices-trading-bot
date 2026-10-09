// Command daily-montecarlo runs the Monte Carlo of the frozen SMA50 rule
// against buy-and-hold on a Bitso daily CSV and writes the result as JSON,
// for evidence files next to the studies (plan §6.4.11; the same engine as
// ui-api's /forward-tests/{book}/montecarlo: shared/pkg/montecarlo).
//
//	go run ./cmd/daily-montecarlo -csv ../../docs/backtest-readiness/evidence-2026-09-27/btc_mxn_daily_bitso.csv \
//	    -from 2018-01-01 -leg-bps 70 -paths 10000 -out mc.json
//
// Reporting only: it never chooses parameters for the pre-registered rule.
// Exit status: 0 on success, 1 on errors, 2 on bad usage.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/montecarlo"
)

// Report is the JSON written.
type Report struct {
	Schema      string              `json:"schema"`
	CSV         string              `json:"csv"`
	From        string              `json:"from"`
	LegBps      float64             `json:"leg_bps"`
	Summary     montecarlo.Summary  `json:"summary"`
	Calibration montecarlo.Calendar `json:"calibration"`
	Trips       montecarlo.Trips    `json:"trips"`
}

func main() {
	csv := flag.String("csv", "", "Bitso daily CSV (bitsodaily format)")
	from := flag.String("from", "2018-01-01", "first day of the resampled history and of the calibration years")
	legBps := flag.Float64("leg-bps", 70, "cost per leg in bps (pre-registered primary: 70 btc_mxn, 40 btc_usd)")
	paths := flag.Int("paths", montecarlo.DefaultPaths, "paths")
	horizon := flag.Int("horizon", montecarlo.DefaultHorizon, "days per path")
	block := flag.Int("block", montecarlo.DefaultMeanBlock, "mean block length in days")
	seed := flag.Int64("seed", montecarlo.DefaultSeed, "random seed")
	out := flag.String("out", "", "write the JSON here (default stdout)")
	flag.Parse()
	if *csv == "" || flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	start, err := time.Parse("2006-01-02", *from)
	if err != nil {
		fmt.Fprintln(os.Stderr, "daily-montecarlo: -from:", err)
		os.Exit(2)
	}
	rep, err := run(*csv, start, *legBps, montecarlo.Config{Paths: *paths, Horizon: *horizon, MeanBlock: *block, Seed: *seed, SMA: 50})
	if err != nil {
		fmt.Fprintln(os.Stderr, "daily-montecarlo:", err)
		os.Exit(1)
	}
	var w io.Writer = os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintln(os.Stderr, "daily-montecarlo:", err)
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		fmt.Fprintln(os.Stderr, "daily-montecarlo:", err)
		os.Exit(1)
	}
	s := rep.Summary
	fmt.Fprintf(os.Stderr, "%d paths x %d days at %g bps/leg: trend median %+.1f%% (5%% %+.1f%%, 95%% %+.1f%%), hold median %+.1f%%; P(beats hold) %.0f%%, P(shallower DD) %.0f%%; history %d/%d and %d/%d years\n",
		s.Config.Paths, s.Config.Horizon, rep.LegBps, 100*s.TrendReturn.P50, 100*s.TrendReturn.P5, 100*s.TrendReturn.P95, 100*s.HoldReturn.P50,
		100*s.PBeatsHold.P, 100*s.PShallowerDD.P, rep.Calibration.BeatsHold, len(rep.Calibration.Years), rep.Calibration.ShallowerDD, len(rep.Calibration.Years))
}

func run(path string, from time.Time, legBps float64, cfg montecarlo.Config) (Report, error) {
	rows, err := bitsodaily.ReadCSV(path)
	if err != nil {
		return Report{}, err
	}
	bars := make([]dailyrule.Bar, 0, len(rows))
	for _, r := range rows {
		d, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			return Report{}, fmt.Errorf("date %q: %w", r.Date, err)
		}
		bars = append(bars, dailyrule.Bar{Date: d, Open: r.Open, High: r.High, Low: r.Low, Close: r.Close})
	}
	cfg.Costs = dailyrule.Costs{Buy: legBps / 1e4, Sell: legBps / 1e4}
	start := 0
	for start < len(bars) && bars[start].Date.Before(from) {
		start++
	}
	sum, err := montecarlo.Run(bars, start, cfg)
	if err != nil {
		return Report{}, err
	}
	return Report{Schema: "montecarlo-run/v1", CSV: path, From: from.Format("2006-01-02"), LegBps: legBps, Summary: sum,
		Calibration: montecarlo.CalendarYears(bars, cfg.SMA, from.Year(), cfg.Costs),
		Trips:       montecarlo.TripStats(bars, cfg.SMA, from, cfg.Costs)}, nil
}
