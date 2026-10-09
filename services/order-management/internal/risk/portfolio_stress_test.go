package risk

import (
	"context"
	"math"
	"testing"

	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/shared/pkg/varmodel"
)

func pathOf(start string, cums ...float64) []varmodel.PathPoint {
	d := date0(start)
	out := make([]varmodel.PathPoint, len(cums))
	for i, c := range cums {
		out[i] = varmodel.PathPoint{Date: d.AddDate(0, 0, i), Cum: c}
	}
	return out
}

func stressBy(r metrics.CurrencyRisk) map[string]metrics.StressResult {
	out := map[string]metrics.StressResult{}
	for _, s := range r.Stress {
		out[s.Scenario] = s
	}
	return out
}

func TestPortfolioStressAndES(t *testing.T) {
	covid := pathOf("2020-03-08", -0.20, -0.36, -0.30)
	celsiusMXN := pathOf("2022-06-10", -0.10, -0.33)
	celsiusUSD := pathOf("2022-06-10", -0.12, -0.38)
	mxnView := viewWith(0.02, returnsWith(300, nil))
	mxnView.Episodes = map[string][]varmodel.PathPoint{"2020-03-covid": covid, "2022-06-celsius-3ac": celsiusMXN}
	usdView := viewWith(0.025, returnsWith(300, nil))
	usdView.Episodes = map[string][]varmodel.PathPoint{"2022-06-celsius-3ac": celsiusUSD} // listed 2020-04

	pm := &PortfolioMonitor{
		Positions: positionRepoWith(t, pos("btc_mxn", "long", 0.1, 1_000_000), pos("btc_usd", "long", 0.01, 60_000)),
		Config: PortfolioConfig{
			DefaultDailyVol: 0.04,
			StressShocks:    []float64{-0.5, -0.3, 0.2},
			StressLimits:    map[string]float64{"MXN": 40_000, "EUR": 1},
		},
		Vol: fakeVol{"btc_mxn": {mxnView, true}, "btc_usd": {usdView, true}},
	}
	snap, err := pm.Snapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mxn, usd := snap.Currencies["MXN"], snap.Currencies["USD"]

	// ES: parametric is 2.338 x the same sigma as the 2.326 x VaR.
	if want := varmodel.ZES * 100_000 * 0.02; math.Abs(mxn.ESParam-want) > 1e-6 {
		t.Fatalf("parametric ES %v want %v", mxn.ESParam, want)
	}
	if !mxn.HistOK || mxn.HistES != 0 { // flat synthetic returns: no loss
		t.Fatalf("historical ES %+v", mxn)
	}

	m := stressBy(mxn)
	for id, want := range map[string]float64{"spot-50%": 50_000, "spot-30%": 30_000, "spot+20%": 0, "2020-03-covid": 36_000, "2022-06-celsius-3ac": 33_000} {
		if s := m[id]; !s.OK || math.Abs(s.Loss-want) > 1e-6 || s.Proxied {
			t.Errorf("MXN %s: %+v, want loss %v", id, s, want)
		}
	}
	// No book has data for 2018: incomplete, not a silent zero.
	if s := m["2018-01-crash"]; s.OK || s.Type != metrics.StressHistorical {
		t.Errorf("MXN 2018-01-crash %+v", s)
	}
	if worst, name := mxn.WorstStress(); worst != 50_000 || name != "spot-50%" || mxn.StressLimit != 40_000 {
		t.Errorf("MXN worst %v %q limit %v", worst, name, mxn.StressLimit)
	}

	u := stressBy(usd)
	// btc_usd did not trade in March 2020: proxied by btc_mxn's path.
	if s := u["2020-03-covid"]; !s.OK || !s.Proxied || math.Abs(s.Loss-600*0.36) > 1e-6 {
		t.Errorf("USD covid %+v", s)
	}
	if s := u["2022-06-celsius-3ac"]; !s.OK || s.Proxied || math.Abs(s.Loss-600*0.38) > 1e-6 {
		t.Errorf("USD celsius %+v", s)
	}

	// The book's own troughs are published for transparency.
	for _, b := range snap.Books {
		if b.Book == "btc_usd" && (len(b.EpisodeTroughs) != 1 || b.EpisodeTroughs["2022-06-celsius-3ac"] != -0.38) {
			t.Errorf("btc_usd troughs %v", b.EpisodeTroughs)
		}
	}

	// A stress limit without positions is still published, with the
	// hypothetical scenarios at zero.
	if eur := snap.Currencies["EUR"]; eur.StressLimit != 1 || len(eur.Stress) == 0 {
		t.Errorf("EUR %+v", eur)
	}
}

func TestPortfolioStressFixedModelIsHypotheticalOnly(t *testing.T) {
	pm := &PortfolioMonitor{
		Positions: positionRepoWith(t, pos("btc_mxn", "long", 0.1, 1_000_000)),
		Config:    PortfolioConfig{DefaultDailyVol: 0.04, StressShocks: DefaultStressShocks},
	}
	snap, err := pm.Snapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mxn := snap.Currencies["MXN"]
	if len(mxn.Stress) != len(DefaultStressShocks) {
		t.Fatalf("stress %+v", mxn.Stress)
	}
	for _, s := range mxn.Stress {
		if s.Type != metrics.StressHypothetical {
			t.Fatalf("historical scenario without an estimator: %+v", s)
		}
	}
	if worst, _ := mxn.WorstStress(); math.Abs(worst-50_000) > 1e-6 {
		t.Fatalf("worst %v", worst)
	}
}

func TestShockID(t *testing.T) {
	for s, want := range map[float64]string{-0.5: "spot-50%", -0.1: "spot-10%", 0.2: "spot+20%", -0.125: "spot-12.5%"} {
		if got := shockID(s); got != want {
			t.Errorf("shockID(%v) = %q, want %q", s, got, want)
		}
	}
}

func TestLoadPortfolioConfigStress(t *testing.T) {
	c, err := LoadPortfolioConfig(envMap(nil))
	if err != nil || len(c.StressShocks) != len(DefaultStressShocks) || len(c.StressLimits) != 0 {
		t.Fatalf("defaults %+v %v", c, err)
	}
	c, err = LoadPortfolioConfig(envMap(map[string]string{
		EnvStressShocks: " -0.4, 0.25 ",
		EnvStressLimits: "mxn=150000,USD=5000",
	}))
	if err != nil || len(c.StressShocks) != 2 || c.StressShocks[0] != -0.4 || c.StressLimits["MXN"] != 150_000 || c.StressLimits["USD"] != 5000 {
		t.Fatalf("parsed %+v %v", c, err)
	}
	for k, v := range map[string]string{
		EnvStressShocks: "-1",
		EnvStressLimits: "MXN=0",
	} {
		if _, err := LoadPortfolioConfig(envMap(map[string]string{k: v})); err == nil {
			t.Errorf("%s=%q accepted", k, v)
		}
	}
	for _, v := range []string{"0", "-0.3,-0.3", "30%", "-0.3,"} {
		if _, err := LoadPortfolioConfig(envMap(map[string]string{EnvStressShocks: v})); err == nil {
			t.Errorf("%s=%q accepted", EnvStressShocks, v)
		}
	}
}
