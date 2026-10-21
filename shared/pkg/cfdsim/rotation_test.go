package cfdsim

import (
	"math"
	"testing"

	"bitso-trading-platform/shared/pkg/dailyrule"
)

// With one asset, SimulateRotation must reproduce Simulate exactly, with
// and without financing and fixed fees.
func TestRotationMatchesSimulate(t *testing.T) {
	bars := synthBars(800, 11)
	want := dailyrule.Trend(bars, 50)
	iw := make([]int, len(want))
	for i, w := range want {
		iw[i] = -1
		if w {
			iw[i] = 0
		}
	}
	costSets := []Costs{
		{},
		{SpreadPerLeg: 0.0008},
		{SpreadPerLeg: 0.0008, FeePerLeg: 0.00136},
		{SpreadPerLeg: 0.0008, NightlyRate: FixedNightly(0.00023)},
	}
	for ci, c := range costSets {
		for _, w := range [][2]int{{0, 799}, {60, 799}, {300, 520}} {
			ref := Simulate(bars, want, w[0], w[1], c)
			got := SimulateRotation([][]dailyrule.Bar{bars}, iw, w[0], w[1], []Costs{c})
			if got.Result != ref {
				t.Fatalf("costs %d window %v:\n rotation %+v\n simulate %+v", ci, w, got.Result, ref)
			}
			if got.Switches != 0 || len(got.ExposureByAsset) != 1 || got.ExposureByAsset[0] != ref.ExposurePct {
				t.Fatalf("one asset: switches %d exposure %v vs %v", got.Switches, got.ExposureByAsset, ref.ExposurePct)
			}
		}
	}
}

// Switching asset->asset at one open pays both legs and counts one switch
// and one round trip; the equity path equals holding a then b.
func TestRotationSwitchCosts(t *testing.T) {
	flat := func(p float64, n int) []dailyrule.Bar {
		out := make([]dailyrule.Bar, n)
		for i := range out {
			out[i] = dailyrule.Bar{Date: synthBars(n, 1)[i].Date, Open: p, High: p, Low: p, Close: p}
		}
		return out
	}
	a, b := flat(100, 10), flat(50, 10)
	want := []int{0, 0, 0, 0, 1, 1, 1, 1, 1, 1} // decide at bar 4's close -> switch at bar 5's open
	leg := 0.001
	r := SimulateRotation([][]dailyrule.Bar{a, b}, want, 1, 9, []Costs{{SpreadPerLeg: leg}, {SpreadPerLeg: leg}})
	if r.Switches != 1 || r.RoundTrips != 2 {
		t.Fatalf("switches %d trips %d", r.Switches, r.RoundTrips)
	}
	// Four legs on flat prices: (1-leg)^4 - 1.
	wantRet := (math.Pow(1-leg, 4) - 1) * 100
	if math.Abs(r.ReturnPct-wantRet) > 1e-9 {
		t.Fatalf("return %.6f want %.6f", r.ReturnPct, wantRet)
	}
	if r.ExposureByAsset[0] != 100*4/9.0 || r.ExposureByAsset[1] != 100*5/9.0 {
		t.Fatalf("exposure %v", r.ExposureByAsset)
	}
}
