package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	sharedMetrics "bitso-trading-platform/shared/pkg/metrics"
)

func TestMetricsCollector_PrimeIntradayGauges_RegistersSeries(t *testing.T) {
	mc := NewMetricsCollector("test-prime-intraday")
	mc.PrimeIntradayGauges("MXN", "btc_mxn", "basic")

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	assertGaugeWithLabels(t, mfs, sharedMetrics.NameDailyRealizedPnL, map[string]string{
		sharedMetrics.LabelCurrency: "MXN",
	}, 0)

	assertGaugeWithLabels(t, mfs, sharedMetrics.NameTradesToday, map[string]string{
		sharedMetrics.LabelBook:     "btc_mxn",
		sharedMetrics.LabelStrategy: "basic",
	}, 0)

	assertGaugeWithLabels(t, mfs, sharedMetrics.NameWinsToday, map[string]string{
		sharedMetrics.LabelBook:     "btc_mxn",
		sharedMetrics.LabelStrategy: "basic",
	}, 0)

	assertGaugeWithLabels(t, mfs, sharedMetrics.NameLossesToday, map[string]string{
		sharedMetrics.LabelBook:     "btc_mxn",
		sharedMetrics.LabelStrategy: "basic",
	}, 0)
}

func assertGaugeWithLabels(t *testing.T, mfs []*dto.MetricFamily, name string, labels map[string]string, want float64) {
	t.Helper()
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.Metric {
			if !labelsMatch(m.Label, labels) {
				continue
			}
			if g := m.GetGauge(); g != nil {
				if got := g.GetValue(); got != want {
					t.Errorf("%s %v: want value %v, got %v", name, labels, want, got)
				}
				return
			}
		}
		t.Fatalf("metric %s with labels %v not found", name, labels)
	}
	t.Fatalf("metric family %q not found", name)
}

func labelsMatch(lps []*dto.LabelPair, want map[string]string) bool {
	if len(lps) != len(want) {
		return false
	}
	got := make(map[string]string, len(lps))
	for _, lp := range lps {
		got[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}
