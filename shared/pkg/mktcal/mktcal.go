// Package mktcal is the NYSE trading calendar: which civil dates the cash
// equity market is open, its session times in America/New_York (so DST is
// handled by the tz database), early closes, and trading-day arithmetic.
//
// Why NYSE and not eToro's own CFD hours: the index CFDs (NSDQ100, SPX500)
// quote nearly 24/5 (and, since ~September 2026, at weekends), but their
// price is only anchored to the underlying index during the cash session.
// The eToro executor therefore acts on NYSE trading days inside the cash
// session, and research annualizes over NYSE trading days (252/yr).
//
// Rules (NYSE Rule 7.2 and the published holiday lists):
//   - New Year's Day (Jan 1; Sunday -> Monday; Saturday -> not observed),
//     Martin Luther King Jr. Day (3rd Mon Jan), Washington's Birthday (3rd Mon
//     Feb), Good Friday, Memorial Day (last Mon May), Juneteenth (Jun 19 from
//     2022), Independence Day (Jul 4), Labor Day (1st Mon Sep), Thanksgiving
//     (4th Thu Nov), Christmas (Dec 25); fixed-date holidays on a Saturday are
//     observed the Friday before, on a Sunday the Monday after;
//   - special closures listed in specialClosures;
//   - early close at 13:00 ET on the day after Thanksgiving, on Jul 3 and Dec
//     24 when they are Monday-Thursday trading days.
//
// Rules are valid from 1990; Juneteenth applies from 2022. Dates are civil
// dates: only the year, month and day of a time.Time argument are used.
package mktcal

import (
	"fmt"
	"time"

	// Service images are alpine without /usr/share/zoneinfo.
	_ "time/tzdata"
)

// NewYork is the exchange time zone.
var NewYork = mustLoc("America/New_York")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// TradingDaysPerYear is the annualization constant for daily returns.
const TradingDaysPerYear = 252

// Regular session times, New York local.
const (
	OpenHour, OpenMinute         = 9, 30
	CloseHour, CloseMinute       = 16, 0
	EarlyCloseHour, EarlyCloseMn = 13, 0
)

// specialClosures are unscheduled full-day closures since 2000.
var specialClosures = map[string]string{
	"2001-09-11": "September 11 attacks",
	"2001-09-12": "September 11 attacks",
	"2001-09-13": "September 11 attacks",
	"2001-09-14": "September 11 attacks",
	"2004-06-11": "National day of mourning (Reagan)",
	"2007-01-02": "National day of mourning (Ford)",
	"2012-10-29": "Hurricane Sandy",
	"2012-10-30": "Hurricane Sandy",
	"2018-12-05": "National day of mourning (G.H.W. Bush)",
	"2025-01-09": "National day of mourning (Carter)",
}

// civil normalizes t to midnight UTC of its calendar date.
func civil(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Date builds a civil date.
func Date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// Holiday returns the name of the holiday or closure on date, if any.
// Weekends are not holidays (see IsTradingDay).
func Holiday(date time.Time) (string, bool) {
	d := civil(date)
	if name, ok := specialClosures[d.Format("2006-01-02")]; ok {
		return name, true
	}
	name, ok := yearHolidays(d.Year())[d]
	return name, ok
}

// IsTradingDay reports whether NYSE holds a session on date.
func IsTradingDay(date time.Time) bool {
	d := civil(date)
	if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	_, hol := Holiday(d)
	return !hol
}

// IsEarlyClose reports a 13:00 ET close on a trading day.
func IsEarlyClose(date time.Time) bool {
	d := civil(date)
	if !IsTradingDay(d) {
		return false
	}
	y := d.Year()
	thanksgiving := nthWeekday(y, time.November, time.Thursday, 4)
	switch {
	case d.Equal(thanksgiving.AddDate(0, 0, 1)):
		return true
	case d.Month() == time.July && d.Day() == 3 && d.Weekday() >= time.Monday && d.Weekday() <= time.Thursday:
		return true
	case d.Month() == time.December && d.Day() == 24 && d.Weekday() >= time.Monday && d.Weekday() <= time.Thursday:
		return true
	}
	return false
}

// Session returns the open and close instants of date's session (New York
// time). ok is false on a non-trading day.
func Session(date time.Time) (open, close time.Time, ok bool) {
	d := civil(date)
	if !IsTradingDay(d) {
		return time.Time{}, time.Time{}, false
	}
	y, m, day := d.Date()
	open = time.Date(y, m, day, OpenHour, OpenMinute, 0, 0, NewYork)
	if IsEarlyClose(d) {
		close = time.Date(y, m, day, EarlyCloseHour, EarlyCloseMn, 0, 0, NewYork)
	} else {
		close = time.Date(y, m, day, CloseHour, CloseMinute, 0, 0, NewYork)
	}
	return open, close, true
}

// IsOpen reports whether the cash session is open at instant t.
func IsOpen(t time.Time) bool {
	open, close, ok := Session(t.In(NewYork))
	return ok && !t.Before(open) && t.Before(close)
}

// NextTradingDay returns the first trading day strictly after date.
func NextTradingDay(date time.Time) time.Time {
	d := civil(date)
	for i := 0; i < 15; i++ {
		d = d.AddDate(0, 0, 1)
		if IsTradingDay(d) {
			return d
		}
	}
	panic(fmt.Sprintf("mktcal: no trading day within 15 days after %s", civil(date).Format("2006-01-02")))
}

// PrevTradingDay returns the last trading day strictly before date.
func PrevTradingDay(date time.Time) time.Time {
	d := civil(date)
	for i := 0; i < 15; i++ {
		d = d.AddDate(0, 0, -1)
		if IsTradingDay(d) {
			return d
		}
	}
	panic(fmt.Sprintf("mktcal: no trading day within 15 days before %s", civil(date).Format("2006-01-02")))
}

// TradingDaysBetween counts trading days in (from, to]; negative when to
// is before from.
func TradingDaysBetween(from, to time.Time) int {
	a, b := civil(from), civil(to)
	sign := 1
	if b.Before(a) {
		a, b, sign = b, a, -1
	}
	n := 0
	for d := a.AddDate(0, 0, 1); !d.After(b); d = d.AddDate(0, 0, 1) {
		if IsTradingDay(d) {
			n++
		}
	}
	return sign * n
}

// TradingDays lists the trading days in [from, to].
func TradingDays(from, to time.Time) []time.Time {
	var out []time.Time
	for d := civil(from); !d.After(civil(to)); d = d.AddDate(0, 0, 1) {
		if IsTradingDay(d) {
			out = append(out, d)
		}
	}
	return out
}

// yearHolidays computes the observed scheduled holidays of a year.
func yearHolidays(y int) map[time.Time]string {
	h := map[time.Time]string{}
	add := func(d time.Time, name string) { h[d] = name }

	// New Year's Day: a Saturday Jan 1 is not observed on Dec 31.
	ny := Date(y, time.January, 1)
	switch ny.Weekday() {
	case time.Sunday:
		add(ny.AddDate(0, 0, 1), "New Year's Day (observed)")
	case time.Saturday:
	default:
		add(ny, "New Year's Day")
	}
	if y >= 1998 {
		add(nthWeekday(y, time.January, time.Monday, 3), "Martin Luther King Jr. Day")
	}
	add(nthWeekday(y, time.February, time.Monday, 3), "Washington's Birthday")
	add(easter(y).AddDate(0, 0, -2), "Good Friday")
	add(lastWeekday(y, time.May, time.Monday), "Memorial Day")
	if y >= 2022 {
		addObserved(h, Date(y, time.June, 19), "Juneteenth")
	}
	addObserved(h, Date(y, time.July, 4), "Independence Day")
	add(nthWeekday(y, time.September, time.Monday, 1), "Labor Day")
	add(nthWeekday(y, time.November, time.Thursday, 4), "Thanksgiving Day")
	addObserved(h, Date(y, time.December, 25), "Christmas Day")
	return h
}

func addObserved(h map[time.Time]string, d time.Time, name string) {
	switch d.Weekday() {
	case time.Saturday:
		h[d.AddDate(0, 0, -1)] = name + " (observed)"
	case time.Sunday:
		h[d.AddDate(0, 0, 1)] = name + " (observed)"
	default:
		h[d] = name
	}
}

func nthWeekday(y int, m time.Month, wd time.Weekday, n int) time.Time {
	d := Date(y, m, 1)
	for d.Weekday() != wd {
		d = d.AddDate(0, 0, 1)
	}
	return d.AddDate(0, 0, 7*(n-1))
}

func lastWeekday(y int, m time.Month, wd time.Weekday) time.Time {
	d := Date(y, m+1, 1).AddDate(0, 0, -1)
	for d.Weekday() != wd {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// easter is Western Easter Sunday (anonymous Gregorian algorithm).
func easter(y int) time.Time {
	a := y % 19
	b, c := y/100, y%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return Date(y, time.Month(month), day)
}
