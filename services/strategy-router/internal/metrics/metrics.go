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
	EvaluationsTotal  prometheus.Counter
	EvaluationErrors  prometheus.Counter
	SwitchesTotal     *prometheus.CounterVec // labels: from, to, regime
	BlockedTotal      *prometheus.CounterVec // labels: reason
	ActiveStrategy    *prometheus.GaugeVec   // labels: strategy
	EvaluationLatency prometheus.Histogram
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
			}, []string{"regime"}),

			EvaluationsTotal: prometheus.NewCounter(prometheus.CounterOpts{
				Name: "strategy_router_evaluations_total",
				Help: "Total number of router evaluation cycles.",
			}),
			EvaluationErrors: prometheus.NewCounter(prometheus.CounterOpts{
				Name: "strategy_router_evaluation_errors_total",
				Help: "Total number of router evaluation cycles that errored out.",
			}),

			SwitchesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "strategy_router_switches_total",
				Help: "Total strategy switches the router performed, labeled by from/to/regime.",
			}, []string{"from", "to", "regime"}),

			BlockedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "strategy_router_blocked_total",
				Help: "Total times a switch was blocked, labeled by reason (has_position|cooldown|not_registered|dry_run).",
			}, []string{"reason"}),

			ActiveStrategy: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: "strategy_router_active_strategy",
				Help: "1 for the strategy the router believes is currently active.",
			}, []string{"strategy"}),

			EvaluationLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
				Name:    "strategy_router_evaluation_latency_ms",
				Help:    "Latency of a router evaluation cycle in milliseconds.",
				Buckets: []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
			}),
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
func (m *Metrics) SetRegime(active string) {
	for _, regime := range []string{"low_vol_range", "trending_up", "trending_down", "high_vol", "neutral"} {
		v := 0.0
		if regime == active {
			v = 1.0
		}
		m.RegimeLabel.WithLabelValues(regime).Set(v)
	}
}

// SetActiveStrategy zeros previous strategies and sets the new one to 1.
// `previous` may be empty (e.g. on first evaluation) — we still set the
// new one. Pass empty `active` to mark "no strategy running" without
// clobbering any previous label.
func (m *Metrics) SetActiveStrategy(previous, active string) {
	if previous != "" && previous != active {
		m.ActiveStrategy.WithLabelValues(previous).Set(0)
	}
	if active != "" {
		m.ActiveStrategy.WithLabelValues(active).Set(1)
	}
}
