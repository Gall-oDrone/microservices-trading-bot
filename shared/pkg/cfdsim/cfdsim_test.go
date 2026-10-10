package cfdsim

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/dailyrule"
)

func synthBars(n int, seed int64) []dailyrule.Bar {
	r := rand.New(rand.NewSource(seed))
	d := time.Date(2020, 1, 6, 0, 0, 0, 0, time.UTC) // Monday
	p := 100.0
	var out []dailyrule.Bar
	for len(out) < n {
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday {
			o := p * (1 + r.NormFloat64()*0.003)
			c := o * (1 + r.NormFloat64()*0.01)
			out = append(out, dailyrule.Bar{Date: d, Open: o, High: math.Max(o, c) * 1.002, Low: math.Min(o, c) * 0.998, Close: c})
			p = c
		}
		d = d.AddDate(0, 0, 1)
	}
	return out
}

// With no financing and no fixed fee, cfdsim must reproduce the frozen
// dailyrule simulator exactly: same return, trips, exposure and drawdown.
func TestMatchesDailyrule(t *testing.T) {
	bars := synthBars(800, 7)
	want := dailyrule.Trend(bars, 50)
	for _, leg := range []float64{0, 0.0004, 0.0088} {
		for _, w := range [][2]int{{0, 799}, {60, 799}, {300, 520}} {
			got := Simulate(bars, want, w[0], w[1], Costs{SpreadPerLeg: leg})
			ref := dailyrule.Simulate(bars, want, w[0], w[1], dailyrule.Costs{Buy: leg, Sell: leg})
			if got.ReturnPct != ref.ReturnPct || got.RoundTrips != ref.RoundTrips || got.ExposurePct != ref.ExposurePct || got.MaxDDPct != ref.MaxDDPct || got.TradeCostPct != ref.CostPct {
				t.Fatalf("leg %v window %v: cfdsim %+v vs dailyrule %+v", leg, w, got, ref)
			}
			if got.FinancePct != 0 || got.NightsHeld != 0 {
				t.Fatalf("no financing expected: %+v", got)
			}
		}
	}
}

// Buy-and-hold over Mon..next Mon pays 7 calendar nights (Fri->Mon = 3).
func TestFinancingCountsCalendarNights(t *testing.T) {
	var bars []dailyrule.Bar
	d := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC) // Monday
	for len(bars) < 6 {
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday {
			bars = append(bars, dailyrule.Bar{Date: d, Open: 100, High: 100, Low: 100, Close: 100})
		}
		d = d.AddDate(0, 0, 1)
	}
	want := []bool{true, true, true, true, true, true}
	// Enter at bars[1] (Tue) open, hold to bars[5] (next Mon) close: nights
	// Tue->Wed, Wed->Thu, Thu->Fri, Fri->Mon(3) = 6.
	r := Simulate(bars, want, 1, 5, Costs{NightlyRate: FixedNightly(0.0001)})
	if r.NightsHeld != 6 {
		t.Fatalf("nights %d, want 6", r.NightsHeld)
	}
	if math.Abs(r.FinancePct-0.06) > 1e-9 || math.Abs(r.ReturnPct+0.06) > 1e-9 {
		t.Fatalf("finance %.6f%% return %.6f%%, want 0.06%% and -0.06%%", r.FinancePct, r.ReturnPct)
	}
}

func TestFixedFeeAndFlatPeriodsCostNothing(t *testing.T) {
	bars := synthBars(300, 3)
	flat := make([]bool, len(bars))
	r := Simulate(bars, flat, 60, 299, Costs{SpreadPerLeg: 0.001, FeePerLeg: 0.0014, NightlyRate: FixedNightly(0.0002)})
	if r.ReturnPct != 0 || r.FinancePct != 0 || r.TradeCostPct != 0 || r.RoundTrips != 0 {
		t.Fatalf("flat strategy %+v", r)
	}
	hold := make([]bool, len(bars))
	for i := range hold {
		hold[i] = true
	}
	r = Simulate(bars, hold, 60, 299, Costs{FeePerLeg: 0.0014})
	if r.RoundTrips != 1 || r.TradeCostPct <= 0.13 || r.TradeCostPct > 0.3 {
		t.Fatalf("hold with fixed fee: %+v (two legs of ~0.14%%)", r)
	}
}

func TestRateLinked(t *testing.T) {
	ref := func(d time.Time) (float64, bool) {
		if d.Year() < 2000 {
			return 0.06, true
		}
		return 0.04, true
	}
	f := RateLinked(ref, 0.044)
	if got := f(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) * 365; math.Abs(got-0.084) > 1e-12 {
		t.Fatalf("annualized %v, want 0.084", got)
	}
	if got := f(time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC)) * 365; math.Abs(got-0.104) > 1e-12 {
		t.Fatalf("annualized %v, want 0.104", got)
	}
	neg := RateLinked(func(time.Time) (float64, bool) { return -0.01, true }, 0)
	if neg(time.Now()) != 0 {
		t.Fatal("negative financing must clamp to 0")
	}
}

func TestSharpeAndCAGR(t *testing.T) {
	bars := synthBars(253, 11)
	hold := make([]bool, len(bars))
	for i := range hold {
		hold[i] = true
	}
	r := Simulate(bars, hold, 0, 252, Costs{})
	// one year of bars: CAGR equals the window return (entry at bars[1] open).
	if math.Abs(r.CAGRPct-r.ReturnPct) > 1e-9 || r.SharpeAnn == 0 {
		t.Fatalf("%+v", r)
	}
}
