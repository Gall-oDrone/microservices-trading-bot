package cfdrisk

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/cfdsim"
	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/shared/pkg/varmodel"
)

// Shocks, the reverse stress and the close-out on a two-book long.
func TestStressShocksAndReverse(t *testing.T) {
	exp := map[string]float64{"nsdq100": 1100, "spx500": 1100}
	s := StressTest(exp, nil, 100000)
	if s.Net != 2200 || s.Gross != 2200 || len(s.Shocks) != 4 {
		t.Fatalf("%+v", s)
	}
	for i, m := range Shocks {
		if !near(s.Shocks[i].Loss, -2200*m, 1e-9) || s.Shocks[i].CloseOut {
			t.Errorf("shock %v: %+v", m, s.Shocks[i])
		}
	}
	// Capital above the net exposure: no uniform fall exhausts it.
	if s.ReverseStressOK || !near(s.ReverseStressMove, -100000.0/2200, 1e-9) {
		t.Fatalf("reverse %v %v", s.ReverseStressMove, s.ReverseStressOK)
	}
	if small := StressTest(exp, nil, 1100); !small.ReverseStressOK || !near(small.ReverseStressMove, -0.5, 1e-9) {
		t.Fatalf("reverse at half the exposure: %+v", small)
	}
	if !near(s.CloseOutLoss, 1100, 1e-9) {
		t.Fatalf("close-out loss %v", s.CloseOutLoss)
	}
	// Without paths every episode is incomplete and there is no worst.
	for _, e := range s.Episodes {
		if e.OK || e.Loss != 0 {
			t.Errorf("%s without paths: %+v", e.ID, e)
		}
	}
	if s.Worst != "" {
		t.Fatal("worst episode without paths")
	}
}

// Episodes on the embedded index paths: today's two longs through the
// crises, the worst being Lehman, with the loss at the worst common day
// and no close-out (no index trough reached -50 %).
func TestStressEpisodesOnKnownPaths(t *testing.T) {
	exp := map[string]float64{"nsdq100": 1100, "spx500": 1100}
	paths := map[string]map[string][]varmodel.PathPoint{
		"nsdq100": KnownIndexEpisodePaths["^NDX"],
		"spx500":  KnownIndexEpisodePaths["^GSPC"],
	}
	s := StressTest(exp, paths, 100000)
	if s.Worst != "2008-09-lehman" {
		t.Fatalf("worst %s (%v)", s.Worst, s.WorstLoss)
	}
	for _, e := range s.Episodes {
		if !e.OK || e.CloseOut || len(e.Troughs) != 2 {
			t.Errorf("%s: %+v", e.ID, e)
		}
		// The loss is at least the sum at the common trough day and at
		// most the sum of the two troughs.
		if e.Loss > -1100*(e.Troughs["nsdq100"]+e.Troughs["spx500"])+1e-9 {
			t.Errorf("%s: loss %v above the sum of troughs", e.ID, e.Loss)
		}
	}
	if !near(s.WorstLoss, 1100*(0.4060+0.4005), 1.5) { // both troughs on 2008-11-20
		t.Fatalf("lehman loss %v", s.WorstLoss)
	}
	// A book whose index path is missing leaves the episode incomplete.
	s2 := StressTest(exp, map[string]map[string][]varmodel.PathPoint{"nsdq100": KnownIndexEpisodePaths["^NDX"]}, 100000)
	for _, e := range s2.Episodes {
		if e.OK {
			t.Errorf("%s complete without spx500", e.ID)
		}
	}
	// A -50 % trough on a long flags the close-out.
	deep := map[string]map[string][]varmodel.PathPoint{
		"x": {"2008-09-lehman": {{Date: ymd("2008-10-10"), Cum: -0.55}}},
	}
	s3 := StressTest(map[string]float64{"x": 1000}, deep, 1e6)
	if !s3.Episodes[0].CloseOut || !near(s3.Episodes[0].Loss, 550, 1e-9) {
		t.Fatalf("%+v", s3.Episodes[0])
	}
}

// synthTradingBars makes NYSE-trading-day bars with lognormal moves.
func synthTradingBars(n int, seed int64, start time.Time) []dailyrule.Bar {
	r := rand.New(rand.NewSource(seed))
	d := start
	if !mktcal.IsTradingDay(d) {
		d = mktcal.NextTradingDay(d)
	}
	p := 100.0
	out := make([]dailyrule.Bar, 0, n)
	for len(out) < n {
		o := p * math.Exp(r.NormFloat64()*0.003)
		c := o * math.Exp(r.NormFloat64()*0.01)
		out = append(out, dailyrule.Bar{Date: d, Open: o, High: math.Max(o, c), Low: math.Min(o, c), Close: c})
		p = c
		d = mktcal.NextTradingDay(d)
	}
	return out
}

var frozenNSDQ = cfdsim.Costs{SpreadPerLeg: 15.5 / 2 / 1e4, NightlyRate: cfdsim.FixedNightly(2.3 / 1e4)}

// Determinism (same seed, same summary; another seed, another), the
// trading-day calendar of the paths, and the zero-cost sanity: with no
// costs the CFD-priced trend path equals the frozen dailyrule simulator
// path for path, and the CFD hold equals the ETF hold.
func TestMonteCarloDeterminismAndZeroCosts(t *testing.T) {
	hist := synthTradingBars(700, 3, ymd("2023-01-03"))
	cfg := MCConfig{Paths: 200, Horizon: 252, MeanBlock: 20, Seed: 1, SMA: 50, CFD: frozenNSDQ,
		ETF: cfdsim.Costs{SpreadPerLeg: 0.13 / 2 / 1e4, FeePerLeg: 1.5 / 1100}}
	a, err := MonteCarlo(hist, 1, cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := MonteCarlo(hist, 1, cfg)
	if a.TrendReturn != b.TrendReturn || a.PBeatsCFDHold != b.PBeatsCFDHold || a.CFDHoldReturn != b.CFDHoldReturn {
		t.Fatal("same seed, different summary")
	}
	cfg2 := cfg
	cfg2.Seed = 2
	if c, _ := MonteCarlo(hist, 1, cfg2); c.TrendReturn == a.TrendReturn {
		t.Fatal("another seed, same summary")
	}
	if a.SampleDays != 699 || a.StartClose != hist[699].Close || a.SampleTo != hist[699].Date.Format("2006-01-02") {
		t.Fatalf("sample %+v", a)
	}
	// 252 trading days after the last bar.
	want := hist[699].Date
	for i := 0; i < 252; i++ {
		want = mktcal.NextTradingDay(want)
	}
	if a.HorizonEnd != want.Format("2006-01-02") {
		t.Fatalf("horizon end %s, want %s", a.HorizonEnd, want.Format("2006-01-02"))
	}
	// Financing is paid: the hold pays about 2.3 bps x 365 nights.
	if a.HoldFinance.P50 < 0.07 || a.HoldFinance.P50 > 0.10 || a.TrendFinance.P50 >= a.HoldFinance.P50 {
		t.Fatalf("financing trend %v hold %v", a.TrendFinance, a.HoldFinance)
	}
	if a.PBeatsETFHold.P > a.PBeatsCFDHold.P {
		t.Fatalf("beating the ETF (no financing) cannot be likelier than beating the CFD: %v vs %v", a.PBeatsETFHold.P, a.PBeatsCFDHold.P)
	}

	// Zero costs: every path's trend return is the dailyrule return.
	zero := MCConfig{Paths: 50, Horizon: 252, MeanBlock: 20, Seed: 1, SMA: 50}
	var paths int
	zero.onPath = func(bars []dailyrule.Bar, want []bool, lo, hi int) {
		paths++
		ref := dailyrule.Simulate(bars, want, lo, hi, dailyrule.Costs{})
		got := cfdsim.Simulate(bars, want, lo, hi, cfdsim.Costs{})
		if got.ReturnPct != ref.ReturnPct || got.RoundTrips != ref.RoundTrips || got.MaxDDPct != ref.MaxDDPct {
			t.Fatalf("path %d: cfdsim %+v vs dailyrule %+v", paths, got, ref)
		}
		for i := lo; i <= hi; i++ {
			if !mktcal.IsTradingDay(bars[i].Date) || (i > lo && !bars[i].Date.Equal(mktcal.NextTradingDay(bars[i-1].Date))) {
				t.Fatalf("path %d bar %d dated %s: not the next trading day", paths, i, bars[i].Date.Format("2006-01-02"))
			}
		}
	}
	z, err := MonteCarlo(hist, 1, zero)
	if err != nil {
		t.Fatal(err)
	}
	if paths != 50 || z.CFDHoldReturn != z.ETFHoldReturn || z.TrendFinance.Mean != 0 || z.PTrendCloseOut.P != 0 {
		t.Fatalf("zero costs: %d paths, hold %v vs etf %v", paths, z.CFDHoldReturn, z.ETFHoldReturn)
	}
	// Bad configurations.
	if _, err := MonteCarlo(hist, 1, MCConfig{Paths: 10, Horizon: 10, MeanBlock: 20, SMA: 50}); err == nil {
		t.Fatal("horizon 10 accepted")
	}
	if _, err := MonteCarlo(hist[:40], 1, zero); err == nil {
		t.Fatal("40 bars accepted")
	}
}
