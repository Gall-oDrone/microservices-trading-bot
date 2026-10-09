package montecarlo

import (
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/dailyrule"
)

func readBars(t *testing.T, f string) []dailyrule.Bar {
	t.Helper()
	rows, err := bitsodaily.ReadCSV(filepath.Join("..", "..", "..", "docs", "backtest-readiness", f))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]dailyrule.Bar, len(rows))
	for i, r := range rows {
		d, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = dailyrule.Bar{Date: d, Open: r.Open, High: r.High, Low: r.Low, Close: r.Close}
	}
	return out
}

var (
	mxnPrimary = dailyrule.Costs{Buy: 0.0070, Sell: 0.0070}
	usdPrimary = dailyrule.Costs{Buy: 0.0040, Sell: 0.0040}
)

// The pre-registrations' backtest base rates (§4 tables), reproduced from
// the committed Bitso closes: btc_mxn 2018-2026 at 70 bps per leg, H2 5/9
// (lost 2019, 2020, 2023, 2024) and H1 6/9; btc_usd 2021-2026 at 40 bps,
// H1 5/6 (failed 2024: 28.4 % vs 27.5 %).
func TestCalendarYearsReproducePreregBaseRates(t *testing.T) {
	mxn := CalendarYears(readBars(t, "evidence-2026-09-27/btc_mxn_daily_bitso.csv"), 50, 2018, mxnPrimary)
	if len(mxn.Years) != 9 || mxn.BeatsHold != 5 || mxn.ShallowerDD != 6 {
		t.Fatalf("btc_mxn: %d years, beats hold %d, shallower %d", len(mxn.Years), mxn.BeatsHold, mxn.ShallowerDD)
	}
	for _, y := range mxn.Years {
		lost := y.Year == 2019 || y.Year == 2020 || y.Year == 2023 || y.Year == 2024
		if y.BeatsHold == lost {
			t.Errorf("btc_mxn %d: beats hold %v", y.Year, y.BeatsHold)
		}
	}
	usd := CalendarYears(readBars(t, "evidence-2026-09-28/btc_usd_daily_bitso.csv"), 50, 2021, usdPrimary)
	if len(usd.Years) != 6 || usd.ShallowerDD != 5 {
		t.Fatalf("btc_usd: %d years, shallower %d", len(usd.Years), usd.ShallowerDD)
	}
	for _, y := range usd.Years {
		if y.Year == 2024 && (y.ShallowerDD || math.Abs(y.TrendMaxDD-0.284) > 0.001 || math.Abs(y.HoldMaxDD-0.275) > 0.001) {
			t.Errorf("btc_usd 2024: %+v", y)
		}
	}
}

// The trip distribution on btc_mxn since 2018 at 70 bps per leg: the
// positive skew the plan documents (§6.4.11).
func TestTripStatsBTCMXN(t *testing.T) {
	bars := readBars(t, "evidence-2026-09-27/btc_mxn_daily_bitso.csv")
	s := TripStats(bars, 50, time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC), mxnPrimary)
	if s.Count < 100 || s.Count > 106 {
		t.Fatalf("count %d", s.Count)
	}
	if !(s.WinRate > 0.12 && s.WinRate < 0.25) || !(s.Mean > 0.02) || !(s.Median < -0.01) || !(s.Payoff > 5) {
		t.Fatalf("shape %+v", s)
	}
	if s.Best.Entry != "2020-10-10" || s.Best.Return < 3 {
		t.Fatalf("best %+v", s.Best)
	}
	// The edge rests on a few trends: without the best trip the chain loses.
	if !(s.Compounded > 0) || !(s.CompoundedExB1 < 0) || !(s.CompoundedExB3 < s.CompoundedExB1) {
		t.Fatalf("concentration %v %v %v", s.Compounded, s.CompoundedExB1, s.CompoundedExB3)
	}
	// Chaining the closed trips and the open one equals the simulator's
	// window return (the open trip's exit leg is the only difference).
	res := dailyrule.Simulate(bars, dailyrule.Trend(bars, 50), sortIdx(bars, "2018-01-01"), len(bars)-1, mxnPrimary)
	chain := 1 + s.Compounded
	if s.Open != nil {
		chain *= (1 + s.Open.Return) * (1 - mxnPrimary.Sell)
	}
	if math.Abs((chain-1)*100-res.ReturnPct) > 1e-6 {
		t.Fatalf("chained %.6f%% vs simulator %.6f%%", (chain-1)*100, res.ReturnPct)
	}
}

func sortIdx(bars []dailyrule.Bar, d string) int {
	for i, b := range bars {
		if b.Date.Format("2006-01-02") >= d {
			return i
		}
	}
	return len(bars)
}

func TestRunDeterministicAndSane(t *testing.T) {
	bars := readBars(t, "evidence-2026-09-27/btc_mxn_daily_bitso.csv")
	cfg := Config{Paths: 400, Horizon: 365, MeanBlock: 20, Seed: 7, SMA: 50, Costs: mxnPrimary}
	from := sortIdx(bars, "2018-01-01")
	a, err := Run(bars, from, cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Run(bars, from, cfg)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same seed, different result")
	}
	cfg.Seed = 8
	if c, _ := Run(bars, from, cfg); reflect.DeepEqual(a.TrendReturn, c.TrendReturn) {
		t.Fatal("seed ignored")
	}
	for name, d := range map[string]Dist{"trend": a.TrendReturn, "hold": a.HoldReturn, "trend dd": a.TrendMaxDD} {
		if !(d.P5 <= d.P25 && d.P25 <= d.P50 && d.P50 <= d.P75 && d.P75 <= d.P95) {
			t.Errorf("%s quantiles out of order %+v", name, d)
		}
	}
	for name, p := range map[string]Prob{"loss": a.PTrendLoss, "beats": a.PBeatsHold, "shallower": a.PShallowerDD} {
		if !(p.Lo <= p.P && p.P <= p.Hi && p.Lo >= 0 && p.Hi <= 1) {
			t.Errorf("%s %+v", name, p)
		}
	}
	// A trend rule out of the market half the time has the shallower
	// drawdown in most paths (history: 6 of 9 years), and loses to holding
	// in the median path of a market that rose over the sample.
	if !(a.PShallowerDD.P > 0.5) || !(a.TrendMaxDD.P50 < a.HoldMaxDD.P50) {
		t.Errorf("drawdown: %+v vs %+v (p %v)", a.TrendMaxDD, a.HoldMaxDD, a.PShallowerDD.P)
	}
	total := 0
	for _, bin := range a.Histogram {
		total += bin.Trend
	}
	if total != cfg.Paths || len(a.Histogram) != histBins || a.SampleDays < 3000 || a.StartClose != bars[len(bars)-1].Close {
		t.Errorf("histogram %d paths, %d bins, %d sample days, start %v", total, len(a.Histogram), a.SampleDays, a.StartClose)
	}
}

// On a history that only rises, buy-and-hold gains on every path and the
// rule (long from the first day) matches it less one round trip at most.
func TestRunMonotoneHistory(t *testing.T) {
	var bars []dailyrule.Bar
	d0 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	p := 100.0
	for i := 0; i < 400; i++ {
		o := p * 1.001
		c := o * 1.002
		bars = append(bars, dailyrule.Bar{Date: d0.AddDate(0, 0, i), Open: o, High: c, Low: o, Close: c})
		p = c
	}
	s, err := Run(bars, 1, Config{Paths: 50, Horizon: 60, MeanBlock: 5, Seed: 1, SMA: 50, Costs: dailyrule.Costs{}})
	if err != nil {
		t.Fatal(err)
	}
	// Both enter at the first synthetic open, so the first day's gap is
	// not earned: (gap x intraday)^60 / gap.
	want := math.Pow(1.001*1.002, 60)/1.001 - 1
	if math.Abs(s.HoldReturn.P50-want) > 1e-9 || math.Abs(s.TrendReturn.P50-want) > 1e-9 || s.PHoldLoss.P != 0 || !s.StartLong {
		t.Fatalf("%+v", s)
	}
	if s.HoldMaxDD.P95 != 0 || s.PBeatsHold.P != 0 {
		t.Fatalf("dd %+v beats %+v", s.HoldMaxDD, s.PBeatsHold)
	}
}

func TestRunRejectsBadConfig(t *testing.T) {
	bars := readBars(t, "evidence-2026-09-28/btc_usd_daily_bitso.csv")
	for _, c := range []Config{
		{Paths: 0, Horizon: 365, MeanBlock: 20, SMA: 50},
		{Paths: MaxPaths + 1, Horizon: 365, MeanBlock: 20, SMA: 50},
		{Paths: 10, Horizon: 5, MeanBlock: 20, SMA: 50},
		{Paths: 10, Horizon: 365, MeanBlock: 0, SMA: 50},
	} {
		if _, err := Run(bars, 1, c); err == nil {
			t.Errorf("accepted %+v", c)
		}
	}
	if _, err := Run(bars[:40], 1, Config{Paths: 10, Horizon: 365, MeanBlock: 20, SMA: 50}); err == nil {
		t.Error("accepted 40 bars")
	}
}

func TestWilson(t *testing.T) {
	p := wilson(50, 100)
	if p.P != 0.5 || math.Abs(p.Lo-0.4038) > 1e-3 || math.Abs(p.Hi-0.5962) > 1e-3 {
		t.Fatalf("%+v", p)
	}
	if z := wilson(0, 100); z.Lo != 0 || z.Hi < 0.03 {
		t.Fatalf("%+v", z)
	}
}
