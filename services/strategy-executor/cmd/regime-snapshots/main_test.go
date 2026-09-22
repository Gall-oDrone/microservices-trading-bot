package main

import (
	"encoding/json"
	"sort"
	"testing"
)

// TestSnapshotLineFieldNames locks the JSON key set emitted by this tool.
//
// strategy-router's classifier.Snapshot has no json tags, so encoding/json
// matches on exact Go field names. strategy-router is a separate module and
// cannot be imported here, so nothing at compile time ties the two together.
// If a field is renamed on either side, json.Unmarshal silently leaves the
// value at zero and every snapshot degrades to "neutral" — a plausible-looking
// but completely wrong regime distribution. This test is the tripwire.
//
// Source of truth:
// services/strategy-router/internal/classifier/classifier.go, type Snapshot.
func TestSnapshotLineFieldNames(t *testing.T) {
	want := []string{
		"ATR",
		"BBLower",
		"BBMiddle",
		"BBUpper",
		"Book",
		"DataHealthy",
		"EMA",
		"Price",
		"RSI",
		"SnapshotAt",
		"StaleReason",
	}

	b, err := json.Marshal(snapshotLine{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)

	if len(got) != len(want) {
		t.Fatalf("key set = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("key set = %v, want %v", got, want)
		}
	}
}

// TestBarWindowMatchesLiveService pins the trailing-window arithmetic to the
// production values in indicators/bars_helpers.go. Live defaults are SMA/EMA/BB
// 20, RSI 14, ATR 14, buffer 5 — minBarsRequired is 20 and barFetchLimit is
// floored at 30.
func TestBarWindowMatchesLiveService(t *testing.T) {
	live := config{smaPeriod: 20, emaPeriod: 20, rsiPeriod: 14, bbPeriod: 20, atrPeriod: 14, barBuffer: 5}

	if got := minBarsRequired(live); got != 20 {
		t.Errorf("minBarsRequired = %d, want 20", got)
	}
	if got := barFetchLimit(live); got != 30 {
		t.Errorf("barFetchLimit = %d, want 30 (25 raised by the floor)", got)
	}

	// RSI/ATR needing period+1 must dominate when they are the widest.
	wide := config{smaPeriod: 5, emaPeriod: 5, rsiPeriod: 50, bbPeriod: 5, atrPeriod: 14, barBuffer: 5}
	if got := minBarsRequired(wide); got != 51 {
		t.Errorf("minBarsRequired = %d, want 51 (RSI period+1)", got)
	}
	if got := barFetchLimit(wide); got != 56 {
		t.Errorf("barFetchLimit = %d, want 56", got)
	}

	// The 500-bar ceiling must hold.
	huge := config{smaPeriod: 900, barBuffer: 5}
	if got := barFetchLimit(huge); got != 500 {
		t.Errorf("barFetchLimit = %d, want the 500 ceiling", got)
	}
}

func TestParseTime(t *testing.T) {
	if _, err := parseTime("2026-08-19"); err != nil {
		t.Errorf("date form should parse: %v", err)
	}
	if _, err := parseTime("2026-08-19T12:30:00Z"); err != nil {
		t.Errorf("RFC3339 form should parse: %v", err)
	}
	if _, err := parseTime("19/08/2026"); err == nil {
		t.Error("expected an error for an unsupported format")
	}
	if _, err := parseTime(""); err == nil {
		t.Error("expected an error for an empty value")
	}
}
