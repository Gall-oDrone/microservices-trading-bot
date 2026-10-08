package managertest

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
)

// Plan §6.4.7: an order that closes with fills is observed once, against its
// own (decision) price; re-syncs of a closed order never count it again.
func TestSyncObservesArrivalSlippageOnce(t *testing.T) {
	ctx := context.Background()
	mgr := setupManager()
	reg := prometheus.NewRegistry()
	mgr.SetRiskSeries(metrics.NewRiskSeries(reg))

	const oid = "slip-buy-1"
	if _, err := mgr.RecordOrderPlaced(ctx, oid, "btc_mxn", "buy", 0.01, 1_000_000, "basic", ""); err != nil {
		t.Fatal(err)
	}
	// Half filled at 1,001,000, then all of it at a VWAP of 1,002,000:
	// 20 bps above the decision price.
	if err := mgr.SyncOrderFromBitso(ctx, oid, 0.005, 1_001_000, models.OrderStatusPartiallyFilled); err != nil {
		t.Fatal(err)
	}
	if _, ok := metricValue(t, reg, "order_filled_notional_quote_total", nil); ok {
		t.Fatal("a partial fill was observed before the order closed")
	}
	if err := mgr.SyncOrderFromBitso(ctx, oid, 0.01, 1_002_000, models.OrderStatusFilled); err != nil {
		t.Fatal(err)
	}
	// The sync job may see the closed order again: no second count.
	_ = mgr.SyncOrderFromBitso(ctx, oid, 0.01, 1_002_000, models.OrderStatusFilled)

	o, err := mgr.GetOrderByBitsoOrderID(ctx, oid)
	if err != nil {
		t.Fatal(err)
	}
	notional, _ := metricValue(t, reg, "order_filled_notional_quote_total", map[string]string{"book": "btc_mxn", "side": "buy"})
	if want := o.FilledAmount * o.AveragePrice; abs(notional-want) > 1e-6 {
		t.Fatalf("notional %v want %v (counted once)", notional, want)
	}
	wantBps := (o.AveragePrice - 1_000_000) / 1_000_000 * 1e4
	if v, _ := o.Metadata["arrival_slippage_bps"].(float64); abs(v-wantBps) > 0.01 || wantBps <= 0 {
		t.Fatalf("arrival_slippage_bps %v want %.2f (> 0: a buy above the decision price)", o.Metadata["arrival_slippage_bps"], wantBps)
	}
	adv, _ := metricValue(t, reg, "order_slippage_cost_quote_total", map[string]string{"direction": metrics.SlippageAdverse})
	if want := wantBps / 1e4 * notional; abs(adv-want) > 1e-6 {
		t.Fatalf("adverse cost %v want %v", adv, want)
	}
}

// A cancelled order with a partial fill is observed; one without fills is not.
func TestSyncObservesSlippageOnPartialCancel(t *testing.T) {
	ctx := context.Background()
	mgr := setupManager()
	reg := prometheus.NewRegistry()
	mgr.SetRiskSeries(metrics.NewRiskSeries(reg))

	if _, err := mgr.RecordOrderPlaced(ctx, "slip-sell-1", "btc_mxn", "sell", 0.01, 1_000_000, "basic", ""); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SyncOrderFromBitso(ctx, "slip-sell-1", 0.004, 1_001_000, models.OrderStatusPartiallyFilled); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SyncOrderFromBitso(ctx, "slip-sell-1", 0.004, 1_001_000, models.OrderStatusCancelled); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.RecordOrderPlaced(ctx, "slip-sell-2", "btc_usd", "sell", 0.01, 60_000, "basic", ""); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SyncOrderFromBitso(ctx, "slip-sell-2", 0, 0, models.OrderStatusCancelled); err != nil {
		t.Fatal(err)
	}
	// Sold 10 bps above the decision price: an improvement.
	imp, _ := metricValue(t, reg, "order_slippage_cost_quote_total",
		map[string]string{"book": "btc_mxn", "side": "sell", "direction": metrics.SlippageImprovement})
	if want := 0.004 * 1_001_000 * 10 / 1e4; abs(imp-want) > 1e-6 {
		t.Fatalf("improvement %v want %v", imp, want)
	}
	if _, ok := metricValue(t, reg, "order_filled_notional_quote_total", map[string]string{"book": "btc_usd"}); ok {
		t.Fatal("a cancel without fills was observed")
	}
}

// metricValue gathers reg and returns the counter/gauge value of the first
// series named name whose labels include want.
func metricValue(t *testing.T, reg *prometheus.Registry, name string, want map[string]string) (float64, bool) {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
	next:
		for _, m := range mf.GetMetric() {
			got := map[string]string{}
			for _, l := range m.GetLabel() {
				got[l.GetName()] = l.GetValue()
			}
			for k, v := range want {
				if got[k] != v {
					continue next
				}
			}
			return m.GetCounter().GetValue() + m.GetGauge().GetValue(), true
		}
	}
	return 0, false
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
