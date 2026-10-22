package etoroledger

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealModeAndRefs(t *testing.T) {
	if !IsAccountMode(ModeDemo) || !IsAccountMode(ModeReal) || IsAccountMode(ModeDryRun) || IsAccountMode("") {
		t.Fatal("IsAccountMode")
	}
	r := Record{Mode: ModeReal, Demo: &Demo{Env: "real"}}
	if !r.IsAccount() || (Record{Mode: ModeReal}).IsAccount() || (Record{Mode: ModeDryRun, Demo: &Demo{}}).IsAccount() {
		t.Fatal("IsAccount")
	}
	ref, err := UIRealClientRef("nsdq100", "2026-11-03", ActionOpen)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref, "ui-real-nsdq100-2026-11-03-open-") || len(ref) != len("ui-real-nsdq100-2026-11-03-open-")+6 || !IsRealRef(ref) {
		t.Fatalf("real ref %q", ref)
	}
	demo, err := UIClientRef("nsdq100", "2026-11-03", ActionOpen)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(demo, "ui-nsdq100-") || IsRealRef(demo) || IsRealRef(ClientRef("nsdq100", "2026-11-03", ActionOpen)) {
		t.Fatalf("demo ref %q", demo)
	}
}

func TestNightsHeld(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	open := time.Date(2026, 11, 3, 10, 0, 0, 0, ny) // Tuesday 10:00
	cases := []struct {
		at   time.Time
		want int
	}{
		{time.Date(2026, 11, 3, 16, 59, 0, 0, ny), 0},
		{time.Date(2026, 11, 3, 18, 0, 0, 0, ny), 1},
		{time.Date(2026, 11, 4, 18, 0, 0, 0, ny), 2},
		{time.Date(2026, 11, 6, 18, 0, 0, 0, ny), 4}, // Friday evening
		{time.Date(2026, 11, 8, 18, 0, 0, 0, ny), 6}, // Sunday evening: the weekend nights count as calendar nights
		{time.Date(2026, 11, 9, 16, 0, 0, 0, ny), 6}, // Monday before the boundary
		{time.Date(2026, 11, 9, 17, 0, 0, 0, ny), 7}, // at the boundary
		{open, 0},
		{open.Add(-time.Hour), 0},
	}
	for _, c := range cases {
		if got := NightsHeld(open, c.at, ny); got != c.want {
			t.Errorf("%s: nights %d want %d", c.at.Format(time.RFC3339), got, c.want)
		}
	}
	if NightsHeld(time.Time{}, open, ny) != 0 || NightsHeld(open, open.Add(48*time.Hour), nil) != 0 {
		t.Fatal("zero or nil inputs")
	}
}

func TestObservationsRoundTripAndDeltas(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, "real-canary", "ledger.jsonl")
	path := ObservationsPath(ledger)
	if filepath.Base(path) != ObservationsFile || filepath.Dir(path) != filepath.Dir(ledger) {
		t.Fatalf("path %s", path)
	}
	obs, err := ReadObservations(path)
	if err != nil || obs != nil {
		t.Fatalf("missing file: %v %v", obs, err)
	}
	d1 := -0.23
	first := Observation{At: "2026-11-03T23:00:00Z", NYDate: "2026-11-03", Env: "real", Ledger: "etoro-real-canary",
		Account:   Account{Currency: "USD", Cash: 10, Equity: 60.5},
		Positions: []ObservedPosition{{PositionID: "1", InstrumentID: 28, Units: 0.04, Leverage: 20, FeesToDate: 0, Nights: 1, InLedger: true, Preview: &Costs{OvernightFee: 0.22, WeekendFee: 0.66}}},
	}
	second := first
	second.At, second.NYDate, second.PreviousAt = "2026-11-04T23:00:00Z", "2026-11-04", first.At
	second.Positions = []ObservedPosition{{PositionID: "1", InstrumentID: 28, Units: 0.04, Leverage: 20, FeesToDate: -0.23, FeesDelta: &d1, Nights: 2, Exposure: 1001, InLedger: true, Preview: &Costs{OvernightFee: 0.22, WeekendFee: 0.66}}}
	for _, o := range []Observation{first, second} {
		if err := AppendObservation(path, o); err != nil {
			t.Fatal(err)
		}
	}
	back, err := ReadObservations(path)
	if err != nil || len(back) != 2 {
		t.Fatalf("read back: %d %v", len(back), err)
	}
	if back[0].Schema != ObservationSchema || back[1].Positions[0].FeesDelta == nil || *back[1].Positions[0].FeesDelta != d1 {
		t.Fatalf("back %+v", back)
	}
	if l := LatestObservation(back); l == nil || l.At != second.At {
		t.Fatalf("latest %+v", l)
	}
	ds := FeeDeltas(back)
	if len(ds) != 1 || ds[0].Delta != d1 || ds[0].Nights != 2 || ds[0].PreviewOvernight != 0.22 || ds[0].PreviewWeekend != 0.66 || ds[0].Exposure != 1001 || ds[0].NYDate != "2026-11-04" {
		t.Fatalf("deltas %+v", ds)
	}
	if LatestObservation(nil) != nil || len(FeeDeltas(nil)) != 0 {
		t.Fatal("empty series")
	}
}
