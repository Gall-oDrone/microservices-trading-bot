package mktcal

import (
	"sort"
	"testing"
	"time"
)

func ds(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// holidaysIn lists weekday closures in a year, sorted.
func holidaysIn(y int) []string {
	var out []string
	for d := Date(y, 1, 1); d.Year() == y; d = d.AddDate(0, 0, 1) {
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday && !IsTradingDay(d) {
			out = append(out, d.Format("2006-01-02"))
		}
	}
	sort.Strings(out)
	return out
}

// Published NYSE holiday schedules (nyse.com/markets/hours-calendars).
func TestPublishedHolidays(t *testing.T) {
	want := map[int][]string{
		2022: {"2022-01-17", "2022-02-21", "2022-04-15", "2022-05-30", "2022-06-20", "2022-07-04", "2022-09-05", "2022-11-24", "2022-12-26"},
		2023: {"2023-01-02", "2023-01-16", "2023-02-20", "2023-04-07", "2023-05-29", "2023-06-19", "2023-07-04", "2023-09-04", "2023-11-23", "2023-12-25"},
		2024: {"2024-01-01", "2024-01-15", "2024-02-19", "2024-03-29", "2024-05-27", "2024-06-19", "2024-07-04", "2024-09-02", "2024-11-28", "2024-12-25"},
		2025: {"2025-01-01", "2025-01-09", "2025-01-20", "2025-02-17", "2025-04-18", "2025-05-26", "2025-06-19", "2025-07-04", "2025-09-01", "2025-11-27", "2025-12-25"},
		2026: {"2026-01-01", "2026-01-19", "2026-02-16", "2026-04-03", "2026-05-25", "2026-06-19", "2026-07-03", "2026-09-07", "2026-11-26", "2026-12-25"},
		2027: {"2027-01-01", "2027-01-18", "2027-02-15", "2027-03-26", "2027-05-31", "2027-06-18", "2027-07-05", "2027-09-06", "2027-11-25", "2027-12-24"},
	}
	for y, w := range want {
		got := holidaysIn(y)
		if len(got) != len(w) {
			t.Errorf("%d: got %v\nwant %v", y, got, w)
			continue
		}
		for i := range w {
			if got[i] != w[i] {
				t.Errorf("%d: got %v\nwant %v", y, got, w)
				break
			}
		}
	}
}

func TestEarlyCloses(t *testing.T) {
	early := []string{"2024-07-03", "2024-11-29", "2024-12-24", "2025-07-03", "2025-11-28", "2025-12-24", "2026-11-27", "2026-12-24", "2027-11-26"}
	for _, s := range early {
		if !IsEarlyClose(ds(t, s)) {
			t.Errorf("%s should close at 13:00", s)
		}
	}
	notEarly := []string{"2026-07-02", "2027-07-02", "2027-12-23", "2026-10-12", "2027-12-24"}
	for _, s := range notEarly {
		if IsEarlyClose(ds(t, s)) {
			t.Errorf("%s is not an early close", s)
		}
	}
	_, close, ok := Session(ds(t, "2026-11-27"))
	if !ok || close.UTC().Format("15:04") != "18:00" {
		t.Fatalf("2026-11-27 close %v, want 13:00 EST = 18:00 UTC", close.UTC())
	}
}

func TestSessionDST(t *testing.T) {
	cases := []struct{ day, openUTC, closeUTC string }{
		{"2026-03-06", "14:30", "21:00"}, // EST
		{"2026-03-09", "13:30", "20:00"}, // EDT from Mar 8
		{"2026-10-30", "13:30", "20:00"}, // EDT
		{"2026-11-02", "14:30", "21:00"}, // EST from Nov 1
	}
	for _, c := range cases {
		o, cl, ok := Session(ds(t, c.day))
		if !ok || o.UTC().Format("15:04") != c.openUTC || cl.UTC().Format("15:04") != c.closeUTC {
			t.Errorf("%s: %v-%v (ok %v), want %s-%s UTC", c.day, o.UTC(), cl.UTC(), ok, c.openUTC, c.closeUTC)
		}
	}
	if _, _, ok := Session(ds(t, "2026-10-10")); ok {
		t.Fatal("Saturday has no session")
	}
}

func TestIsOpen(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := map[string]bool{
		"2026-10-12T13:29:59Z": false, // 09:29:59 EDT
		"2026-10-12T13:30:00Z": true,
		"2026-10-12T19:59:59Z": true,
		"2026-10-12T20:00:00Z": false,
		"2026-10-10T15:00:00Z": false, // Saturday
		"2026-11-26T15:00:00Z": false, // Thanksgiving
		"2026-11-27T17:59:00Z": true,  // early close day, 12:59 EST
		"2026-11-27T18:00:00Z": false,
	}
	for s, want := range cases {
		if got := IsOpen(at(s)); got != want {
			t.Errorf("IsOpen(%s) = %v, want %v", s, got, want)
		}
	}
}

func TestTradingDayArithmetic(t *testing.T) {
	if got := NextTradingDay(ds(t, "2026-10-09")); got.Format("2006-01-02") != "2026-10-12" {
		t.Fatalf("after Fri 2026-10-09: %v", got)
	}
	if got := NextTradingDay(ds(t, "2026-04-02")); got.Format("2006-01-02") != "2026-04-06" {
		t.Fatalf("Good Friday skipped: %v", got)
	}
	if got := PrevTradingDay(ds(t, "2026-01-02")); got.Format("2006-01-02") != "2025-12-31" {
		t.Fatalf("before 2026-01-02: %v", got)
	}
	if n := TradingDaysBetween(ds(t, "2026-10-09"), ds(t, "2026-10-16")); n != 5 {
		t.Fatalf("(Fri, next Fri] = %d trading days, want 5", n)
	}
	if n := TradingDaysBetween(ds(t, "2026-10-16"), ds(t, "2026-10-09")); n != -5 {
		t.Fatalf("reversed = %d", n)
	}
	// Full years: 252 minus holidays falling on weekdays, 2024 = 252, 2025 = 250.
	for y, want := range map[int]int{2023: 250, 2024: 252, 2025: 250, 2026: 251} {
		if n := len(TradingDays(Date(y, 1, 1), Date(y, 12, 31))); n != want {
			t.Errorf("%d: %d trading days, want %d", y, n, want)
		}
	}
}

func TestEaster(t *testing.T) {
	for y, want := range map[int]string{2000: "2000-04-23", 2019: "2019-04-21", 2024: "2024-03-31", 2026: "2026-04-05", 2027: "2027-03-28", 2038: "2038-04-25"} {
		if got := easter(y).Format("2006-01-02"); got != want {
			t.Errorf("easter(%d) = %s, want %s", y, got, want)
		}
	}
}

func TestHolidayNames(t *testing.T) {
	if n, ok := Holiday(ds(t, "2026-07-03")); !ok || n != "Independence Day (observed)" {
		t.Fatalf("2026-07-03: %q %v", n, ok)
	}
	if n, ok := Holiday(ds(t, "2025-01-09")); !ok || n == "" {
		t.Fatal("Carter day of mourning missing")
	}
	if _, ok := Holiday(ds(t, "2021-12-31")); ok {
		t.Fatal("NYSE does not observe a Saturday New Year's Day on Friday")
	}
}
