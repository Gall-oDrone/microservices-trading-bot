package dailyledger

import (
	"strings"
	"time"
)

// Mexico is the timezone of the daily bars.
var Mexico = func() *time.Location {
	loc, err := time.LoadLocation("America/Mexico_City")
	if err != nil {
		return time.FixedZone("CST", -6*3600)
	}
	return loc
}()

// BaseQuote splits "btc_mxn" into ("btc", "mxn").
func BaseQuote(book string) (string, string) {
	b, q, _ := strings.Cut(strings.ToLower(book), "_")
	return b, q
}

// FeeQuote converts a leg's fees to the quote currency. Bitso charges buys
// in the asset received (base) and sells in quote; base fees are valued at
// the leg's average price. Fees in any other currency are ignored and
// reported in the second return value.
func FeeQuote(book string, l Leg) (fee float64, unknown []string) {
	base, quote := BaseQuote(book)
	for ccy, amt := range l.Fees {
		switch strings.ToLower(ccy) {
		case base:
			fee += amt * l.AvgPrice
		case quote:
			fee += amt
		default:
			unknown = append(unknown, ccy)
		}
	}
	return fee, unknown
}

// FeeBps is the leg's fee in bps of its notional (0 if no notional).
func FeeBps(book string, l Leg) float64 {
	if l.Notional <= 0 {
		return 0
	}
	f, _ := FeeQuote(book, l)
	return f / l.Notional * 1e4
}

// SlippageBps is how much worse than ref the leg filled, in bps: positive
// is adverse (bought above / sold below ref). ok is false without a ref.
func SlippageBps(l Leg, ref float64) (float64, bool) {
	if ref <= 0 || l.AvgPrice <= 0 {
		return 0, false
	}
	if l.Side == "sell" {
		return (1 - l.AvgPrice/ref) * 1e4, true
	}
	return (l.AvgPrice/ref - 1) * 1e4, true
}

// ExpectedLastBar is the latest Mexico City date whose daily candle has
// closed at now (yesterday, Mexico time); same as the executor.
func ExpectedLastBar(now time.Time) string {
	return now.In(Mexico).AddDate(0, 0, -1).Format("2006-01-02")
}

// MissingDays lists Mexico City dates in (lastRecorded, expected] that have
// no record. lastRecorded "" returns nil.
func MissingDays(lastRecorded string, now time.Time) []string {
	if lastRecorded == "" {
		return nil
	}
	last, err := time.Parse("2006-01-02", lastRecorded)
	if err != nil {
		return nil
	}
	want, _ := time.Parse("2006-01-02", ExpectedLastBar(now))
	var out []string
	for d := last.AddDate(0, 0, 1); !d.After(want); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format("2006-01-02"))
	}
	return out
}

// LastStagePosition is the position after the latest stage record, or flat.
func LastStagePosition(recs []Record) Position {
	pos := Position{State: "flat"}
	for _, r := range recs { // sorted by bar_date
		if r.Mode == "stage" && r.Stage != nil {
			pos = r.Stage.PositionAfter
		}
	}
	return pos
}

// LegsOn counts stage legs whose fill date is day.
func LegsOn(recs []Record, day string) int {
	n := 0
	for _, r := range recs {
		if r.Stage != nil && r.Stage.Leg != nil && r.Decision.FillDate == day {
			n++
		}
	}
	return n
}
