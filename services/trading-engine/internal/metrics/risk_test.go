package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRiskMetricsSeries(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewRiskMetrics(reg)

	m.SetPolicy("v1", "built-in")
	m.SetPolicy("v2", "/etc/policy.json") // replaces, never two versions at once
	if n := testutil.CollectAndCount(m.policyInfo); n != 1 {
		t.Fatalf("policy info series = %d, want 1", n)
	}
	if v := testutil.ToFloat64(m.policyInfo.WithLabelValues("v2", "/etc/policy.json")); v != 1 {
		t.Fatalf("policy info = %v", v)
	}

	m.SetHaltState(2, 0, false, time.Unix(100, 0))
	if testutil.ToFloat64(m.haltActive) != 0 || testutil.ToFloat64(m.haltFilesConfig) != 2 {
		t.Fatal("clear halt state wrong")
	}
	m.SetHaltState(2, 1, false, time.Unix(200, 0)) // invalid file blocks too
	if testutil.ToFloat64(m.haltActive) != 1 || testutil.ToFloat64(m.haltFilesInvalid) != 1 {
		t.Fatal("invalid halt file should set trading_halt_active")
	}
	m.SetHaltState(2, 0, true, time.Unix(300, 0))
	if testutil.ToFloat64(m.haltActive) != 1 || testutil.ToFloat64(m.haltLastCheck) != 300 {
		t.Fatal("halted state wrong")
	}

	m.RecordPolicyCheck("btc_mxn", PolicyBlocked, []string{"halted", "max_order_btc"})
	m.RecordPolicyCheck("btc_mxn", PolicyAllowed, nil)
	if testutil.ToFloat64(m.policyRejections.WithLabelValues("btc_mxn", "halted")) != 1 ||
		testutil.ToFloat64(m.policyChecks.WithLabelValues("btc_mxn", PolicyAllowed)) != 1 {
		t.Fatal("policy counters wrong")
	}

	m.ObserveUtilization("btc_mxn", LimitMaxOrderBTC, 0.05, 0.1)
	m.ObserveUtilization("btc_mxn", LimitMaxOrderBTC, 1, 0) // disabled limit: skipped
	m.ObservePriceDeviation("btc_mxn", "buy", 12)
	m.ObserveSignalAge("btc_mxn", -time.Second) // clock skew clamps to 0
	m.ObserveSignalToOrder("btc_mxn", 3*time.Second)
	m.SetSessionUtilization(LimitMaxDailyLoss, 0.4)
	m.SetLimit(SessionBook, LimitMaxDailyLoss, 500)

	want := `
# HELP order_limit_utilization_ratio Order value / limit for each per-order limit (1 = at the limit)
# TYPE order_limit_utilization_ratio histogram
order_limit_utilization_ratio_bucket{book="btc_mxn",limit="max_order_btc",le="0.1"} 0
order_limit_utilization_ratio_bucket{book="btc_mxn",limit="max_order_btc",le="0.25"} 0
order_limit_utilization_ratio_bucket{book="btc_mxn",limit="max_order_btc",le="0.5"} 1
order_limit_utilization_ratio_bucket{book="btc_mxn",limit="max_order_btc",le="0.75"} 1
order_limit_utilization_ratio_bucket{book="btc_mxn",limit="max_order_btc",le="0.9"} 1
order_limit_utilization_ratio_bucket{book="btc_mxn",limit="max_order_btc",le="1"} 1
order_limit_utilization_ratio_bucket{book="btc_mxn",limit="max_order_btc",le="1.25"} 1
order_limit_utilization_ratio_bucket{book="btc_mxn",limit="max_order_btc",le="2"} 1
order_limit_utilization_ratio_bucket{book="btc_mxn",limit="max_order_btc",le="+Inf"} 1
order_limit_utilization_ratio_sum{book="btc_mxn",limit="max_order_btc"} 0.5
order_limit_utilization_ratio_count{book="btc_mxn",limit="max_order_btc"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "order_limit_utilization_ratio"); err != nil {
		t.Fatal(err)
	}
	if testutil.ToFloat64(m.sessionUtilization.WithLabelValues(LimitMaxDailyLoss)) != 0.4 ||
		testutil.ToFloat64(m.limit.WithLabelValues(SessionBook, LimitMaxDailyLoss)) != 500 {
		t.Fatal("session gauges wrong")
	}
	if n, err := testutil.GatherAndCount(reg, "signal_age_at_decision_seconds", "signal_to_order_latency_seconds",
		"order_price_deviation_bps"); err != nil || n != 3 {
		t.Fatalf("histogram series = %d, %v", n, err)
	}
}
