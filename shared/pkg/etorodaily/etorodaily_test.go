package etorodaily

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/etoro"
)

func day(s string) time.Time {
	d, _ := time.Parse("2006-01-02", s)
	return d
}

func candle(date string, o, h, l, c float64) etoro.Candle {
	return etoro.Candle{InstrumentID: 28, FromDate: day(date), Open: o, High: h, Low: l, Close: c}
}

func TestToRowsDropsWeekendHolidayInProgress(t *testing.T) {
	cs := []etoro.Candle{
		candle("2026-07-02", 10, 11, 9, 10.5),
		candle("2026-07-03", 10.5, 11, 10, 10.8), // Independence Day (observed): CFD trades, NYSE closed
		candle("2026-07-04", 10.8, 10.9, 10.7, 10.8),
		candle("2026-07-05", 10.8, 11, 10.7, 10.9), // Sunday evening futures open
		candle("2026-07-06", 10.9, 11.5, 10.8, 11.2),
		candle("2026-07-07", 11.2, 11.0, 11.1, 11.3), // high < close: invalid
		candle("2026-07-08", 11.3, 11.6, 11.2, 11.5),
		candle("2026-07-09", 11.5, 11.7, 11.4, 11.6), // in progress at now
	}
	now := time.Date(2026, 7, 9, 15, 0, 0, 0, time.UTC)
	rows, rep := ToRows(cs, now, Options{})
	got := []string{}
	for _, r := range rows {
		got = append(got, r.Date)
	}
	want := []string{"2026-07-02", "2026-07-06", "2026-07-08"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("kept %v, want %v", got, want)
	}
	if rep.InProgress != 1 || rep.Weekend != 2 || rep.Holiday != 1 || rep.Invalid != 1 || rep.Kept != 3 {
		t.Fatalf("report %+v", rep)
	}
	if rep.MissingTrading != 1 { // 2026-07-07 was a trading day but its bar was invalid
		t.Fatalf("missing trading days %d, want 1", rep.MissingTrading)
	}

	all, rep2 := ToRows(cs, now, Options{KeepNonTradingDays: true})
	if len(all) != 6 || rep2.Weekend != 0 || rep2.Holiday != 0 {
		t.Fatalf("KeepNonTradingDays kept %d (%+v)", len(all), rep2)
	}
}

func TestFreshness(t *testing.T) {
	// Monday 2026-10-12 13:35 UTC: last closed trading day is Friday 10-09.
	now := time.Date(2026, 10, 12, 13, 35, 0, 0, time.UTC)
	if got := LatestClosedTradingDay(now).Format("2006-01-02"); got != "2026-10-09" {
		t.Fatalf("latest closed = %s", got)
	}
	// Tuesday after the Monday close: Monday's bucket closed at Tue 00:00Z.
	if got := LatestClosedTradingDay(time.Date(2026, 10, 13, 0, 0, 1, 0, time.UTC)).Format("2006-01-02"); got != "2026-10-12" {
		t.Fatalf("latest closed = %s", got)
	}
	// Day after Thanksgiving 2026-11-27: previous trading day is 11-25.
	if got := LatestClosedTradingDay(time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)).Format("2006-01-02"); got != "2026-11-25" {
		t.Fatalf("latest closed = %s", got)
	}
	rows := []bitsodaily.Row{{Date: "2026-10-08"}, {Date: "2026-10-09"}}
	if err := CheckFresh(rows, now); err != nil {
		t.Fatal(err)
	}
	if err := CheckFresh(rows[:1], now); err == nil {
		t.Fatal("stale series accepted")
	}
}

// The real demo fixture (10 newest NSDQ100 bars on 2026-10-10 01:47Z)
// keeps exactly the closed weekday trading days.
func TestRealFixture(t *testing.T) {
	raw, err := os.ReadFile("../etoro/testdata/candles_28_oneday.json")
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Candles []struct {
			Candles []etoro.Candle `json:"candles"`
		} `json:"candles"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 10, 1, 47, 0, 0, time.UTC)
	rows, rep := ToRows(resp.Candles[0].Candles, now, Options{})
	if rep.InProgress != 1 || rep.Weekend != 2 || rep.Kept != 7 {
		t.Fatalf("report %+v (want Sat 10-10 in progress, 10-03/10-04 weekend, 7 weekdays)", rep)
	}
	if rows[len(rows)-1].Date != "2026-10-09" || rows[len(rows)-1].Close != 30893.3 {
		t.Fatalf("last row %+v", rows[len(rows)-1])
	}
	if err := CheckFresh(rows, now); err != nil {
		t.Fatal(err)
	}

	// Round-trips through the research CSV format.
	path := filepath.Join(t.TempDir(), "nsdq100.csv")
	if err := bitsodaily.WriteCSV(path, "etoro_nsdq100", rows); err != nil {
		t.Fatal(err)
	}
	back, err := bitsodaily.ReadCSV(path)
	if err != nil || len(back) != len(rows) || back[0] != rows[0] {
		t.Fatalf("csv round trip: %v %+v vs %+v", err, back[0], rows[0])
	}
	if bars := Bars(back); len(bars) != 7 || bars[6].Close != 30893.3 {
		t.Fatalf("bars %+v", bars)
	}
}
