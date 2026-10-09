// Package execsim replays an execution schedule against archived public
// trades, for the offline execution study (plan §6.4.13). It answers one
// question: given the trades that actually printed, what would a leg have
// cost under a different maker window, repricing rule or child-order
// slicing than the one the daily executor uses today (one post-only order at
// the best price, 60 minutes, then a market order for the rest)?
//
// The model is deliberately simple and its limits are stated, not hidden:
//
//   - The archive holds trades, not the book. Each trade says which side was
//     resting (maker side), so a trade whose maker was a buyer printed at the
//     best bid and one whose maker was a seller printed at the best ask. The
//     last such print (if fresher than QuoteMaxAge) stands in for the best
//     bid or ask; otherwise the last trade ± an assumed half-spread does
//     (measured by the capacity page and the book sampler).
//   - Queue position is unknown. Two fill models bracket it: Through (a trade
//     must print strictly past our price, so everything ahead of us at that
//     level traded first: conservative) and Touch (a trade at our price fills
//     us: optimistic). Fills are capped by the traded amount.
//   - A market order pays the half-spread plus an optional impact term
//     (ImpactBps, e.g. the square-root law) at the last trade price.
//   - Our own orders do not move the market (true at stage sizes; the impact
//     term is the only allowance for larger ones).
//
// Results are research only. The live executor and the pre-registered costs
// do not change because of them; any execution change needs its own
// pre-registration.
package execsim

import (
	"math"
	"sort"
	"time"
)

// Trade is one public trade.
type Trade struct {
	At     time.Time
	Price  float64
	Amount float64 // base currency
	// MakerSide is +1 when the resting order was a buy (the trade printed at
	// the best bid), -1 when it was a sell (at the best ask), 0 if unknown.
	MakerSide int
}

// FillModel decides when a resting order is filled by a printed trade.
type FillModel int

const (
	// Through fills only on trades strictly past the resting price.
	Through FillModel = iota
	// Touch fills on trades at or past the resting price.
	Touch
)

func (m FillModel) String() string {
	if m == Touch {
		return "touch"
	}
	return "through"
}

// Params is one execution schedule for one leg.
type Params struct {
	Buy bool
	Qty float64 // base currency
	// Window is how long the leg may rest as a maker before the market
	// fallback. 0 sends a market order at the start.
	Window time.Duration
	// Slices splits the leg into child orders spread evenly over Window; each
	// rests Window/Slices and then sends its own remainder at market. 1 (or
	// less) is a single order, as the executor does today.
	Slices int
	// RepriceEvery re-pegs the resting price to the best price (see
	// QuoteMaxAge) at this interval. 0 never reprices, as today.
	RepriceEvery time.Duration
	// QuoteMaxAge is how old a bid or ask print may be to stand in for the
	// best price. 0 always uses the last trade ± HalfSpreadBps.
	QuoteMaxAge   time.Duration
	HalfSpreadBps float64
	Model         FillModel
	MakerFeeBps   float64
	TakerFeeBps   float64
	// ImpactBps is the market order's cost beyond the half-spread for a
	// given quantity. nil means none.
	ImpactBps func(qty float64) float64
}

// Result is one simulated leg. Costs are bps of the arrival price (the last
// trade at the start); positive is a cost.
type Result struct {
	Arrival  float64
	AvgPrice float64
	MakerQty float64
	TakerQty float64
	FeeBps   float64 // notional-weighted fees
	PriceBps float64 // average price against arrival, + adverse
	CostBps  float64 // PriceBps + FeeBps: implementation shortfall
	End      time.Time
}

// MakerShare is the fraction of the quantity filled as a maker.
func (r Result) MakerShare() float64 {
	if q := r.MakerQty + r.TakerQty; q > 0 {
		return r.MakerQty / q
	}
	return 0
}

// VsBps prices the fill against another reference (e.g. the day's open, the
// paper account's price), fees included; positive is a cost.
func (r Result) VsBps(ref float64, buy bool) float64 {
	if ref <= 0 || r.AvgPrice <= 0 {
		return 0
	}
	px := (r.AvgPrice/ref - 1) * 1e4
	if !buy {
		px = -px
	}
	return px + r.FeeBps
}

// lastAt returns the index of the last trade at or before t, or -1.
func lastAt(trades []Trade, t time.Time) int {
	return sort.Search(len(trades), func(i int) bool { return trades[i].At.After(t) }) - 1
}

// quote returns the best price on one side as of trades[i]: bid (side +1)
// or ask (side -1). It uses the last print with that maker side within
// maxAge, never through the last trade, else the last trade ± hs.
func quote(trades []Trade, i, side int, hs float64, maxAge time.Duration) float64 {
	last := trades[i].Price
	if maxAge > 0 {
		oldest := trades[i].At.Add(-maxAge)
		for j := i; j >= 0 && !trades[j].At.Before(oldest); j-- {
			if trades[j].MakerSide == side {
				if side > 0 {
					return math.Min(trades[j].Price, last)
				}
				return math.Max(trades[j].Price, last)
			}
		}
	}
	return last * (1 - float64(side)*hs)
}

// Run simulates p from start. trades must be sorted by time. ok is false when
// there is no trade at or before start to price the arrival.
func Run(trades []Trade, start time.Time, p Params) (Result, bool) {
	var r Result
	i0 := lastAt(trades, start)
	if i0 < 0 || p.Qty <= 0 {
		return r, false
	}
	r.Arrival = trades[i0].Price
	slices := p.Slices
	if slices < 1 || p.Window <= 0 {
		slices = 1
	}
	childQty := p.Qty / float64(slices)
	step := p.Window / time.Duration(slices)
	hs := p.HalfSpreadBps / 1e4
	sign, side := 1.0, 1 // a buy rests on the bid and takes the ask
	if !p.Buy {
		sign, side = -1, -1
	}
	var makerNotional, takerNotional float64

	for k := 0; k < slices; k++ {
		cs := start.Add(time.Duration(k) * step)
		ce := cs.Add(step)
		rem := childQty
		if p.Window > 0 {
			i := lastAt(trades, cs)
			price := quote(trades, i, side, hs, p.QuoteMaxAge) // best bid for a buy, best ask for a sell
			nextReprice := cs.Add(p.RepriceEvery)
			for j := i + 1; j < len(trades) && rem > 0; j++ {
				t := trades[j]
				if !t.At.Before(ce) {
					break
				}
				if p.RepriceEvery > 0 && !t.At.Before(nextReprice) {
					price = quote(trades, j-1, side, hs, p.QuoteMaxAge)
					for !t.At.Before(nextReprice) {
						nextReprice = nextReprice.Add(p.RepriceEvery)
					}
				}
				if fills(t.Price, price, p.Buy, p.Model) {
					take := math.Min(rem, t.Amount)
					rem -= take
					r.MakerQty += take
					makerNotional += take * price
				}
			}
		}
		if rem > 1e-12 {
			at := lastAt(trades, ce)
			if p.Window <= 0 {
				at = i0
			}
			ref := quote(trades, at, -side, hs, p.QuoteMaxAge) // a buy takes the ask
			var impact float64
			if p.ImpactBps != nil {
				impact = p.ImpactBps(rem) / 1e4
			}
			r.TakerQty += rem
			takerNotional += rem * ref * (1 + sign*impact)
		}
		r.End = ce
	}
	notional := makerNotional + takerNotional
	q := r.MakerQty + r.TakerQty
	r.AvgPrice = notional / q
	r.FeeBps = (makerNotional*p.MakerFeeBps + takerNotional*p.TakerFeeBps) / notional
	r.PriceBps = sign * (r.AvgPrice/r.Arrival - 1) * 1e4
	r.CostBps = r.PriceBps + r.FeeBps
	return r, true
}

func fills(tradePx, restPx float64, buy bool, m FillModel) bool {
	if m == Touch {
		if buy {
			return tradePx <= restPx
		}
		return tradePx >= restPx
	}
	if buy {
		return tradePx < restPx
	}
	return tradePx > restPx
}

// MaxGap is the longest stretch without a trade in [from, to], counting the
// edges, so a missing archive hour shows up as a gap.
func MaxGap(trades []Trade, from, to time.Time) time.Duration {
	prev := from
	var gap time.Duration
	for i := lastAt(trades, from) + 1; i < len(trades) && !trades[i].At.After(to); i++ {
		if d := trades[i].At.Sub(prev); d > gap {
			gap = d
		}
		prev = trades[i].At
	}
	if d := to.Sub(prev); d > gap {
		gap = d
	}
	return gap
}

// Summary aggregates many legs of one schedule.
type Summary struct {
	Legs          int     `json:"legs"`
	MakerShare    float64 `json:"maker_share"`     // Σ maker qty / Σ qty
	FullMakerRate float64 `json:"full_maker_rate"` // legs with no market fallback
	MeanBps       float64 `json:"mean_cost_bps"`
	MedianBps     float64 `json:"median_cost_bps"`
	P90Bps        float64 `json:"p90_cost_bps"`
	StdBps        float64 `json:"std_cost_bps"`
	MeanVsOpenBps float64 `json:"mean_vs_open_bps"` // against the day's open (the paper price)
	StdVsOpenBps  float64 `json:"std_vs_open_bps"`  // tracking error against the paper account
}

// Leg is one simulated leg with its reference to the day's open.
type Leg struct {
	Result
	VsOpenBps float64
}

// Summarize computes the statistics of legs.
func Summarize(legs []Leg) Summary {
	s := Summary{Legs: len(legs)}
	if len(legs) == 0 {
		return s
	}
	costs := make([]float64, len(legs))
	var maker, qty, full, sum, sumOpen float64
	for i, l := range legs {
		costs[i] = l.CostBps
		maker += l.MakerQty
		qty += l.MakerQty + l.TakerQty
		if l.TakerQty <= 1e-12 {
			full++
		}
		sum += l.CostBps
		sumOpen += l.VsOpenBps
	}
	n := float64(len(legs))
	s.MakerShare, s.FullMakerRate = maker/qty, full/n
	s.MeanBps, s.MeanVsOpenBps = sum/n, sumOpen/n
	var v, vo float64
	for _, l := range legs {
		v += (l.CostBps - s.MeanBps) * (l.CostBps - s.MeanBps)
		vo += (l.VsOpenBps - s.MeanVsOpenBps) * (l.VsOpenBps - s.MeanVsOpenBps)
	}
	if n > 1 {
		s.StdBps, s.StdVsOpenBps = math.Sqrt(v/(n-1)), math.Sqrt(vo/(n-1))
	}
	sort.Float64s(costs)
	s.MedianBps, s.P90Bps = quantile(costs, 0.5), quantile(costs, 0.9)
	return s
}

// quantile interpolates linearly in sorted xs.
func quantile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	pos := q * float64(len(xs)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return xs[lo] + (xs[hi]-xs[lo])*(pos-float64(lo))
}

// Paired compares two schedules leg by leg on the same days and sides
// (base[i] and alt[i] must be the same leg). The difference is alt − base,
// so negative means alt was cheaper. The p-value is one-sided for "alt is
// cheaper", from the normal approximation to the paired t statistic (fine
// for the 60+ legs a forward window gives; reported with N so a reader can
// judge).
type Paired struct {
	N               int     `json:"n"`
	MeanDiffBps     float64 `json:"mean_diff_bps"`
	StdErrBps       float64 `json:"std_err_bps"`
	T               float64 `json:"t"`
	PAltCheaper     float64 `json:"p_alt_cheaper"` // one-sided
	AltCheaperShare float64 `json:"alt_cheaper_share"`
	TieShare        float64 `json:"tie_share"`
	BaseMeanBps     float64 `json:"base_mean_bps"`
	AltMeanBps      float64 `json:"alt_mean_bps"`
	BaseP90Bps      float64 `json:"base_p90_bps"`
	AltP90Bps       float64 `json:"alt_p90_bps"`
}

// Compare pairs base and alt; ok is false when they are not the same length
// or empty.
func Compare(base, alt []Leg) (Paired, bool) {
	if len(base) == 0 || len(base) != len(alt) {
		return Paired{}, false
	}
	n := float64(len(base))
	p := Paired{N: len(base)}
	diffs := make([]float64, len(base))
	bc, ac := make([]float64, len(base)), make([]float64, len(base))
	var sum float64
	for i := range base {
		d := alt[i].CostBps - base[i].CostBps
		diffs[i], bc[i], ac[i] = d, base[i].CostBps, alt[i].CostBps
		sum += d
		p.BaseMeanBps += bc[i] / n
		p.AltMeanBps += ac[i] / n
		switch {
		case math.Abs(d) < 1e-9:
			p.TieShare++
		case d < 0:
			p.AltCheaperShare++
		}
	}
	p.AltCheaperShare /= n
	p.TieShare /= n
	p.MeanDiffBps = sum / n
	if len(base) > 1 {
		var v float64
		for _, d := range diffs {
			v += (d - p.MeanDiffBps) * (d - p.MeanDiffBps)
		}
		p.StdErrBps = math.Sqrt(v/(n-1)) / math.Sqrt(n)
	}
	switch {
	case p.StdErrBps > 0:
		p.T = p.MeanDiffBps / p.StdErrBps
		p.PAltCheaper = 0.5 * math.Erfc(-p.T/math.Sqrt2) // P(Z <= t)
	case p.MeanDiffBps < 0:
		p.PAltCheaper = 0
	default:
		p.PAltCheaper = 1
	}
	sort.Float64s(bc)
	sort.Float64s(ac)
	p.BaseP90Bps, p.AltP90Bps = quantile(bc, 0.9), quantile(ac, 0.9)
	return p, true
}
