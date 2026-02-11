package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Collector exposes Prometheus metrics for the trading engine.
type Collector struct {
	ordersExecutedTotal  *prometheus.CounterVec
	bitsoAvailableBalance *prometheus.GaugeVec
}

// NewCollector creates a new metrics collector.
func NewCollector() *Collector {
	return &Collector{
		ordersExecutedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orders_executed_total",
				Help: "Total number of orders successfully executed (placed on exchange)",
			},
			[]string{"book", "strategy"},
		),
		bitsoAvailableBalance: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "bitso_available_balance",
				Help: "Available balance per currency from Bitso (quote currency for trading)",
			},
			[]string{"currency"},
		),
	}
}

// RecordOrderExecuted increments the orders_executed_total counter.
func (c *Collector) RecordOrderExecuted(book, strategy string) {
	c.ordersExecutedTotal.WithLabelValues(book, strategy).Inc()
}

// RecordBalances sets bitso_available_balance for each currency.
// Pass a map of currency code -> available amount (e.g. "MXN" -> 1000.50).
func (c *Collector) RecordBalances(currencyToAvailable map[string]float64) {
	for currency, available := range currencyToAvailable {
		c.bitsoAvailableBalance.WithLabelValues(currency).Set(available)
	}
}

// Handler returns the HTTP handler for /metrics.
func (c *Collector) Handler() http.Handler {
	return promhttp.Handler()
}
