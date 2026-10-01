// Package dailyrule holds the frozen daily trend rule used by the forward
// tests pre-registered in docs/backtest-readiness/FORWARD-TEST-PREREGISTRATION-*.
//
// Rule: long while the day's close is strictly above the simple average of
// the last N closes (including that day); otherwise flat. The decision taken
// at day t's close is executed at day t+1's open.
//
// cmd/daily-research (the registered evaluation engine) and
// cmd/daily-executor (the daily stage runner) both call this package, so the
// signal they compute cannot drift apart. Do NOT change the arithmetic here:
// the pre-registrations freeze it, and the running-sum order is what makes
// the results bit-identical to the registered evidence.
package dailyrule

import "time"

// Bar is one daily OHLC bar, labelled by its calendar date.
type Bar struct {
	Date                   time.Time
	Open, High, Low, Close float64
}

// MaxGapDays is the longest calendar gap the trend SMA will average across.
// Short holes (a few missing days) are tolerated; a multi-month hole is not,
// because an SMA spanning it would blend prices from different market regimes
// into one number. After a longer gap the warm-up restarts.
const MaxGapDays = 7

// Point is the rule's state at one bar.
type Point struct {
	// Warm is true once N consecutive (gap-free) closes are available.
	Warm bool
	// SMA is the N-day simple average of closes; zero when not Warm.
	SMA float64
	// Long is the rule's decision at this bar's close.
	Long bool
}

// Evaluate returns the rule's state at every bar. bars must be sorted by date.
func Evaluate(bars []Bar, n int) []Point {
	out := make([]Point, len(bars))
	var win []float64
	sum := 0.0
	for i, b := range bars {
		if i > 0 && b.Date.Sub(bars[i-1].Date) > MaxGapDays*24*time.Hour {
			win, sum = win[:0], 0 // restart warm-up after a long gap
		}
		win = append(win, b.Close)
		sum += b.Close
		if len(win) > n {
			sum -= win[0]
			win = win[1:]
		}
		if len(win) == n {
			sma := sum / float64(n)
			out[i] = Point{Warm: true, SMA: sma, Long: b.Close > sma}
		}
	}
	return out
}

// Trend returns only the long/flat decision at every bar.
func Trend(bars []Bar, n int) []bool {
	pts := Evaluate(bars, n)
	w := make([]bool, len(pts))
	for i, p := range pts {
		w[i] = p.Long
	}
	return w
}
