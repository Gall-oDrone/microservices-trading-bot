package main

import (
	"math"
	"testing"
	"time"
)

func mkDays(closes []float64, vols []float64) []day {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) // a Monday
	d := make([]day, len(closes))
	for i, c := range closes {
		o := c
		if i > 0 {
			o = closes[i-1] // open = previous close
		}
		v := 1.0
		if vols != nil {
			v = vols[i]
		}
		d[i] = day{date: t0.AddDate(0, 0, i), open: o, high: c, low: c, close: c, volume: v}
	}
	return d
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Holding all the time with zero costs returns last close / first fill open.
func TestBuyAndHoldFrictionless(t *testing.T) {
	d := mkDays([]float64{100, 110, 121, 99, 130}, nil)
	w := []float64{1, 1, 1, 1, 1}
	r := simulate(d, w, 1, 4, 0)
	if want := 130.0/100.0 - 1; !near(r.ret, want) {
		t.Fatalf("ret %v want %v", r.ret, want)
	}
	if r.trades != 1 {
		t.Fatalf("trades %d", r.trades)
	}
}

// One round trip pays roughly two legs of cost.
func TestCostsPerLeg(t *testing.T) {
	d := mkDays([]float64{100, 100, 100, 100}, nil)
	w := []float64{1, 1, 0, 0} // buy at d[1].open, sell at d[3].open
	r := simulate(d, w, 1, 3, 0.01)
	if r.trades != 2 || r.ret > -0.0195 || r.ret < -0.0205 {
		t.Fatalf("trades %d ret %v, want 2 trades and about -2%%", r.trades, r.ret)
	}
}

// A partial weight drifts with price and does not rebalance daily.
func TestNoDailyRebalanceAtConstantWeight(t *testing.T) {
	d := mkDays([]float64{100, 100, 120, 90, 150}, nil)
	w := []float64{0.5, 0.5, 0.5, 0.5, 0.5}
	if r := simulate(d, w, 1, 4, 0.001); r.trades != 1 {
		t.Fatalf("trades %d, want 1 (entry only)", r.trades)
	}
}

func TestVolumeRatioExcludesToday(t *testing.T) {
	vols := []float64{1, 1, 1, 4}
	r := volumeRatio(mkDays([]float64{1, 1, 1, 1}, vols), 3)
	if !math.IsNaN(r[2]) || !near(r[3], 4) {
		t.Fatalf("got %v", r)
	}
}

func TestSpikeRuleHoldsHDays(t *testing.T) {
	closes := []float64{100, 100, 100, 100, 110, 110, 110, 110, 110, 110}
	vols := []float64{1, 1, 1, 1, 9, 1, 1, 1, 1, 1}
	d := mkDays(closes, vols)
	vr := volumeRatio(d, 3)
	w := spikeRule(d, vr, 2, 3, true) // event at day 4 (up, 9x volume)
	want := []float64{0, 0, 0, 0, 1, 1, 1, 0, 0, 0}
	for i := range want {
		if w[i] != want[i] {
			t.Fatalf("w=%v want %v", w, want)
		}
	}
	if down := spikeRule(d, vr, 2, 3, false); down[4] != 0 {
		t.Fatalf("down rule fired on an up day: %v", down)
	}
}

// weekly() only changes size on Sunday closes (or entries).
func TestWeeklyChangesOnlyOnSundays(t *testing.T) {
	d := mkDays(make([]float64, 15), nil) // 2024-01-01 is a Monday; day 6 and 13 are Sundays
	on := make([]bool, 15)
	z := make([]float64, 15)
	for i := range on {
		on[i] = true
		z[i] = 0.2 + 0.05*float64(i)
	}
	w := weekly(d, on, z, 0.01)
	for i := 1; i < 15; i++ {
		if w[i] != w[i-1] && d[i].date.Weekday() != time.Sunday {
			t.Fatalf("size changed on %s (day %d): %v", d[i].date.Weekday(), i, w)
		}
	}
	if w[6] != z[6] || w[13] != z[13] {
		t.Fatalf("Sunday sizes not taken: %v", w)
	}
}
