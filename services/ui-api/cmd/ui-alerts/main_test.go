package main

import (
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/alerts"
	"bitso-trading-platform/ui-api/internal/api"
	"bitso-trading-platform/ui-api/internal/store"
)

func TestEvaluateInProcess(t *testing.T) {
	ls := []api.Ledger{
		{Name: "stage", Store: store.New("../../internal/api/testdata/ledger.jsonl", "")},
		{Name: "gone", Store: store.New(filepath.Join(t.TempDir(), "x", "ledger.jsonl"), "")},
	}
	// The test ledger ends on 2026-10-01; a week later every book has missed days.
	now := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	srv := &api.Server{Ledgers: ls, Policy: risk.DefaultPolicy(), StageSize: 0.001, Version: "test",
		Log: log.New(io.Discard, "", 0), Now: func() time.Time { return now }}
	got := evaluate(srv.Handler(), ls)
	by := map[string]alerts.Alert{}
	for _, a := range got {
		by[a.Key] = a
	}
	for _, k := range []string{"stage/executor.ledger.btc_mxn", "stage/executor.ledger.btc_usd"} {
		if by[k].Severity != alerts.Critical {
			t.Fatalf("%s missing or not critical in %+v", k, got)
		}
	}
	// A ledger with no file yet: its coverage is "no_data" (a warning), not an
	// evaluation error.
	if a, ok := by["gone/executor.ledger.btc_mxn"]; !ok || a.Severity != alerts.Warning {
		t.Fatalf("missing ledger: %+v", got)
	}
	for k := range by {
		if k == "stage/eval.data health" || k == "stage/eval.risk" {
			t.Fatalf("unexpected evaluation error %s: %+v", k, by[k])
		}
	}
}

func TestStateRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "deep", "state.json")
	if s, err := loadState(p); err != nil || len(s.Open) != 0 {
		t.Fatalf("missing state: %+v %v", s, err)
	}
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	_, s := alerts.Diff(alerts.State{}, []alerts.Alert{{Key: "k", Severity: alerts.Critical, Title: "t"}}, at, time.Hour)
	if err := saveState(p, s); err != nil {
		t.Fatal(err)
	}
	back, err := loadState(p)
	if err != nil || back.Open["k"].Title != "t" || !back.Open["k"].LastSent.Equal(at) || !back.LastRun.Equal(at) {
		t.Fatalf("round trip %+v %v", back, err)
	}
}
