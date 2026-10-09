package main

import (
	"path/filepath"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/montecarlo"
)

func TestRunOnCommittedHistory(t *testing.T) {
	csv := filepath.Join("..", "..", "..", "..", "docs", "backtest-readiness", "evidence-2026-09-27", "btc_mxn_daily_bitso.csv")
	rep, err := run(csv, time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC), 70, montecarlo.Config{Paths: 200, Horizon: 365, MeanBlock: 20, Seed: 1, SMA: 50})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Schema != "montecarlo-run/v1" || rep.Summary.Config.Paths != 200 || rep.Summary.Config.Costs.Buy != 0.007 {
		t.Fatalf("%+v", rep.Summary.Config)
	}
	if rep.Calibration.BeatsHold != 5 || rep.Calibration.ShallowerDD != 6 || rep.Trips.Count < 100 {
		t.Fatalf("calibration %d/%d trips %d", rep.Calibration.BeatsHold, rep.Calibration.ShallowerDD, rep.Trips.Count)
	}
	if _, err := run("missing.csv", time.Now(), 70, montecarlo.Config{Paths: 10, Horizon: 30, MeanBlock: 5, SMA: 50}); err == nil {
		t.Fatal("missing file accepted")
	}
}
