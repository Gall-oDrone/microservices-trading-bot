package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// MetricsCollector collects and exposes Prometheus metrics
type MetricsCollector struct {
	// Backtest metrics
	backtestsCreated   prometheus.Counter
	backtestsCompleted *prometheus.CounterVec
	backtestsFailed    *prometheus.CounterVec
	backtestDuration   *prometheus.HistogramVec
	activeBacktests    prometheus.Gauge

		// Data processing metrics
		eventsProcessed  *prometheus.CounterVec
		dataLoadDuration *prometheus.HistogramVec

		// Data fetch errors (Phase 2): by source (market_data, file)
		dataFetchErrorsTotal *prometheus.CounterVec

		// Performance metrics
	metricsCalculationTime *prometheus.HistogramVec

	// System metrics
	serviceUptime prometheus.Gauge
	serviceHealth *prometheus.GaugeVec
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(serviceName string) *MetricsCollector {
	return &MetricsCollector{
		// Backtest metrics
		backtestsCreated: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: serviceName,
			Name:      "backtests_created_total",
			Help:      "Total number of backtests created",
		}),
		backtestsCompleted: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: serviceName,
			Name:      "backtests_completed_total",
			Help:      "Total number of backtests completed by status",
		}, []string{"status"}),
		backtestsFailed: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: serviceName,
			Name:      "backtests_failed_total",
			Help:      "Total number of backtests failed by reason",
		}, []string{"reason"}),
		backtestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: serviceName,
			Name:      "backtest_duration_seconds",
			Help:      "Backtest execution duration in seconds",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~1024s
		}, []string{"status"}),
		activeBacktests: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: serviceName,
			Name:      "active_backtests",
			Help:      "Number of currently running backtests",
		}),

		// Data processing metrics
		eventsProcessed: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: serviceName,
			Name:      "events_processed_total",
			Help:      "Total number of market events processed",
		}, []string{"event_type"}),
		dataLoadDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: serviceName,
			Name:      "data_load_duration_seconds",
			Help:      "Data loading duration in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 10), // 0.1s to ~100s
		}, []string{"source"}),

		dataFetchErrorsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: serviceName,
			Name:      "data_fetch_errors_total",
			Help:      "Total data fetch errors by source (market_data, file)",
		}, []string{"source"}),

		// Performance metrics
		metricsCalculationTime: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: serviceName,
			Name:      "metrics_calculation_duration_seconds",
			Help:      "Metrics calculation duration in seconds",
			Buckets:   prometheus.DefBuckets,
		}, []string{"metric"}),

		// System metrics
		serviceUptime: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: serviceName,
			Name:      "uptime_seconds",
			Help:      "Service uptime in seconds",
		}),
		serviceHealth: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: serviceName,
			Name:      "health",
			Help:      "Service health status (1 = healthy, 0 = unhealthy)",
		}, []string{"component"}),
	}
}

// Backtest metrics

// RecordBacktestCreated increments the created backtests counter
func (m *MetricsCollector) RecordBacktestCreated() {
	m.backtestsCreated.Inc()
}

// RecordBacktestCompleted records a completed backtest
func (m *MetricsCollector) RecordBacktestCompleted(status string, duration time.Duration) {
	m.backtestsCompleted.WithLabelValues(status).Inc()
	m.backtestDuration.WithLabelValues(status).Observe(duration.Seconds())
}

// RecordBacktestFailed records a failed backtest
func (m *MetricsCollector) RecordBacktestFailed(reason string) {
	m.backtestsFailed.WithLabelValues(reason).Inc()
}

// RecordActiveBacktests sets the number of active backtests
func (m *MetricsCollector) RecordActiveBacktests(count int) {
	m.activeBacktests.Set(float64(count))
}

// Data processing metrics

// RecordEventsProcessed records processed events
func (m *MetricsCollector) RecordEventsProcessed(eventType string, count int) {
	m.eventsProcessed.WithLabelValues(eventType).Add(float64(count))
}

// RecordDataLoadDuration records data loading duration
func (m *MetricsCollector) RecordDataLoadDuration(source string, duration time.Duration) {
	m.dataLoadDuration.WithLabelValues(source).Observe(duration.Seconds())
}

// RecordDataFetchError records a data fetch error (Phase 2). source: market_data, file.
func (m *MetricsCollector) RecordDataFetchError(source string) {
	m.dataFetchErrorsTotal.WithLabelValues(source).Inc()
}

// Performance metrics

// RecordMetricsCalculation records metrics calculation time
func (m *MetricsCollector) RecordMetricsCalculation(metric string, duration time.Duration) {
	m.metricsCalculationTime.WithLabelValues(metric).Observe(duration.Seconds())
}

// System metrics

// RecordServiceUptime records service uptime
func (m *MetricsCollector) RecordServiceUptime(uptime time.Duration) {
	m.serviceUptime.Set(uptime.Seconds())
}

// RecordServiceHealth records service health status
func (m *MetricsCollector) RecordServiceHealth(healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	m.serviceHealth.WithLabelValues("service").Set(value)
}

// RecordComponentHealth records component health status
func (m *MetricsCollector) RecordComponentHealth(component string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	m.serviceHealth.WithLabelValues(component).Set(value)
}
