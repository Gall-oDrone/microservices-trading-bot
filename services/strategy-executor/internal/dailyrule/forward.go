// Package dailyrule forwards to bitso-trading-platform/shared/pkg/dailyrule,
// where the frozen daily trend rule moved on 2026-10-08 (plan §7 item 5) so
// that services/backtesting can run the identical rule. The aliases keep
// every caller in this module unchanged; there is no second implementation.
package dailyrule

import shared "bitso-trading-platform/shared/pkg/dailyrule"

// Bar is one daily OHLC bar, labelled by its calendar date.
type Bar = shared.Bar

// Point is the rule's state at one bar.
type Point = shared.Point

// MaxGapDays is the longest calendar gap the trend SMA will average across.
const MaxGapDays = shared.MaxGapDays

// Evaluate returns the rule's state at every bar. bars must be sorted by date.
func Evaluate(bars []Bar, n int) []Point { return shared.Evaluate(bars, n) }

// Trend returns only the long/flat decision at every bar.
func Trend(bars []Bar, n int) []bool { return shared.Trend(bars, n) }

// Costs are per-leg decimal costs (commission + slippage).
type Costs = shared.Costs

// Result is one simulated window, starting from equity 1.0.
type Result = shared.Result

// Simulate is the registered daily simulator (shared.Simulate).
func Simulate(bars []Bar, want []bool, lo, hi int, c Costs) Result {
	return shared.Simulate(bars, want, lo, hi, c)
}
