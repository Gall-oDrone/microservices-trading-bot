package execcost

import (
	"math"
	"testing"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// A book around mid 100: asks 100.5 x1, 101 x2, 102 x5; bids mirror.
var (
	asks = []Level{{100.5, 1}, {101, 2}, {102, 5}}
	bids = []Level{{99.5, 1}, {99, 2}, {98, 5}}
)

func TestWalk(t *testing.T) {
	f := Walk(asks, 0.5, 100, true)
	if !f.Complete || f.Levels != 1 || !near(f.AvgPrice, 100.5, 1e-12) || !near(f.CostBps, 50, 1e-9) {
		t.Fatalf("%+v", f)
	}
	// 3 BTC: 1 @ 100.5 + 2 @ 101 = 302.5 -> avg 100.8333, 83.3 bps.
	f = Walk(asks, 3, 100, true)
	if !f.Complete || f.Levels != 2 || !near(f.AvgPrice, 302.5/3, 1e-9) || !near(f.CostBps, (302.5/3/100-1)*1e4, 1e-9) {
		t.Fatalf("%+v", f)
	}
	s := Walk(bids, 3, 100, false)
	if !near(s.CostBps, (1-(99.5+2*99)/3/100)*1e4, 1e-9) {
		t.Fatalf("sell %+v", s)
	}
	// Past the visible depth: incomplete, priced on what was there.
	if f := Walk(asks, 10, 100, true); f.Complete || f.Filled != 8 {
		t.Fatalf("thin %+v", f)
	}
	if f := Walk(asks, 0, 100, true); f.Filled != 0 || f.CostBps != 0 {
		t.Fatalf("zero %+v", f)
	}
}

func TestMaxQtyWithin(t *testing.T) {
	// 50 bps: only the first level (1 BTC) on each side.
	if q := MaxQtyWithin(bids, asks, 100, 50); !near(q, 1, 1e-6) {
		t.Fatalf("50 bps -> %v", q)
	}
	// 75 bps: 1 @ 50 + x @ 100 averages 75 at x = 1.
	if q := MaxQtyWithin(bids, asks, 100, 75); !near(q, 2, 1e-6) {
		t.Fatalf("75 bps -> %v", q)
	}
	if q := MaxQtyWithin(bids, asks, 100, 10); q != 0 {
		t.Fatalf("inside the half-spread -> %v", q)
	}
	if q := MaxQtyWithin(bids, asks, 100, 1e6); q != 8 {
		t.Fatalf("whole book -> %v", q)
	}
}

func TestSqrtLaw(t *testing.T) {
	// 1 % of a day's volume at 2 % daily vol, Y = 1: 0.02 * 0.1 = 20 bps.
	if b := SqrtImpactBps(1, 100, 0.02, 1); !near(b, 20, 1e-9) {
		t.Fatalf("%v", b)
	}
	// Quadrupling size doubles impact.
	if b := SqrtImpactBps(4, 100, 0.02, 1); !near(b, 40, 1e-9) {
		t.Fatalf("%v", b)
	}
	// Capacity inverts it.
	if q := SqrtCapacity(100, 0.02, 1, 20); !near(q, 1, 1e-9) {
		t.Fatalf("%v", q)
	}
	if SqrtImpactBps(1, 0, 0.02, 1) != 0 || SqrtCapacity(0, 0.02, 1, 10) != 0 {
		t.Fatal("degenerate inputs")
	}
}

func TestDailyStats(t *testing.T) {
	closes := []float64{100, 110, 99, 108.9, 98.01}
	vols := []float64{5, 1, 3, 2, 4}
	m, med, v := DailyStats(closes, vols, 4)
	if !near(m, 2.5, 1e-12) || !near(med, 2.5, 1e-12) {
		t.Fatalf("volume %v %v", m, med)
	}
	// Returns alternate ln(1.1) and ln(0.9): sample sd of 4 such values.
	a, b := math.Log(1.1), math.Log(0.9)
	mu := (a + b) / 2
	want := math.Sqrt((2*(a-mu)*(a-mu) + 2*(b-mu)*(b-mu)) / 3)
	if !near(v, want, 1e-12) {
		t.Fatalf("vol %v want %v", v, want)
	}
	if m, _, v := DailyStats(nil, nil, 30); m != 0 || v != 0 {
		t.Fatal("empty")
	}
}
