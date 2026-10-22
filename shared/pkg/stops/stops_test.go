package stops

import (
	"math"
	"strings"
	"testing"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// flatBars builds n bars with a constant true range of rng around a flat
// close so ATR == rng exactly.
func flatBars(n int, close, rng float64) []Bar {
	out := make([]Bar, n)
	for i := range out {
		out[i] = Bar{High: close + rng/2, Low: close - rng/2, Close: close}
	}
	return out
}

func TestATRWilder(t *testing.T) {
	// Hand-computed: 15 bars, period 14. True ranges all 10 except the last (30).
	bars := flatBars(15, 100, 10)
	bars = append(bars, Bar{High: 120, Low: 90, Close: 100}) // TR = 30
	s := ATRSeries(bars, 14)
	if len(s) != 2 {
		t.Fatalf("series len %d want 2", len(s))
	}
	if !near(s[0], 10, 1e-9) {
		t.Fatalf("first ATR %v want 10", s[0])
	}
	want := (10*13 + 30) / 14.0
	if !near(s[1], want, 1e-9) {
		t.Fatalf("smoothed ATR %v want %v", s[1], want)
	}
	if a, ok := ATR(bars, 14); !ok || !near(a, want, 1e-9) {
		t.Fatalf("ATR %v %v", a, ok)
	}
	// Gap: the true range uses the previous close.
	gap := []Bar{{High: 101, Low: 99, Close: 100}, {High: 111, Low: 110, Close: 110}}
	if a, ok := ATR(gap, 1); !ok || !near(a, 11, 1e-9) {
		t.Fatalf("gap ATR %v %v want 11", a, ok)
	}
}

func TestATRNeedsBars(t *testing.T) {
	if _, ok := ATR(flatBars(14, 100, 1), 14); ok {
		t.Fatal("14 bars must not be enough for ATR(14)")
	}
	if _, ok := ATR(nil, 14); ok {
		t.Fatal("nil bars")
	}
	bad := flatBars(20, 100, 1)
	bad[5].Low = 0
	if _, ok := ATR(bad, 14); ok {
		t.Fatal("a malformed bar must fail closed")
	}
	if _, ok := ATR(flatBars(20, 100, 1), 0); ok {
		t.Fatal("period 0")
	}
}

func TestMeasureRegime(t *testing.T) {
	// 300 calm bars (range 1) then 20 wild bars (range 4): today's ATR% is
	// well above the median -> extreme.
	bars := append(flatBars(300, 100, 1), flatBars(20, 100, 4)...)
	v, err := Measure(bars, 14, 252)
	if err != nil {
		t.Fatal(err)
	}
	if v.Period != 14 || v.Bars != 320 || v.Close != 100 || !near(v.ATRPct, v.ATR/100, 1e-12) {
		t.Fatalf("%+v", v)
	}
	if v.Regime != RegimeExtreme || v.Ratio < 1.75 || v.Lookback != 252 {
		t.Fatalf("regime %s ratio %.2f lookback %d", v.Regime, v.Ratio, v.Lookback)
	}
	// Uniform history -> normal (ratio 1).
	v, err = Measure(flatBars(60, 100, 2), 14, 252)
	if err != nil || v.Regime != RegimeNormal || !near(v.Ratio, 1, 1e-9) {
		t.Fatalf("uniform: %+v %v", v, err)
	}
	// Enough for ATR but not for a regime.
	v, err = Measure(flatBars(20, 100, 2), 14, 252)
	if err != nil || v.Regime != RegimeUnknown || v.Ratio != 0 {
		t.Fatalf("short: %+v %v", v, err)
	}
	if _, err := Measure(flatBars(10, 100, 2), 14, 252); err == nil {
		t.Fatal("too few bars must error")
	}
	// Defaults.
	if v, err := Measure(flatBars(20, 100, 2), 0, 0); err != nil || v.Period != DefaultATRPeriod {
		t.Fatalf("defaults: %+v %v", v, err)
	}
}

// nsdq is the NSDQ100 x1 long case of the evidence: entry 30000, ATR 315
// (1.05%), spread 0.3 bps, venue min 1%, policy caps 10% / 0.5% equity.
func nsdq() Input {
	return Input{
		Entry: 30000, Amount: 1100, SpreadPct: 0.00003, Equity: 100000, ATR: 315,
		Bounds: Bounds{MinStopLossPct: 0.01, MaxStopLossPct: 1, MaxTakeProfitPct: 10, Tick: 0.01},
		Limits: Limits{MaxStopLossPct: 0.10, MaxLossPctEquity: 0.005},
	}
}

func TestRecommendProfiles(t *testing.T) {
	levels, err := Recommend(nsdq())
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 3 {
		t.Fatalf("profiles %d", len(levels))
	}
	m := levels[Moderate]
	if !m.Feasible || len(m.Clamps) != 0 {
		t.Fatalf("moderate must be feasible without clamps: %+v", m)
	}
	// 2 x 315 = 630 below 30000 -> 29370, 4 x 315 above -> 31260.
	if m.SLRate != 29370 || m.TPRate != 31260 {
		t.Fatalf("moderate rates %v / %v", m.SLRate, m.TPRate)
	}
	if !near(m.SLPct, 0.021, 1e-9) || !near(m.TPPct, 0.042, 1e-9) || !near(m.RR, 2, 1e-9) {
		t.Fatalf("moderate pct %v %v rr %v", m.SLPct, m.TPPct, m.RR)
	}
	units := 1100.0 / 30000
	if !near(m.MaxLossUSD, units*630, 1e-6) || !near(m.TargetUSD, units*1260, 1e-6) {
		t.Fatalf("money %v %v", m.MaxLossUSD, m.TargetUSD)
	}
	if !near(m.SLATR, 2, 1e-9) || !near(m.TPATR, 4, 1e-9) {
		t.Fatalf("atr distances %v %v", m.SLATR, m.TPATR)
	}
	c, a := levels[Conservative], levels[Aggressive]
	if !(c.SLRate > m.SLRate && m.SLRate > a.SLRate) || !(c.TPRate < m.TPRate && m.TPRate < a.TPRate) {
		t.Fatalf("ordering: %v %v %v / %v %v %v", c.SLRate, m.SLRate, a.SLRate, c.TPRate, m.TPRate, a.TPRate)
	}
	if !near(c.RR, 1.5, 1e-9) || !near(a.RR, 2, 1e-9) {
		t.Fatalf("rr %v %v", c.RR, a.RR)
	}
	for p, l := range levels {
		if l.Profile != p {
			t.Fatalf("profile label %s vs %s", l.Profile, p)
		}
	}
}

func TestRecommendNeedsATR(t *testing.T) {
	in := nsdq()
	in.ATR = 0
	if _, err := Recommend(in); err == nil {
		t.Fatal("ATR 0 must error")
	}
	in.ATR, in.Entry = 315, 0
	if _, err := Recommend(in); err == nil {
		t.Fatal("entry 0 must error")
	}
}

func TestNoiseFloorVenueMinimum(t *testing.T) {
	// A calm instrument: 1.5 x ATR is 0.5%, below NSDQ100's 1% venue minimum.
	in := nsdq()
	in.ATR = 100
	levels, _ := Recommend(in)
	c := levels[Conservative]
	if !near(c.SLPct, 0.01, 1e-9) || len(c.Clamps) != 1 || !strings.Contains(c.Clamps[0], "floor") {
		t.Fatalf("floor: %+v", c)
	}
	// A wide spread lifts the floor above the venue minimum: 2 x 1% + 0.25%.
	in.SpreadPct = 0.01
	levels, _ = Recommend(in)
	if a := levels[Aggressive]; !near(a.SLPct, 0.0225, 1e-9) {
		t.Fatalf("spread floor: %+v", a)
	}
	// The policy minimum wins when it is the largest.
	in.Limits.MinStopLossPct = 0.03
	levels, _ = Recommend(in)
	if a := levels[Aggressive]; !near(a.SLPct, 0.03, 1e-9) {
		t.Fatalf("policy floor: %+v", a)
	}
}

func TestLossCaps(t *testing.T) {
	// Policy max distance: 3 x ATR of 1500 = 15% > 10%.
	in := nsdq()
	in.ATR = 1500
	levels, _ := Recommend(in)
	a := levels[Aggressive]
	if !near(a.SLPct, 0.10, 1e-9) || len(a.Clamps) != 1 || !strings.Contains(a.Clamps[0], "policy's 10.0% maximum") {
		t.Fatalf("max distance: %+v", a)
	}
	// Equity cap: a big position makes 0.5% of equity bind first, and here
	// it binds below the venue's 1% floor, which is infeasible: the cap is
	// still applied (never send a wider stop) and the reason says why.
	in = nsdq()
	in.Amount, in.Equity = 50000, 10000 // max loss 50 USD on 50k notional = 0.1%
	levels, _ = Recommend(in)
	m := levels[Moderate]
	if !near(m.SLPct, 0.001, 1e-9) || !near(m.MaxLossUSD, 50, 1e-6) {
		t.Fatalf("equity cap: %+v", m)
	}
	if m.Feasible || !strings.Contains(m.Reason, "no stop satisfies") || !strings.Contains(m.Reason, "maximum loss") {
		t.Fatalf("0.1%% cap vs 1%% floor must be infeasible: %+v", m)
	}
	// The same cap above the floor is a plain clamp: 500 USD of 100k equity
	// on 50k notional is a 1% stop, above the 0.26% noise floor.
	in.Bounds.MinStopLossPct, in.Equity = 0, 100000
	levels, _ = Recommend(in)
	m = levels[Moderate]
	if !m.Feasible || !near(m.SLPct, 0.01, 1e-9) || len(m.Clamps) != 1 || !strings.Contains(m.Clamps[0], "maximum loss of 0.50% of equity (500.00)") {
		t.Fatalf("equity clamp: %+v", m)
	}
	// Venue caps on both: ATR = entry makes the aggressive stop 300% and the
	// target 600%; the venue allows 50% / 500%.
	in = nsdq()
	in.ATR = 30000
	in.Limits = Limits{}
	in.Bounds.MaxStopLossPct, in.Bounds.MaxTakeProfitPct = 0.5, 5
	levels, _ = Recommend(in)
	a = levels[Aggressive]
	if !near(a.SLPct, 0.5, 1e-9) || !near(a.TPPct, 5, 1e-9) || len(a.Clamps) != 2 {
		t.Fatalf("venue caps: %+v", a)
	}
}

func TestRoundingDirection(t *testing.T) {
	in := nsdq()
	in.Entry, in.ATR, in.Amount = 6500.123, 0, 1100
	in.Bounds.MinStopLossPct = 0
	l, err := Build(in, 0.0123456, 0.0345678)
	if err != nil {
		t.Fatal(err)
	}
	// Exact: 6419.8726 -> floor to 6419.87; 6724.8180 -> ceil to 6724.82.
	if l.SLRate != 6419.87 || l.TPRate != 6724.82 {
		t.Fatalf("rounding %v %v", l.SLRate, l.TPRate)
	}
	if l.SLPct < 0.0123456 || l.TPPct < 0.0345678 {
		t.Fatalf("rounding must never tighten: %v %v", l.SLPct, l.TPPct)
	}
	if l.Profile != Custom || l.SLATR != 0 {
		t.Fatalf("%+v", l)
	}
}

func TestBuildValidation(t *testing.T) {
	in := nsdq()
	if _, err := Build(in, 0, 0.01); err == nil {
		t.Fatal("sl 0")
	}
	if _, err := Build(in, -0.01, 0); err == nil {
		t.Fatal("sl negative")
	}
	if _, err := Build(in, 0.02, -1); err == nil {
		t.Fatal("tp negative")
	}
	if _, err := Build(in, math.NaN(), 0); err == nil {
		t.Fatal("NaN")
	}
	l, err := Build(in, 0.02, 0)
	if err != nil || l.TPRate != 0 || l.TPPct != 0 || l.RR != 0 || l.TargetUSD != 0 {
		t.Fatalf("no target: %+v %v", l, err)
	}
}

func TestMarkBelowEntry(t *testing.T) {
	// Protecting an existing long that is under water: distances from the
	// mark, percentages against the open rate; the stop must stay below the mark.
	in := nsdq()
	in.Mark = 29000
	levels, _ := Recommend(in)
	m := levels[Moderate]
	if m.SLRate != 29000-630 || m.TPRate != 29000+1260 {
		t.Fatalf("from mark: %v %v", m.SLRate, m.TPRate)
	}
	if !near(m.SLPct, (30000-28370)/30000.0, 1e-9) {
		t.Fatalf("pct against entry: %v", m.SLPct)
	}
	// A custom stop above the mark is infeasible.
	l, _ := Build(in, 0.02, 0) // 29400 > mark 29000
	if l.Feasible || !strings.Contains(l.Reason, "not below the current price") {
		t.Fatalf("%+v", l)
	}
	// A target below the mark too.
	in.Mark = 31000
	l, _ = Build(in, 0.05, 0.02) // TP 30600 < 31000
	if l.Feasible || !strings.Contains(l.Reason, "not above the current price") {
		t.Fatalf("%+v", l)
	}
}

func TestDisasterStopPct(t *testing.T) {
	if got := DisasterStopPct(0.0105); !near(got, 0.08, 1e-12) {
		t.Fatalf("floor wins: %v", got)
	}
	if got := DisasterStopPct(0.03); !near(got, 0.12, 1e-12) {
		t.Fatalf("4 x ATR wins: %v", got)
	}
}

func TestRatchet(t *testing.T) {
	step := 0.25 * 300 // 75
	cases := []struct {
		name                string
		current, cand, mark float64
		want                float64
		raise               bool
	}{
		{"no stop takes candidate", 0, 29000, 30000, 29000, true},
		{"raise by more than a step", 29000, 29100, 30000, 29100, true},
		{"too small a move", 29000, 29050, 30000, 29000, false},
		{"never lower", 29000, 28000, 30000, 29000, false},
		{"candidate at the mark", 29000, 30000, 30000, 29000, false},
		{"candidate above the mark", 0, 30500, 30000, 0, false},
		{"no candidate", 29000, 0, 30000, 29000, false},
		{"exactly a step", 29000, 29075, 30000, 29075, true},
	}
	for _, c := range cases {
		got, raise := Ratchet(c.current, c.cand, step, c.mark)
		if got != c.want || raise != c.raise {
			t.Errorf("%s: got %v %v want %v %v", c.name, got, raise, c.want, c.raise)
		}
	}
}

func TestHasStopAndProfiles(t *testing.T) {
	if HasStop(0.01) || HasStop(0) || !HasStop(29000) {
		t.Fatal("HasStop")
	}
	if p, err := ParseProfile(""); err != nil || p != Moderate {
		t.Fatalf("default %v %v", p, err)
	}
	if _, err := ParseProfile("yolo"); err == nil {
		t.Fatal("unknown profile")
	}
	if _, ok := ProfileMultipliers(Custom); ok {
		t.Fatal("custom has no multipliers")
	}
	if m, ok := ProfileMultipliers(Moderate); !ok || m.SL != 2 || m.TP != 4 {
		t.Fatalf("%+v", m)
	}
}

// A stop clamped to the ceiling is rounded up to the tick, never past the
// cap: at an entry where the exact cap falls between ticks, flooring would
// put the distance a hair over the policy's maximum, which risk.Check (and
// the venue) would refuse. Found by the real-account canary (2026-10-22:
// 2.45 % of 29,851 = 29,119.65 exactly after flooring to 29,119.65 reads
// 2.4500017 %).
func TestStopAtTheCeilingNeverRoundsPastIt(t *testing.T) {
	in := Input{Entry: 29851, Mark: 29850, Amount: 1000, SpreadPct: 0.00003, ATR: 475.7,
		Bounds: Bounds{MinStopLossPct: 0.0005, MaxStopLossPct: 0.025, MaxTakeProfitPct: 10, Tick: 0.01},
		Limits: Limits{MinStopLossPct: 0.023, MaxStopLossPct: 0.0245}}
	all, err := Recommend(in)
	if err != nil {
		t.Fatal(err)
	}
	for p, l := range all {
		if !l.Feasible || l.SLPct > 0.0245 || l.SLPct < 0.023 {
			t.Errorf("%s: stop %.7f%% (%v) is outside the 2.3-2.45%% band", p, l.SLPct*100, l.SLRate)
		}
		// Moderate (2 ATR = 3.19 %) and aggressive (3 ATR) are clamped to the
		// ceiling and must land one tick inside it; conservative (1.5 ATR =
		// 2.39 %) is inside the band and untouched.
		if p != Conservative && l.SLRate != 29119.66 {
			t.Errorf("%s: stop rate %v want 29119.66", p, l.SLRate)
		}
	}
	// A custom distance under the ceiling still rounds down as before.
	l, err := Build(in, 0.024, 0)
	if err != nil || l.SLRate != math.Floor(29851*(1-0.024)*100+1e-9)/100 || l.SLPct < 0.024 {
		t.Fatalf("under the ceiling: %+v %v", l, err)
	}
}
