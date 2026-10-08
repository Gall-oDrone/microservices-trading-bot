package dailyrule

import "time"

// The registered daily simulator, moved from cmd/daily-research (plan §7
// item 5) so cmd/daily-research and services/backtesting compute the same
// numbers. Like the rule, the arithmetic and its order are frozen: the
// registered evidence was produced by exactly this loop. SimulateTrace only
// records what the loop already computes; it never changes a value.

// Costs are per-leg costs as decimals (commission + slippage), e.g. 0.0088
// for 78 bps taker fee + 10 bps slippage.
type Costs struct{ Buy, Sell float64 }

// Result is one simulated window, starting from equity 1.0.
type Result struct {
	ReturnPct   float64
	RoundTrips  int
	ExposurePct float64
	MaxDDPct    float64
	CostPct     float64 // total costs as % of starting equity
}

// Fill is one leg the simulator executed. Amounts are per 1.0 of starting
// equity: scale by the initial balance for money.
type Fill struct {
	Index int // bar index of the fill
	Date  time.Time
	Buy   bool
	Price float64 // the bar's open, or the final close for the closing leg
	Units float64 // base units bought or sold
	Fee   float64 // cost charged on this leg
	Cash  float64 // cash after the leg
	Final bool    // the close-out at the window's final close
}

// EquityPoint is the mark-to-close equity after each bar of the window.
type EquityPoint struct {
	Index    int
	Date     time.Time
	Equity   float64
	Cash     float64
	Held     bool
	Drawdown float64 // fraction below the running peak
}

// Trace is what SimulateTrace records.
type Trace struct {
	Fills  []Fill
	Equity []EquityPoint
}

// Simulate trades bars[lo..hi] (inclusive). want[i] is the position desired
// after day i's close; it is filled at bars[i+1].Open. So the first possible
// fill is at bars[lo].Open, driven by want[lo-1]; when lo == 0 there is no
// prior close to decide on, and the first fill is at bars[1].Open.
func Simulate(bars []Bar, want []bool, lo, hi int, c Costs) Result {
	return simulate(bars, want, lo, hi, c, nil)
}

// SimulateTrace is Simulate plus the fills and the equity curve.
func SimulateTrace(bars []Bar, want []bool, lo, hi int, c Costs) (Result, Trace) {
	var tr Trace
	res := simulate(bars, want, lo, hi, c, &tr)
	return res, tr
}

func simulate(bars []Bar, want []bool, lo, hi int, c Costs, tr *Trace) Result {
	const start = 1.0
	cash, units := start, 0.0
	held := false
	peak, maxDD, costs := start, 0.0, 0.0
	exposed, trips := 0, 0

	for i := lo; i <= hi; i++ {
		// Decision from the previous close, filled at today's open.
		var desire bool
		if i > 0 {
			desire = want[i-1]
		}
		o := bars[i].Open
		switch {
		case desire && !held:
			fee := cash * c.Buy
			units = (cash - fee) / o
			costs += fee
			cash, held = 0, true
			if tr != nil {
				tr.Fills = append(tr.Fills, Fill{Index: i, Date: bars[i].Date, Buy: true, Price: o, Units: units, Fee: fee, Cash: cash})
			}
		case !desire && held:
			gross := units * o
			fee := gross * c.Sell
			cash = gross - fee
			costs += fee
			if tr != nil {
				tr.Fills = append(tr.Fills, Fill{Index: i, Date: bars[i].Date, Price: o, Units: units, Fee: fee, Cash: cash})
			}
			units, held = 0, false
			trips++
		}
		if held {
			exposed++
		}
		eq := cash + units*bars[i].Close
		if eq > peak {
			peak = eq
		}
		dd := (peak - eq) / peak
		if dd > maxDD {
			maxDD = dd
		}
		if tr != nil {
			tr.Equity = append(tr.Equity, EquityPoint{Index: i, Date: bars[i].Date, Equity: eq, Cash: cash, Held: held, Drawdown: dd})
		}
	}
	if held { // close out at the final close, with costs
		gross := units * bars[hi].Close
		fee := gross * c.Sell
		cash = gross - fee
		costs += fee
		trips++
		if tr != nil {
			tr.Fills = append(tr.Fills, Fill{Index: hi, Date: bars[hi].Date, Price: bars[hi].Close, Units: units, Fee: fee, Cash: cash, Final: true})
		}
	}
	days := hi - lo + 1
	return Result{
		ReturnPct:   (cash/start - 1) * 100,
		RoundTrips:  trips,
		ExposurePct: 100 * float64(exposed) / float64(days),
		MaxDDPct:    maxDD * 100,
		CostPct:     costs / start * 100,
	}
}
