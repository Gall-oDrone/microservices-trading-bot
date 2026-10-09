// Package execcost estimates what an order of a given size costs to
// execute, for the capacity and market-impact study (plan §6.4.12).
//
// Two views, because neither is enough alone:
//
//   - Walk: a market order filled against a snapshot of the visible book
//     (Bitso sends the top 20 levels per side). Exact for what is shown, blind
//     to hidden and replenishing liquidity, and undefined past the shown depth.
//   - SqrtImpactBps: the square-root law of market impact used across
//     equities, futures and crypto: impact ≈ Y · σ_daily · sqrt(Q / V_daily),
//     with Y of order 1 (0.5–1 in most studies). It describes a meta-order
//     worked over the day, so it extends past the visible book, but Y is an
//     assumption, so it is reported as a range.
//
// Costs are in bps of the reference (mid) price; positive is a cost.
package execcost

import (
	"math"
	"sort"
)

// Level is one price level of the visible book (base-currency amount).
type Level struct {
	Price  float64 `json:"price"`
	Amount float64 `json:"amount"`
}

// Fill is a market order walked through the book.
type Fill struct {
	Qty      float64 `json:"qty"`
	Filled   float64 `json:"filled"`
	AvgPrice float64 `json:"avg_price"`
	CostBps  float64 `json:"cost_bps"` // vs mid; + is a cost (half-spread included)
	Levels   int     `json:"levels"`   // levels touched
	Complete bool    `json:"complete"` // false: the visible book is too thin for qty
}

// Walk fills qty against levels (best first: asks for a buy, bids for a
// sell) and prices it against mid.
func Walk(levels []Level, qty, mid float64, buy bool) Fill {
	f := Fill{Qty: qty}
	if qty <= 0 || mid <= 0 {
		return f
	}
	var notional float64
	left := qty
	for _, l := range levels {
		if left <= 0 {
			break
		}
		if l.Amount <= 0 || l.Price <= 0 {
			continue
		}
		take := math.Min(left, l.Amount)
		notional += take * l.Price
		f.Filled += take
		left -= take
		f.Levels++
	}
	f.Complete = left <= qty*1e-12
	if f.Filled > 0 {
		f.AvgPrice = notional / f.Filled
		if buy {
			f.CostBps = (f.AvgPrice/mid - 1) * 1e4
		} else {
			f.CostBps = (1 - f.AvgPrice/mid) * 1e4
		}
	}
	return f
}

// Depth is the visible amount on one side.
func Depth(levels []Level) float64 {
	var s float64
	for _, l := range levels {
		s += l.Amount
	}
	return s
}

// MaxQtyWithin is the largest quantity whose walk costs at most budgetBps on
// both sides (buy through asks, sell through bids), within the visible book;
// 0 when even the best level is beyond budget.
func MaxQtyWithin(bids, asks []Level, mid, budgetBps float64) float64 {
	limit := math.Min(Depth(bids), Depth(asks))
	ok := func(q float64) bool {
		b, s := Walk(asks, q, mid, true), Walk(bids, q, mid, false)
		const eps = 1e-9 // bps: a level priced exactly at the budget is within it
		return b.Complete && s.Complete && b.CostBps <= budgetBps+eps && s.CostBps <= budgetBps+eps
	}
	if limit <= 0 || !ok(limit*1e-9) {
		return 0
	}
	if ok(limit) {
		return limit
	}
	lo, hi := limit*1e-9, limit
	for i := 0; i < 60; i++ {
		m := (lo + hi) / 2
		if ok(m) {
			lo = m
		} else {
			hi = m
		}
	}
	return lo
}

// SqrtImpactBps is Y · σ · sqrt(qty / adv) in bps (σ the daily return vol).
func SqrtImpactBps(qty, adv, dailyVol, y float64) float64 {
	if qty <= 0 || adv <= 0 || dailyVol <= 0 {
		return 0
	}
	return y * dailyVol * math.Sqrt(qty/adv) * 1e4
}

// SqrtCapacity is the quantity whose square-root impact equals budgetBps.
func SqrtCapacity(adv, dailyVol, y, budgetBps float64) float64 {
	if adv <= 0 || dailyVol <= 0 || y <= 0 || budgetBps <= 0 {
		return 0
	}
	r := budgetBps / 1e4 / (y * dailyVol)
	return adv * r * r
}

// DailyStats summarises the last n days: mean and median volume (base
// units) and the standard deviation of daily log returns (n returns, so
// n+1 closes are used when available).
func DailyStats(closes, volumes []float64, n int) (meanVol, medianVol, vol float64) {
	if n <= 0 || len(volumes) == 0 {
		return 0, 0, 0
	}
	v := volumes[max(0, len(volumes)-n):]
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	for _, x := range s {
		meanVol += x
	}
	meanVol /= float64(len(s))
	if k := len(s); k%2 == 1 {
		medianVol = s[k/2]
	} else {
		medianVol = (s[k/2-1] + s[k/2]) / 2
	}
	c := closes[max(0, len(closes)-n-1):]
	var r []float64
	for i := 1; i < len(c); i++ {
		if c[i] > 0 && c[i-1] > 0 {
			r = append(r, math.Log(c[i]/c[i-1]))
		}
	}
	if len(r) > 1 {
		var m, ss float64
		for _, x := range r {
			m += x
		}
		m /= float64(len(r))
		for _, x := range r {
			ss += (x - m) * (x - m)
		}
		vol = math.Sqrt(ss / float64(len(r)-1))
	}
	return meanVol, medianVol, vol
}
