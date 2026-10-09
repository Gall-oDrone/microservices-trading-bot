package montecarlo

import (
	"math"
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/dailyrule"
)

// YearResult is the rule against buy-and-hold over one calendar year, the
// way cmd/daily-research runs a window: the rule's signal from the full
// history (so January starts warm), both entering at the window's first
// open and closing at its last close, each paying the costs.
type YearResult struct {
	Year        int     `json:"year"`
	From        string  `json:"from"`
	To          string  `json:"to"`
	TrendReturn float64 `json:"trend_return"` // fractions
	HoldReturn  float64 `json:"hold_return"`
	TrendMaxDD  float64 `json:"trend_max_dd"`
	HoldMaxDD   float64 `json:"hold_max_dd"`
	RoundTrips  int     `json:"round_trips"`
	BeatsHold   bool    `json:"beats_hold"`   // H2
	ShallowerDD bool    `json:"shallower_dd"` // H1
}

// Calendar is the per-year table and its base rates: the observed share of
// years in which each hypothesis held, which a Monte Carlo run should
// roughly reproduce before its probabilities are trusted.
type Calendar struct {
	Years         []YearResult `json:"years"`
	BeatsHold     int          `json:"beats_hold"`
	ShallowerDD   int          `json:"shallower_dd"`
	RateBeatsHold float64      `json:"rate_beats_hold"`
	RateShallower float64      `json:"rate_shallower_dd"`
}

// CalendarYears runs every calendar year from fromYear to the last bar's
// year (the last one partial). Years with fewer than 30 bars are skipped.
func CalendarYears(bars []dailyrule.Bar, sma int, fromYear int, c dailyrule.Costs) Calendar {
	out := Calendar{Years: []YearResult{}}
	if len(bars) == 0 {
		return out
	}
	want := dailyrule.Trend(bars, sma)
	hold := make([]bool, len(bars))
	for i := range hold {
		hold[i] = true
	}
	for y := fromYear; y <= bars[len(bars)-1].Date.Year(); y++ {
		from := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(y, 12, 31, 0, 0, 0, 0, time.UTC)
		lo := sort.Search(len(bars), func(i int) bool { return !bars[i].Date.Before(from) })
		hi := sort.Search(len(bars), func(i int) bool { return bars[i].Date.After(to) }) - 1
		if hi-lo+1 < 30 || lo < 1 {
			continue
		}
		t := dailyrule.Simulate(bars, want, lo, hi, c)
		h := dailyrule.Simulate(bars, hold, lo, hi, c)
		r := YearResult{Year: y, From: bars[lo].Date.Format("2006-01-02"), To: bars[hi].Date.Format("2006-01-02"),
			TrendReturn: t.ReturnPct / 100, HoldReturn: h.ReturnPct / 100, TrendMaxDD: t.MaxDDPct / 100, HoldMaxDD: h.MaxDDPct / 100,
			RoundTrips: t.RoundTrips}
		r.BeatsHold = t.ReturnPct > h.ReturnPct
		r.ShallowerDD = t.MaxDDPct < h.MaxDDPct
		if r.BeatsHold {
			out.BeatsHold++
		}
		if r.ShallowerDD {
			out.ShallowerDD++
		}
		out.Years = append(out.Years, r)
	}
	if n := len(out.Years); n > 0 {
		out.RateBeatsHold = float64(out.BeatsHold) / float64(n)
		out.RateShallower = float64(out.ShallowerDD) / float64(n)
	}
	return out
}

// Trip is one closed round trip of the rule, net of both legs' costs.
type Trip struct {
	Entry  string  `json:"entry"`
	Exit   string  `json:"exit"`
	Days   int     `json:"days"`
	Return float64 `json:"return"` // fraction, net
}

// Trips summarises the rule's closed round trips over a window.
type Trips struct {
	Count          int     `json:"count"`
	WinRate        float64 `json:"win_rate"`
	Mean           float64 `json:"mean"`   // expectancy per trip
	Median         float64 `json:"median"` // the typical trip
	AvgWin         float64 `json:"avg_win"`
	AvgLoss        float64 `json:"avg_loss"`
	Payoff         float64 `json:"payoff"` // avg win / |avg loss|
	Best           Trip    `json:"best"`
	Worst          Trip    `json:"worst"`
	Compounded     float64 `json:"compounded"`               // all trips chained
	CompoundedExB1 float64 `json:"compounded_ex_best"`       // without the best trip
	CompoundedExB3 float64 `json:"compounded_ex_best_three"` // without the best three
	MeanDays       float64 `json:"mean_days"`
	Open           *Trip   `json:"open"` // the trip still open at the window's end, marked at its last close (gross of the exit leg)
	Window         string  `json:"window"`
}

// TripStats runs the rule from the first bar on or after from to the last
// bar and summarises its closed round trips (the closing leg at the
// window's end is not a trip; it is reported as Open).
func TripStats(bars []dailyrule.Bar, sma int, from time.Time, c dailyrule.Costs) Trips {
	out := Trips{}
	lo := sort.Search(len(bars), func(i int) bool { return !bars[i].Date.Before(from) })
	hi := len(bars) - 1
	if lo < 1 || hi-lo < 2 {
		return out
	}
	out.Window = bars[lo].Date.Format("2006-01-02") + ".." + bars[hi].Date.Format("2006-01-02")
	_, tr := dailyrule.SimulateTrace(bars, dailyrule.Trend(bars, sma), lo, hi, c)
	var trips []Trip
	var entry *dailyrule.Fill
	for i := range tr.Fills {
		f := tr.Fills[i]
		switch {
		case f.Buy:
			entry = &tr.Fills[i]
		case entry != nil && !f.Final:
			// Equity in: the cash before the buy (units x price + fee); out:
			// the cash after the sell's fee.
			in := entry.Units*entry.Price + entry.Fee
			trips = append(trips, Trip{Entry: entry.Date.Format("2006-01-02"), Exit: f.Date.Format("2006-01-02"),
				Days: int(f.Date.Sub(entry.Date).Hours() / 24), Return: f.Cash/in - 1})
			entry = nil
		case entry != nil && f.Final:
			in := entry.Units*entry.Price + entry.Fee
			out.Open = &Trip{Entry: entry.Date.Format("2006-01-02"), Exit: f.Date.Format("2006-01-02"),
				Days: int(f.Date.Sub(entry.Date).Hours() / 24), Return: entry.Units*f.Price/in - 1}
			entry = nil
		}
	}
	out.Count = len(trips)
	if out.Count == 0 {
		return out
	}
	rets := make([]float64, len(trips))
	var wins, losses []float64
	var sum, days float64
	out.Best, out.Worst = trips[0], trips[0]
	for i, t := range trips {
		rets[i] = t.Return
		sum += t.Return
		days += float64(t.Days)
		if t.Return > 0 {
			wins = append(wins, t.Return)
		} else {
			losses = append(losses, t.Return)
		}
		if t.Return > out.Best.Return {
			out.Best = t
		}
		if t.Return < out.Worst.Return {
			out.Worst = t
		}
	}
	out.Mean = sum / float64(out.Count)
	out.MeanDays = days / float64(out.Count)
	out.WinRate = float64(len(wins)) / float64(out.Count)
	out.AvgWin, out.AvgLoss = mean(wins), mean(losses)
	if out.AvgLoss != 0 {
		out.Payoff = out.AvgWin / math.Abs(out.AvgLoss)
	}
	sorted := append([]float64(nil), rets...)
	sort.Float64s(sorted)
	if n := len(sorted); n%2 == 1 {
		out.Median = sorted[n/2]
	} else {
		out.Median = (sorted[n/2-1] + sorted[n/2]) / 2
	}
	chain := func(xs []float64) float64 {
		p := 1.0
		for _, x := range xs {
			p *= 1 + x
		}
		return p - 1
	}
	out.Compounded = chain(sorted)
	if len(sorted) > 1 {
		out.CompoundedExB1 = chain(sorted[:len(sorted)-1])
	}
	if len(sorted) > 3 {
		out.CompoundedExB3 = chain(sorted[:len(sorted)-3])
	}
	return out
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}
