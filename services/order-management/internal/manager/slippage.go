package manager

import (
	"math"

	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
)

// Realized slippage per executed order (plan §6.4.7). When an order closes
// with fills (filled, or cancelled after a partial fill) its average fill
// price is compared once with the order's own price: the price the signal
// decided on and trading-engine sent. Together with trading-engine's
// order_price_deviation_bps (decision price vs the touch mid) this is the
// desk's implementation shortfall, excluding fees.

const (
	slippageRecordedMetaKey = "slippage_recorded"
	arrivalSlippageMetaKey  = "arrival_slippage_bps"
)

// SetRiskSeries registers the execution-quality series (optional; nil
// disables the slippage observation). Call once during start-up.
func (m *Manager) SetRiskSeries(s *metrics.RiskSeries) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.riskSeries = s
}

// markSlippage decides, before the order is persisted, whether this sync
// closes the order with fills for the first time. If so it stamps the order
// (so a re-sync never counts it twice) and returns true; call
// observeSlippage after the update succeeds. Caller holds m.mu.
func (m *Manager) markSlippage(order *models.Order, status models.OrderStatus) bool {
	if m.riskSeries == nil || order == nil || !(order.FilledAmount > 0) {
		return false
	}
	if status != models.OrderStatusFilled && status != models.OrderStatusCancelled {
		return false
	}
	if order.Metadata == nil {
		order.Metadata = make(map[string]interface{})
	}
	if done, _ := order.Metadata[slippageRecordedMetaKey].(bool); done {
		return false
	}
	bps, ok := metrics.SlippageBps(order.Side, order.Price, order.AveragePrice)
	if !ok {
		return false
	}
	order.Metadata[slippageRecordedMetaKey] = true
	order.Metadata[arrivalSlippageMetaKey] = math.Round(bps*100) / 100
	return true
}

// observeSlippage records the order marked by markSlippage.
func (m *Manager) observeSlippage(order *models.Order) {
	m.riskSeries.ObserveExecution(order.Book, order.Side, order.FilledAmount, order.Price, order.AveragePrice)
}
