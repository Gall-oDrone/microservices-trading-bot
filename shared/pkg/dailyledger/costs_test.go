package dailyledger

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPreregCostsAreTheRegisteredNumbers(t *testing.T) {
	// The pre-registrations' §3 tables (maker/taker + 10 bps slippage).
	want := map[string][2]float64{"btc_mxn": {70, 88}, "btc_usd": {40, 46}}
	if len(PreregCosts) != len(want) {
		t.Fatalf("PreregCosts has %d books, want %d", len(PreregCosts), len(want))
	}
	for book, w := range want {
		pc := PreregCosts[book]
		if pc.PrimaryLegBps != w[0] || pc.SecondaryLegBps != w[1] || pc.Prereg == "" {
			t.Errorf("%s = %+v, want primary %v secondary %v", book, pc, w[0], w[1])
		}
	}
}

func TestBudgetForWeightsByNotional(t *testing.T) {
	// The first real btc_mxn stage leg: 78 bps fee + 40 bps slippage on a
	// small notional; then two cheap maker legs on larger notionals.
	legs := []LegCost{
		{Notional: 1000, FeeQuote: 7.8, SlippageBps: 40, SlippageKnown: true}, // 118 bps
		{Notional: 3000, FeeQuote: 18, SlippageBps: 5, SlippageKnown: true},   // 65 bps
		{Notional: 4000, FeeQuote: 24, SlippageBps: -10, SlippageKnown: true}, // 50 bps
	}
	b := BudgetFor("btc_mxn", legs)
	cost := 7.8 + 4 + 18 + 1.5 + 24 - 4 // 51.3
	if b.Legs != 3 || !approx(b.Notional, 8000) || !approx(b.CostQuote, cost) {
		t.Fatalf("got %+v", b)
	}
	if !approx(b.WeightedBps, cost/8000*1e4) { // 64.125, below the simple mean 77.7
		t.Fatalf("weighted %v", b.WeightedBps)
	}
	if !approx(b.BudgetQuote, 56) || !approx(b.ExcessQuote, cost-56) || !approx(b.BudgetUsed, cost/56) {
		t.Fatalf("budget %+v", b)
	}
	if b.OverPessimistic {
		t.Fatal("64 bps is not above the 88 bps pessimistic scenario")
	}
}

func TestBudgetForOverPessimistic(t *testing.T) {
	b := BudgetFor("btc_mxn", []LegCost{{Notional: 1000, FeeQuote: 7.8, SlippageBps: 40, SlippageKnown: true}})
	if !approx(b.WeightedBps, 118) || !b.OverPessimistic || !approx(b.ExcessQuote, 11.8-7) {
		t.Fatalf("got %+v", b)
	}
}

func TestBudgetForMissingRefIsFeesOnly(t *testing.T) {
	b := BudgetFor("btc_usd", []LegCost{
		{Notional: 500, FeeQuote: 1.5},
		{Notional: 500, FeeQuote: 1.5, SlippageBps: 20, SlippageKnown: true},
	})
	if b.LegsWithoutRef != 1 || !approx(b.CostQuote, 4) || !approx(b.WeightedBps, 40) || b.OverPessimistic {
		t.Fatalf("got %+v", b)
	}
}

func TestBudgetForUnregisteredBookAndNoLegs(t *testing.T) {
	b := BudgetFor("eth_mxn", []LegCost{{Notional: 100, FeeQuote: 5}})
	if b.Prereg != "" || b.BudgetQuote != 0 || b.BudgetUsed != 0 || b.OverPessimistic || !approx(b.WeightedBps, 500) {
		t.Fatalf("got %+v", b)
	}
	z := BudgetFor("btc_mxn", nil)
	if z.Legs != 0 || z.WeightedBps != 0 || z.BudgetUsed != 0 || z.OverPessimistic || z.PrimaryLegBps != 70 {
		t.Fatalf("got %+v", z)
	}
	// Zero-notional legs (nothing filled) are counted but carry no cost.
	n := BudgetFor("btc_mxn", []LegCost{{Notional: 0, FeeQuote: 0}})
	if n.Legs != 1 || n.Notional != 0 || n.OverPessimistic {
		t.Fatalf("got %+v", n)
	}
}
