// Package metrics owns the Prometheus instrumentation for strategy-router.
//
// All metrics are registered against the default Prometheus registry so
// /metrics on the HTTP server exposes them without extra wiring.
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics is the bag of Prometheus metrics the router emits.
type Metrics struct {
	RegimeLabel       *prometheus.GaugeVec
	EvaluationsTotal  *prometheus.CounterVec
	EvaluationErrors  *prometheus.CounterVec
	SwitchesTotal     *prometheus.CounterVec
	BlockedTotal      *prometheus.CounterVec
	ActiveStrategy    *prometheus.GaugeVec
	EvaluationLatency *prometheus.HistogramVec
}

var (
	once     sync.Once
	instance *Metrics
)

// Get lazily constructs and registers metrics once per process.
func Get() *Metrics {
	once.Do(func() {
		instance = &Metrics{
			RegimeLabel: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: "strategy_router_regime",
				Help: "1 for the currently classified regime, 0 for the others.",
			}, []string{"book", "regime"}),

			EvaluationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "strategy_router_evaluations_total",
				Help: "Total number of router evaluation cycles.",
			}, []string{"book"}),
			EvaluationErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "strategy_router_evaluation_errors_total",
				Help: "Total number of router evaluation cycles that errored out.",
			}, []string{"book"}),

			SwitchesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "strategy_router_switches_total",
				Help: "Total strategy switches the router performed, labeled by from/to/regime.",
			}, []string{"book", "from", "to", "regime"}),

			BlockedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "strategy_router_blocked_total",
				Help: "Total times a switch was blocked, labeled by reason (has_position|cooldown|not_registered|dry_run).",
			}, []string{"book", "reason"}),

			ActiveStrategy: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: "strategy_router_active_strategy",
				Help: "1 for the strategy the router believes is currently active.",
			}, []string{"book", "strategy"}),

			EvaluationLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "strategy_router_evaluation_latency_ms",
				Help:    "Latency of a router evaluation cycle in milliseconds.",
				Buckets: []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
			}, []string{"book"}),
		}

		prometheus.MustRegister(
			instance.RegimeLabel,
			instance.EvaluationsTotal,
			instance.EvaluationErrors,
			instance.SwitchesTotal,
			instance.BlockedTotal,
			instance.ActiveStrategy,
			instance.EvaluationLatency,
		)
	})
	return instance
}

// SetRegime sets the gauge to 1 for the active regime and 0 for the others.
// Tracking all five labels (rather than a single labelled gauge) keeps the
// resulting time series easy to plot in Grafana.
func (m *Metrics) SetRegime(book, active string) {
	for _, regime := range []string{"low_vol_range", "trending_up", "trending_down", "high_vol", "neutral"} {
		v := 0.0
		if regime == active {
			v = 1.0
		}
		m.RegimeLabel.WithLabelValues(book, regime).Set(v)
	}
}

// SetActiveStrategy zeros previous strategies and sets the new one to 1.
func (m *Metrics) SetActiveStrategy(book, previous, active string) {
	if previous != "" && previous != active {
		m.ActiveStrategy.WithLabelValues(book, previous).Set(0)
	}
	if active != "" {
		m.ActiveStrategy.WithLabelValues(book, active).Set(1)
	}
}
