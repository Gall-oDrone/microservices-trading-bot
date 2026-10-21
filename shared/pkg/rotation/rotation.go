// Package rotation is the dual-momentum rule studied as a candidate next cell
// for the eToro demo (docs/backtest-readiness/DUAL-MOMENTUM-INDEX-STUDY-*.md):
// a small set of risk assets (NSDQ100, SPX500) and cash.
//
// Rule (Antonacci-style, decided once a month):
//   - on the last trading day of each month, compute each asset's return over
//     the lookback L trading days (optionally skipping the most recent `skip`
//     days, the "12-1" convention);
//   - relative momentum: pick the asset with the highest lookback return;
//   - absolute momentum: hold it only if its return beats the cash hurdle
//     (0, or the T-bill return over the same span); otherwise hold cash;
//   - the choice holds until the next month-end decision. Fill is at the
//     next bar's open, as in dailyrule and cfdsim.
//
// Between decision days the target is carried forward unchanged, so the
// output has one entry per bar like dailyrule.Trend and can be fed to
// cfdsim.SimulateRotation directly.
//
// This package is NOT frozen by any pre-registration yet. Once a
// pre-registration references it, its arithmetic is frozen like dailyrule's.
package rotation

import (
	"time"

	"bitso-trading-platform/shared/pkg/dailyrule"
)

// Flat is the target when no asset passes the absolute-momentum hurdle.
const Flat = -1

// Params of the rule.
type Params struct {
	// Lookback in trading days (bars). 252 = 12 months, 126 = 6 months.
	Lookback int
	// Skip is how many of the most recent bars to leave out of the lookback
	// return (21 for the "12-1" convention). 0 = none.
	Skip int
	// Hurdle returns the cash return over the lookback span ending at bar
	// date d, as a decimal (0.02 = 2 %). nil = 0 % hurdle.
	Hurdle func(d time.Time) float64
}

// Point is the rule's state at one bar.
type Point struct {
	// Decision is true on a month-end bar where the rule re-evaluated.
	Decision bool
	// Warm is true once every asset has Lookback bars behind it.
	Warm bool
	// Momentum is each asset's lookback return at this bar (decimal); only
	// filled on decision bars.
	Momentum []float64
	// Hurdle is the cash return used on a decision bar.
	Hurdle float64
	// Target is the asset index to hold after this bar's close, or Flat.
	Target int
}

// IsMonthEnd reports whether bars[i] is the last bar of its calendar month
// in the series (the next bar is in a different month, or i is the last bar).
func IsMonthEnd(bars []dailyrule.Bar, i int) bool {
	if i == len(bars)-1 {
		return true
	}
	a, b := bars[i].Date, bars[i+1].Date
	return a.Year() != b.Year() || a.Month() != b.Month()
}

// Evaluate runs the rule over aligned bar series (same length, same dates;
// see Align). It returns one Point per bar.
func Evaluate(assets [][]dailyrule.Bar, p Params) []Point {
	if len(assets) == 0 || len(assets[0]) == 0 {
		return nil
	}
	n := len(assets[0])
	for _, a := range assets {
		if len(a) != n {
			panic("rotation: asset series are not aligned")
		}
	}
	need := p.Lookback + p.Skip
	out := make([]Point, n)
	cur := Flat
	for i := 0; i < n; i++ {
		pt := Point{Target: cur}
		pt.Warm = i >= need
		if pt.Warm && IsMonthEnd(assets[0], i) {
			pt.Decision = true
			pt.Momentum = make([]float64, len(assets))
			end := i - p.Skip
			start := end - p.Lookback
			best, bestRet := Flat, 0.0
			for k, a := range assets {
				r := a[end].Close/a[start].Close - 1
				pt.Momentum[k] = r
				if best == Flat || r > bestRet {
					best, bestRet = k, r
				}
			}
			if p.Hurdle != nil {
				pt.Hurdle = p.Hurdle(assets[0][i].Date)
			}
			if bestRet > pt.Hurdle {
				cur = best
			} else {
				cur = Flat
			}
			pt.Target = cur
		}
		out[i] = pt
	}
	return out
}

// Targets returns only the target per bar.
func Targets(assets [][]dailyrule.Bar, p Params) []int {
	pts := Evaluate(assets, p)
	w := make([]int, len(pts))
	for i, pt := range pts {
		w[i] = pt.Target
	}
	return w
}

// Align keeps only the dates every series has, in order, so the returned
// series have identical length and dates. Bars must be sorted by date.
func Align(series ...[]dailyrule.Bar) [][]dailyrule.Bar {
	if len(series) == 0 {
		return nil
	}
	common := map[time.Time]int{}
	for _, s := range series {
		seen := map[time.Time]bool{}
		for _, b := range s {
			if !seen[b.Date] {
				seen[b.Date] = true
				common[b.Date]++
			}
		}
	}
	out := make([][]dailyrule.Bar, len(series))
	for k, s := range series {
		for _, b := range s {
			if common[b.Date] == len(series) {
				out[k] = append(out[k], b)
			}
		}
	}
	return out
}
