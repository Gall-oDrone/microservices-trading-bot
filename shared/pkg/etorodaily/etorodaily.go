// Package etorodaily turns eToro's live daily candles into the closed,
// trading-day bars the daily rule consumes, and writes them in the CSV
// format the research tools already read (bitsodaily.WriteCSV/ReadCSV).
//
// Day labelling (verified on the demo API, 2026-10-10): the live route
// /api/v1/market-data/instruments/{id}/history/candles/desc/OneDay/{n}
// buckets UTC days (fromDate = D 00:00Z), so bar D closes at D+1 00:00 UTC,
// i.e. 20:00 New York time in summer and 19:00 in winter, after the cash
// close. It returns at most 1000 bars (back to Dec 2022 on 2026-10-10).
//
// The index CFDs also quote outside the cash market: on US holidays (the
// underlying futures trade a short session) and, since ~September 2026, at
// weekends (synthetic weekend prices). Those bars are dropped by default so
// the series has one bar per NYSE trading day, matching the official index
// series (Yahoo ^NDX / ^GSPC) used for long-history research and the NYSE
// calendar the executor acts on. Which bar set the forward test uses is
// frozen by its pre-registration, not here.
package etorodaily

import (
	"context"
	"fmt"
	"math"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/mktcal"
)

// Day is one UTC day, the bucket of the live daily route.
const Day = 24 * time.Hour

// Options control which bars are kept.
type Options struct {
	// KeepNonTradingDays keeps weekend and NYSE-holiday bars.
	KeepNonTradingDays bool
}

// Report counts what ToRows dropped and why.
type Report struct {
	In             int    `json:"in"`
	Kept           int    `json:"kept"`
	InProgress     int    `json:"in_progress"`     // bucket not closed yet
	Weekend        int    `json:"weekend"`         // Saturday/Sunday UTC bars
	Holiday        int    `json:"holiday"`         // NYSE holiday bars
	Invalid        int    `json:"invalid"`         // non-positive or inconsistent OHLC
	Duplicate      int    `json:"duplicate"`       // same UTC date twice
	First          string `json:"first,omitempty"` // first kept date
	Last           string `json:"last,omitempty"`  // last kept date
	MissingTrading int    `json:"missing_trading_days"`
}

// Fetch reads the newest count daily candles (≤ etoro.MaxCandles) and
// converts them with ToRows.
func Fetch(ctx context.Context, c *etoro.Client, instrumentID int64, count int, now time.Time, opt Options) ([]bitsodaily.Row, Report, error) {
	cs, err := c.Candles(ctx, instrumentID, etoro.OneDay, count)
	if err != nil {
		return nil, Report{}, err
	}
	rows, rep := ToRows(cs, now, opt)
	return rows, rep, nil
}

// ToRows converts live daily candles to closed bars sorted by date,
// labelled by UTC date. A bucket is closed when fromDate+24h <= now.
func ToRows(cs []etoro.Candle, now time.Time, opt Options) ([]bitsodaily.Row, Report) {
	rep := Report{In: len(cs)}
	byDate := map[string]bitsodaily.Row{}
	for _, c := range cs {
		st := c.FromDate.UTC()
		day := time.Date(st.Year(), st.Month(), st.Day(), 0, 0, 0, 0, time.UTC)
		if day.Add(Day).After(now) {
			rep.InProgress++
			continue
		}
		if !opt.KeepNonTradingDays {
			if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
				rep.Weekend++
				continue
			}
			if !mktcal.IsTradingDay(day) {
				rep.Holiday++
				continue
			}
		}
		if !validOHLC(c.Open, c.High, c.Low, c.Close) {
			rep.Invalid++
			continue
		}
		date := day.Format("2006-01-02")
		if _, dup := byDate[date]; dup {
			rep.Duplicate++
		}
		byDate[date] = bitsodaily.Row{Date: date, Open: c.Open, High: c.High, Low: c.Low, Close: c.Close, BucketStartUTC: day}
	}
	rows := make([]bitsodaily.Row, 0, len(byDate))
	for _, r := range byDate {
		rows = append(rows, r)
	}
	sortRows(rows)
	rep.Kept = len(rows)
	if len(rows) > 0 {
		rep.First, rep.Last = rows[0].Date, rows[len(rows)-1].Date
		if !opt.KeepNonTradingDays {
			rep.MissingTrading = len(MissingTradingDays(rows))
		}
	}
	return rows, rep
}

func validOHLC(o, h, l, c float64) bool {
	for _, v := range []float64{o, h, l, c} {
		if !(v > 0) || math.IsInf(v, 0) {
			return false
		}
	}
	return h >= math.Max(o, c) && l <= math.Min(o, c)
}

func sortRows(rows []bitsodaily.Row) {
	for i := 1; i < len(rows); i++ { // insertion sort: inputs are nearly sorted
		for j := i; j > 0 && rows[j].Date < rows[j-1].Date; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// MissingTradingDays lists NYSE trading days between the first and last row
// that have no bar.
func MissingTradingDays(rows []bitsodaily.Row) []string {
	if len(rows) < 2 {
		return nil
	}
	have := make(map[string]bool, len(rows))
	for _, r := range rows {
		have[r.Date] = true
	}
	first, _ := time.Parse("2006-01-02", rows[0].Date)
	last, _ := time.Parse("2006-01-02", rows[len(rows)-1].Date)
	var out []string
	for _, d := range mktcal.TradingDays(first, last) {
		if s := d.Format("2006-01-02"); !have[s] {
			out = append(out, s)
		}
	}
	return out
}

// LatestClosedTradingDay is the most recent NYSE trading day whose UTC
// bucket has closed at now: the bar a run at now must find as the last row,
// or it is acting on stale data.
func LatestClosedTradingDay(now time.Time) time.Time {
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	if !mktcal.IsTradingDay(d) {
		d = mktcal.PrevTradingDay(d)
	}
	return d
}

// CheckFresh errors unless the last row is LatestClosedTradingDay(now).
func CheckFresh(rows []bitsodaily.Row, now time.Time) error {
	if len(rows) == 0 {
		return fmt.Errorf("etorodaily: no closed bars")
	}
	want := LatestClosedTradingDay(now).Format("2006-01-02")
	if got := rows[len(rows)-1].Date; got != want {
		return fmt.Errorf("etorodaily: stale bars: last closed bar %s, want %s", got, want)
	}
	return nil
}

// Bars converts rows to the rule's bar type.
func Bars(rows []bitsodaily.Row) []dailyrule.Bar {
	out := make([]dailyrule.Bar, 0, len(rows))
	for _, r := range rows {
		d, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			continue
		}
		out = append(out, dailyrule.Bar{Date: d, Open: r.Open, High: r.High, Low: r.Low, Close: r.Close})
	}
	return out
}
