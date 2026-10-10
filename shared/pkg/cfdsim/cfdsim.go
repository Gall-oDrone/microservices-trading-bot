// Package cfdsim simulates the frozen daily trend rule (shared/pkg/dailyrule)
// on a position-based instrument: an x1 index CFD that pays overnight
// financing, or an ETF that pays a fixed commission per order.
//
// It is a new simulator, not a change to dailyrule.Simulate: the Bitso
// pre-registrations freeze that loop's arithmetic. The fill model and the
// order of operations are the same (decide at day t's close, fill at day
// t+1's open, all-in/all-out, close-out at the window's last close), and with
// zero financing and zero fixed fees Simulate reproduces dailyrule.Simulate
// bit for bit (TestMatchesDailyrule).
//
// Costs added on top:
//   - financing: for every night a position is held, NightlyRate(date) times
//     the position's value at that close, times the calendar nights until the
//     next bar (Friday -> Monday = 3, the usual triple weekend charge; a
//     holiday adds its night). It is debited from cash, as eToro debits the
//     account balance;
//   - FeePerLeg: a fixed commission expressed as a decimal of the notional
//     (1.50 USD on a 1,100 USD order = 0.001364), charged like the spread.
package cfdsim

import (
	"math"
	"time"

	"bitso-trading-platform/shared/pkg/dailyrule"
)

// Costs of trading one instrument at x1.
type Costs struct {
	// SpreadPerLeg is the spread (and markup) paid on each fill as a decimal
	// of notional: half the round-trip spread.
	SpreadPerLeg float64
	// FeePerLeg is a fixed commission per fill as a decimal of notional.
	FeePerLeg float64
	// NightlyRate is the financing charge per held night as a decimal of the
	// position value, for the night after the bar dated d. nil: none.
	NightlyRate func(d time.Time) float64
}

// leg is the total proportional cost of one fill.
func (c Costs) leg() float64 { return c.SpreadPerLeg + c.FeePerLeg }

// Result is one simulated window, starting from equity 1.0.
type Result struct {
	ReturnPct    float64 `json:"return_pct"`
	CAGRPct      float64 `json:"cagr_pct"`
	RoundTrips   int     `json:"round_trips"`
	ExposurePct  float64 `json:"exposure_pct"`
	MaxDDPct     float64 `json:"max_dd_pct"`
	SharpeAnn    float64 `json:"sharpe_ann"`     // mean/stdev of daily equity returns x sqrt(252), rf = 0
	TradeCostPct float64 `json:"trade_cost_pct"` // spread + fees, % of starting equity
	FinancePct   float64 `json:"finance_pct"`    // overnight financing, % of starting equity
	NightsHeld   int     `json:"nights_held"`
	Bars         int     `json:"bars"`
	// MinEquity is the lowest mark-to-close equity (start 1.0). Below 0.5 of
	// the position's margin an eToro (ESMA) account is closed out; the
	// simulator does not stop, it flags it (MarginCloseOut) so a multi-decade
	// CFD hold is read as "would have been closed out", not as a >100 %
	// drawdown.
	MinEquity      float64 `json:"min_equity"`
	MarginCloseOut bool    `json:"margin_close_out"`
}

// Simulate trades bars[lo..hi] (inclusive) with the dailyrule convention:
// want[i] is the position desired after day i's close, filled at
// bars[i+1].Open.
func Simulate(bars []dailyrule.Bar, want []bool, lo, hi int, c Costs) Result {
	const start = 1.0
	cash, units := start, 0.0
	held := false
	peak, maxDD, trade, finance := start, 0.0, 0.0, 0.0
	exposed, trips, nights := 0, 0, 0
	leg := c.leg()
	prevEq := start
	minEq, closeOut, margin := start, false, 0.0
	var rets []float64

	for i := lo; i <= hi; i++ {
		var desire bool
		if i > 0 {
			desire = want[i-1]
		}
		o := bars[i].Open
		switch {
		case desire && !held:
			fee := cash * leg
			units = (cash - fee) / o
			margin = cash - fee
			trade += fee
			cash, held = 0, true
		case !desire && held:
			gross := units * o
			fee := gross * leg
			cash += gross - fee
			trade += fee
			units, held = 0, false
			trips++
		}
		if held {
			exposed++
		}
		// Financing for the night(s) after this close, while still held.
		if held && i < hi && c.NightlyRate != nil {
			n := int(math.Round(bars[i+1].Date.Sub(bars[i].Date).Hours() / 24))
			if n < 1 {
				n = 1
			}
			f := units * bars[i].Close * c.NightlyRate(bars[i].Date) * float64(n)
			cash -= f
			finance += f
			nights += n
		}
		eq := cash + units*bars[i].Close
		if eq < minEq {
			minEq = eq
		}
		// ESMA close-out: equity below half of the margin committed at entry
		// (x1: the invested amount) while a financed (CFD) position is open.
		// An ETF holding (no NightlyRate) is not margined.
		if held && c.NightlyRate != nil && eq < 0.5*margin {
			closeOut = true
		}
		if eq > peak {
			peak = eq
		}
		if dd := (peak - eq) / peak; dd > maxDD {
			maxDD = dd
		}
		if i > lo && prevEq > 0 {
			rets = append(rets, eq/prevEq-1)
		}
		prevEq = eq
	}
	if held {
		gross := units * bars[hi].Close
		fee := gross * leg
		cash += gross - fee
		trade += fee
		trips++
	}
	days := hi - lo + 1
	r := Result{
		ReturnPct:      (cash/start - 1) * 100,
		RoundTrips:     trips,
		ExposurePct:    100 * float64(exposed) / float64(days),
		MaxDDPct:       maxDD * 100,
		TradeCostPct:   trade / start * 100,
		FinancePct:     finance / start * 100,
		NightsHeld:     nights,
		Bars:           days,
		SharpeAnn:      sharpe(rets),
		MinEquity:      minEq,
		MarginCloseOut: closeOut,
	}
	if days > 1 && cash > 0 {
		r.CAGRPct = (math.Pow(cash/start, 252/float64(days-1)) - 1) * 100
	}
	return r
}

func sharpe(rets []float64) float64 {
	if len(rets) < 2 {
		return 0
	}
	var m float64
	for _, x := range rets {
		m += x
	}
	m /= float64(len(rets))
	var v float64
	for _, x := range rets {
		v += (x - m) * (x - m)
	}
	sd := math.Sqrt(v / float64(len(rets)-1))
	if sd == 0 {
		return 0
	}
	return m / sd * math.Sqrt(252)
}

// FixedNightly is a constant financing rate per night (decimal).
func FixedNightly(rate float64) func(time.Time) float64 {
	return func(time.Time) float64 { return rate }
}

// RateLinked is eToro-style financing: (reference rate + markup) / 365 per
// night, the reference being an annual decimal looked up by date (the last
// known value on or before d). markup is an annual decimal.
func RateLinked(ref func(time.Time) (float64, bool), markup float64) func(time.Time) float64 {
	return func(d time.Time) float64 {
		r, ok := ref(d)
		if !ok {
			r = 0
		}
		v := (r + markup) / 365
		if v < 0 {
			return 0
		}
		return v
	}
}
