package dailyrule

import (
	"math"
	"testing"
	"time"
)

func simBars(closes ...float64) []Bar {
	d0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]Bar, len(closes))
	for i, c := range closes {
		o := c
		if i > 0 {
			o = closes[i-1] // open at the previous close: easy to reason about
		}
		out[i] = Bar{Date: d0.AddDate(0, 0, i), Open: o, High: math.Max(o, c), Low: math.Min(o, c), Close: c}
	}
	return out
}

// The decision at day t's close fills at day t+1's open; an open position
// is closed at the window's final close with costs.
func TestSimulateFillsNextOpenAndClosesOut(t *testing.T) {
	bars := simBars(100, 110, 120, 90, 100)
	want := []bool{true, true, false, true, true}
	res, tr := SimulateTrace(bars, want, 0, 4, Costs{Buy: 0.01, Sell: 0.02})

	if len(tr.Fills) != 4 {
		t.Fatalf("fills = %+v", tr.Fills)
	}
	// Buy at bars[1].Open (=100), sell at bars[3].Open (=120), buy at
	// bars[4].Open (=90), close out at bars[4].Close (=100).
	wantFills := []struct {
		idx   int
		buy   bool
		price float64
		final bool
	}{{1, true, 100, false}, {3, false, 120, false}, {4, true, 90, false}, {4, false, 100, true}}
	for i, f := range wantFills {
		g := tr.Fills[i]
		if g.Index != f.idx || g.Buy != f.buy || g.Price != f.price || g.Final != f.final {
			t.Fatalf("fill %d = %+v, want %+v", i, g, f)
		}
	}
	if res.RoundTrips != 2 {
		t.Fatalf("round trips = %d", res.RoundTrips)
	}
	// Hand-computed: 1 → 0.99/100 units → ×120 ×0.98 → ×0.99/90 → ×100 ×0.98.
	cash := 0.99 / 100 * 120 * 0.98
	cash = cash * 0.99 / 90 * 100 * 0.98
	if math.Abs(res.ReturnPct-(cash-1)*100) > 1e-12 {
		t.Fatalf("return = %v, want %v", res.ReturnPct, (cash-1)*100)
	}
	if last := tr.Fills[len(tr.Fills)-1]; math.Abs(last.Cash-cash) > 1e-12 {
		t.Fatalf("final cash = %v, want %v", last.Cash, cash)
	}
}

// With lo > 0 the first fill uses the decision from the prior day's close.
func TestSimulateWindowUsesPriorDayDecision(t *testing.T) {
	bars := simBars(100, 100, 100, 100)
	want := []bool{false, true, true, true}
	_, tr := SimulateTrace(bars, want, 2, 3, Costs{})
	if len(tr.Fills) == 0 || tr.Fills[0].Index != 2 || !tr.Fills[0].Buy {
		t.Fatalf("fills = %+v", tr.Fills)
	}
	if len(tr.Equity) != 2 || tr.Equity[0].Index != 2 {
		t.Fatalf("equity = %+v", tr.Equity)
	}
}

// The trace only records: Simulate and SimulateTrace agree bit for bit, the
// curve's largest drawdown is MaxDDPct, and fees sum to CostPct.
func TestSimulateTraceConsistent(t *testing.T) {
	bars := simBars(100, 104, 98, 120, 111, 130, 90, 95, 140, 135, 150, 120)
	want := []bool{true, false, true, true, false, true, true, false, true, true, true, true}
	c := Costs{Buy: 0.0088, Sell: 0.0088}
	plain := Simulate(bars, want, 0, len(bars)-1, c)
	res, tr := SimulateTrace(bars, want, 0, len(bars)-1, c)
	if plain != res {
		t.Fatalf("Simulate %+v != SimulateTrace %+v", plain, res)
	}
	maxDD, fees := 0.0, 0.0
	for _, p := range tr.Equity {
		maxDD = math.Max(maxDD, p.Drawdown)
	}
	for _, f := range tr.Fills {
		fees += f.Fee
	}
	if maxDD*100 != res.MaxDDPct {
		t.Fatalf("curve max drawdown %v != MaxDDPct %v", maxDD*100, res.MaxDDPct)
	}
	if math.Abs(fees*100-res.CostPct) > 1e-12 {
		t.Fatalf("fees %v != CostPct %v", fees*100, res.CostPct)
	}
	if len(tr.Equity) != len(bars) {
		t.Fatalf("equity points = %d", len(tr.Equity))
	}
}
