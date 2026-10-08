package metrics

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
