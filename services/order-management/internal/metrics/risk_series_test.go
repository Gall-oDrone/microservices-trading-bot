package metrics

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"bitso-trading-platform/shared/pkg/varmodel"
)

func TestSlippageBps(t *testing.T) {
	cases := []struct {
		side     string
		ref, avg float64
		want     float64
		ok       bool
	}{
		{"buy", 100, 101, 100, true},     // paid 1 % more: cost
		{"buy", 100, 99.5, -50, true},    // improvement
		{"sell", 100, 99, 100, true},     // received 1 % less: cost
		{"sell", 100, 100.25, -25, true}, // improvement
		{"buy", 0, 100, 0, false},
		{"sell", 100, 0, 0, false},
		{"hold", 100, 100, 0, false},
		{"buy", math.NaN(), 100, 0, false},
		{"buy", math.Inf(1), 100, 0, false},
	}
	for _, c := range cases {
		got, ok := SlippageBps(c.side, c.ref, c.avg)
		if ok != c.ok || math.Abs(got-c.want) > 1e-9 {
			t.Errorf("SlippageBps(%s, %v, %v) = %v, %v; want %v, %v", c.side, c.ref, c.avg, got, ok, c.want, c.ok)
		}
	}
}

func TestObserveExecutionSplitsCostByDirection(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewRiskSeries(reg)
	if !m.ObserveExecution("btc_mxn", "buy", 0.01, 1_000_000, 1_001_000) { // +10 bps on 10,010
		t.Fatal("not observed")
	}
	m.ObserveExecution("btc_mxn", "buy", 0.01, 1_000_000, 999_500) // -5 bps on 9,995
	if m.ObserveExecution("btc_mxn", "buy", 0, 1, 1) || m.ObserveExecution("btc_mxn", "buy", 1, 0, 1) {
		t.Fatal("unusable inputs must not be observed")
	}
	adv := testutil.ToFloat64(m.slippageCost.WithLabelValues("btc_mxn", "buy", SlippageAdverse))
	imp := testutil.ToFloat64(m.slippageCost.WithLabelValues("btc_mxn", "buy", SlippageImprovement))
	notional := testutil.ToFloat64(m.filledNotional.WithLabelValues("btc_mxn", "buy"))
	if math.Abs(adv-10.01) > 1e-6 || math.Abs(imp-4.9975) > 1e-6 || math.Abs(notional-20_005) > 1e-6 {
		t.Fatalf("adverse %v improvement %v notional %v", adv, imp, notional)
	}
	// Notional-weighted slippage, as the alert computes it.
	if bps := (adv - imp) / notional * 1e4; math.Abs(bps-2.5056) > 1e-3 {
		t.Fatalf("weighted bps %v", bps)
	}
	if n := testutil.CollectAndCount(reg, "order_arrival_slippage_bps"); n != 1 {
		t.Fatalf("histogram series %d", n)
	}
}

func TestSetPortfolioPublishes(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewRiskSeries(reg)
	at := time.Unix(1_790_000_000, 0)
	m.SetPortfolio([]BookExposure{
		{Book: "btc_mxn", Asset: "btc", Currency: "MXN", Base: 0.1, Mark: 1_000_000, Quote: 100_000, DailyVol: 0.04},
		{Book: "btc_usd", Asset: "btc", Currency: "USD", Base: -0.01, Mark: 60_000, Quote: -600, DailyVol: 0.04, Fallback: true},
	}, map[string]float64{"btc": 0.09}, map[string]CurrencyRisk{
		"MXN": {Gross: 100_000, Net: 100_000, VaR: 9305.39, Limit: 20_000},
		"USD": {Gross: 600, Net: -600, VaR: 55.83},
	}, at)

	if v := testutil.ToFloat64(m.varQuote.WithLabelValues("MXN")); v != 9305.39 {
		t.Fatalf("var %v", v)
	}
	if v := testutil.ToFloat64(m.markFallback.WithLabelValues("btc_usd")); v != 1 {
		t.Fatalf("fallback %v", v)
	}
	if v := testutil.ToFloat64(m.lastRun); v != float64(at.Unix()) {
		t.Fatalf("last run %v", v)
	}
	// No limit configured for USD: no limit series (an alert would divide by it).
	if n := testutil.CollectAndCount(reg, "portfolio_var_limit_quote"); n != 1 {
		t.Fatalf("limit series %d, want 1 (MXN only)", n)
	}
}

func TestSetPortfolioPublishesVaRModel(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewRiskSeries(reg)
	at := time.Unix(1_790_000_000, 0)
	bt := varmodel.Backtest{Observations: 250, ExceptionsLong: 6, ExceptionsShort: 1, KupiecLong: 0.06, KupiecShort: 0.4, Zone: varmodel.ZoneYellow}
	books := []BookExposure{
		{Book: "btc_mxn", Currency: "MXN", Quote: 100_000, DailyVol: 0.021, VolSource: "estimated", VolDataAgeSeconds: 3600,
			Model: &VolModel{EWMA: 0.012, Long: 0.021, Estimate: 0.021, Backtest: bt}},
		{Book: "eth_mxn", Currency: "MXN", DailyVol: 0.04, VolSource: "fallback", VolDataAgeSeconds: -1},
	}
	m.SetPortfolio(books, nil, map[string]CurrencyRisk{
		"MXN": {VaR: 4885, HistVaR: 6100, HistScenarios: 365, HistOK: true},
	}, at)

	for src, want := range map[string]float64{"estimated": 1, "fallback": 0, "override": 0, "fixed": 0} {
		if v := testutil.ToFloat64(m.volSource.WithLabelValues("btc_mxn", src)); v != want {
			t.Errorf("btc_mxn source %s = %v", src, v)
		}
	}
	if v := testutil.ToFloat64(m.volSource.WithLabelValues("eth_mxn", "fallback")); v != 1 {
		t.Errorf("eth_mxn fallback = %v", v)
	}
	if v := testutil.ToFloat64(m.volEstimate.WithLabelValues("btc_mxn", "ewma")); v != 0.012 {
		t.Errorf("ewma %v", v)
	}
	if v := testutil.ToFloat64(m.btZone.WithLabelValues("btc_mxn")); v != 1 {
		t.Errorf("zone %v", v)
	}
	if v := testutil.ToFloat64(m.btExc.WithLabelValues("btc_mxn", "long")); v != 6 {
		t.Errorf("long exceptions %v", v)
	}
	if v := testutil.ToFloat64(m.volDataAge.WithLabelValues("eth_mxn")); v != -1 {
		t.Errorf("eth_mxn data age %v", v)
	}
	// No estimate for eth_mxn: no model series for it.
	if n := testutil.CollectAndCount(reg, "risk_var_backtest_zone"); n != 1 {
		t.Errorf("zone series %d, want 1", n)
	}
	if v := testutil.ToFloat64(m.histVaR.WithLabelValues("MXN")); v != 6100 {
		t.Errorf("historical VaR %v", v)
	}

	// History goes missing: the historical series disappears rather than
	// freezing at its last value.
	m.SetPortfolio(books, nil, map[string]CurrencyRisk{"MXN": {VaR: 4885}}, at)
	if n := testutil.CollectAndCount(reg, "portfolio_var_historical_quote"); n != 0 {
		t.Errorf("historical VaR series %d after history was lost, want 0", n)
	}
	if n := testutil.CollectAndCount(reg, "portfolio_var_historical_scenarios"); n != 0 {
		t.Errorf("historical scenario series %d after history was lost, want 0", n)
	}
}

func TestSetPortfolioPublishesESAndStress(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewRiskSeries(reg)
	at := time.Unix(1_790_000_000, 0)
	stress := []StressResult{
		{Scenario: "spot-50%", Type: StressHypothetical, Loss: 50_000, OK: true},
		{Scenario: "2020-03-covid", Type: StressHistorical, Loss: 36_000, OK: true, Proxied: true},
		{Scenario: "2018-01-crash", Type: StressHistorical, OK: false},
	}
	books := []BookExposure{{Book: "btc_mxn", Currency: "MXN", EpisodeTroughs: map[string]float64{"2020-03-covid": -0.358}}}
	m.SetPortfolio(books, nil, map[string]CurrencyRisk{
		"MXN": {ESParam: 4700, HistES: 6900, HistOK: true, Stress: stress, StressLimit: 40_000},
	}, at)

	for _, c := range []struct {
		g    *prometheus.GaugeVec
		lv   []string
		want float64
	}{
		{m.esQuote, []string{"MXN", ESParametric}, 4700},
		{m.esQuote, []string{"MXN", ESHistorical}, 6900},
		{m.stressLoss, []string{"MXN", "spot-50%", StressHypothetical}, 50_000},
		{m.stressLoss, []string{"MXN", "2020-03-covid", StressHistorical}, 36_000},
		{m.stressWorst, []string{"MXN"}, 50_000},
		{m.stressLimit, []string{"MXN"}, 40_000},
		{m.stressProxied, []string{"MXN", "2020-03-covid"}, 1},
		{m.stressIncomplete, []string{"MXN", "2020-03-covid"}, 0},
		{m.stressIncomplete, []string{"MXN", "2018-01-crash"}, 1},
		{m.episodeTrough, []string{"btc_mxn", "2020-03-covid"}, -0.358},
	} {
		if v := testutil.ToFloat64(c.g.WithLabelValues(c.lv...)); v != c.want {
			t.Errorf("%v = %v, want %v", c.lv, v, c.want)
		}
	}
	// The incomplete scenario has no loss series (2 of 3 published).
	if n := testutil.CollectAndCount(reg, "portfolio_stress_loss_quote"); n != 2 {
		t.Errorf("stress loss series %d, want 2", n)
	}

	// A scenario that loses its data, and history that goes missing, drop
	// their series instead of freezing.
	stress[1].OK = false
	m.SetPortfolio(books, nil, map[string]CurrencyRisk{"MXN": {ESParam: 4700, Stress: stress}}, at)
	if n := testutil.CollectAndCount(reg, "portfolio_stress_loss_quote"); n != 1 {
		t.Errorf("stress loss series %d after covid lost data, want 1", n)
	}
	if n := testutil.CollectAndCount(reg, "portfolio_es_quote"); n != 1 {
		t.Errorf("ES series %d without history, want 1 (parametric)", n)
	}
}

func TestSetPortfolioPublishesCapitalRatiosAndReverseStress(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewRiskSeries(reg)
	at := time.Unix(1_790_000_000, 0)
	stress := []StressResult{{Scenario: "2018-01-crash", Type: StressHistorical, Loss: 62_000, OK: true}}
	r := CurrencyRisk{Gross: 100_000, Net: 100_000, VaR: 4_600, ESParam: 4_700, HistVaR: 5_400, HistES: 6_900, HistOK: true,
		Stress: stress, Capital: 50_000}
	m.SetPortfolio(nil, nil, map[string]CurrencyRisk{"MXN": r}, at)
	for _, c := range []struct {
		measure string
		want    float64
	}{
		{CapVaRParametric, 0.092}, {CapVaRHistorical, 0.108}, {CapESParametric, 0.094}, {CapESHistorical, 0.138},
		{CapStressWorst, 1.24}, {CapExposureNet, 2}, {CapExposureGross, 2},
	} {
		if v := testutil.ToFloat64(m.capitalRatio.WithLabelValues("MXN", c.measure)); math.Abs(v-c.want) > 1e-12 {
			t.Errorf("%s = %v, want %v", c.measure, v, c.want)
		}
	}
	if v := testutil.ToFloat64(m.capital.WithLabelValues("MXN")); v != 50_000 {
		t.Errorf("capital %v", v)
	}
	// A 50 % fall loses the 50k capital on a 100k long.
	if v := testutil.ToFloat64(m.reverseStress.WithLabelValues("MXN")); math.Abs(v+0.5) > 1e-12 {
		t.Errorf("reverse stress %v", v)
	}

	// History lost and capital above the long's exposure: the historical
	// ratios and the reverse-stress move disappear instead of freezing.
	r.HistOK, r.Capital = false, 150_000
	m.SetPortfolio(nil, nil, map[string]CurrencyRisk{"MXN": r}, at)
	if n := testutil.CollectAndCount(reg, "portfolio_risk_capital_ratio"); n != 5 {
		t.Errorf("capital ratio series %d, want 5", n)
	}
	if n := testutil.CollectAndCount(reg, "portfolio_reverse_stress_move_ratio"); n != 0 {
		t.Errorf("reverse stress series %d, want 0 (unreachable for a long)", n)
	}

	// No capital configured: nothing is published.
	reg2 := prometheus.NewRegistry()
	m2 := NewRiskSeries(reg2)
	m2.SetPortfolio(nil, nil, map[string]CurrencyRisk{"USD": {Net: 1000, VaR: 50}}, at)
	for _, name := range []string{"portfolio_capital_quote", "portfolio_risk_capital_ratio", "portfolio_reverse_stress_move_ratio"} {
		if n := testutil.CollectAndCount(reg2, name); n != 0 {
			t.Errorf("%s: %d series without capital", name, n)
		}
	}
}
