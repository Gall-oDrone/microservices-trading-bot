package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	sharedMetrics "bitso-trading-platform/shared/pkg/metrics"
)

// MetricsCollector collects and exposes Prometheus metrics
type MetricsCollector struct {
	// Order metrics
	ordersCreated   *prometheus.CounterVec
	ordersFilled    *prometheus.CounterVec
	ordersCancelled *prometheus.CounterVec
	ordersRejected  *prometheus.CounterVec
	ordersActive    *prometheus.GaugeVec
	orderDuration   *prometheus.HistogramVec

	// Validation metrics
	validations        *prometheus.CounterVec
	validationDuration *prometheus.HistogramVec

	// Risk metrics
	riskChecks     *prometheus.CounterVec
	riskViolations *prometheus.CounterVec

	// Repository metrics
	repoOperations *prometheus.CounterVec
	repoDuration   *prometheus.HistogramVec

	// Publisher metrics
	eventsPublished *prometheus.CounterVec
	eventsFailed    *prometheus.CounterVec

	// Kafka metrics
	messagesConsumed *prometheus.CounterVec
	messagesProduced *prometheus.CounterVec
	kafkaLag         *prometheus.GaugeVec

	// HTTP metrics
	httpRequestsTotal    *prometheus.CounterVec
	httpRequestDuration  *prometheus.HistogramVec
	httpRequestsInFlight *prometheus.GaugeVec

	// System metrics
	serviceUptime prometheus.Gauge
	serviceHealth *prometheus.GaugeVec

	// Bitso sync job (Phase 2)
	bitsoSyncAttemptsTotal        prometheus.Counter
	bitsoSyncErrorsTotal          prometheus.Counter
	bitsoSyncLastSuccessTimestamp prometheus.Gauge

	// Session risk endpoint (Phase 2)
	sessionRiskRequestsTotal     prometheus.Counter
	sessionRiskRequestErrorsTotal prometheus.Counter

	// Intraday / P&L metrics (financial production standard)
	dailyRealizedPnL   *prometheus.GaugeVec
	dailyUnrealizedPnL *prometheus.GaugeVec
	drawdownPercent    *prometheus.GaugeVec
	drawdownAbsolute   *prometheus.GaugeVec
	peakEquity         *prometheus.GaugeVec
	currentEquity      *prometheus.GaugeVec
	tradesToday        *prometheus.GaugeVec
	winsToday          *prometheus.GaugeVec
	lossesToday        *prometheus.GaugeVec
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(serviceName string) *MetricsCollector {
	mc := &MetricsCollector{
		// Order metrics
		ordersCreated: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orders_created_total",
				Help: "Total number of orders created",
			},
			[]string{"book", "strategy"},
		),
		ordersFilled: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orders_filled_total",
				Help: "Total number of orders filled",
			},
			[]string{"book", "strategy"},
		),
		ordersCancelled: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orders_cancelled_total",
				Help: "Total number of orders cancelled",
			},
			[]string{"book", "strategy", "reason"},
		),
		ordersRejected: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orders_rejected_total",
				Help: "Total number of orders rejected",
			},
			[]string{"book", "strategy", "reason"},
		),
		ordersActive: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "orders_active",
				Help: "Number of active orders",
			},
			[]string{"book"},
		),
		orderDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "order_processing_duration_seconds",
				Help:    "Order processing duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"stage"},
		),

		// Validation metrics
		validations: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "validations_total",
				Help: "Total number of validations performed",
			},
			[]string{"type", "result"},
		),
		validationDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "validation_duration_seconds",
				Help:    "Validation duration in seconds",
				Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
			},
			[]string{"type"},
		),

		// Risk metrics
		riskChecks: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "risk_checks_total",
				Help: "Total number of risk checks performed",
			},
			[]string{"check_type", "result"},
		),
		riskViolations: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "risk_violations_total",
				Help: "Total number of risk violations detected",
			},
			[]string{"violation_type"},
		),

		// Repository metrics
		repoOperations: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "repository_operations_total",
				Help: "Total number of repository operations",
			},
			[]string{"operation", "result"},
		),
		repoDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "repository_duration_seconds",
				Help:    "Repository operation duration in seconds",
				Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
			},
			[]string{"operation"},
		),

		// Publisher metrics
		eventsPublished: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "events_published_total",
				Help: "Total number of events published",
			},
			[]string{"event_type"},
		),
		eventsFailed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "events_failed_total",
				Help: "Total number of events failed to publish",
			},
			[]string{"event_type", "reason"},
		),

		// Kafka metrics
		messagesConsumed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kafka_messages_consumed_total",
				Help: "Total number of Kafka messages consumed",
			},
			[]string{"topic"},
		),
		messagesProduced: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kafka_messages_produced_total",
				Help: "Total number of Kafka messages produced",
			},
			[]string{"topic"},
		),
		kafkaLag: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kafka_consumer_lag",
				Help: "Kafka consumer lag",
			},
			[]string{"topic", "partition"},
		),

		// HTTP metrics
		httpRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),
		httpRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			},
			[]string{"method", "path"},
		),
		httpRequestsInFlight: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "http_requests_in_flight",
				Help: "Number of HTTP requests currently being served",
			},
			[]string{"method", "path"},
		),

		// System metrics
		serviceUptime: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "service_uptime_seconds",
				Help: "Service uptime in seconds",
			},
		),
		serviceHealth: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "service_health",
				Help: "Service health status (1 = healthy, 0 = unhealthy)",
			},
			[]string{"status"},
		),

		// Bitso sync job (Phase 2)
		bitsoSyncAttemptsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bitso_sync_attempts_total",
			Help: "Total number of Bitso sync job attempts",
		}),
		bitsoSyncErrorsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bitso_sync_errors_total",
			Help: "Total number of Bitso sync job errors",
		}),
		bitsoSyncLastSuccessTimestamp: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "bitso_sync_last_success_timestamp_seconds",
			Help: "Unix timestamp of last successful Bitso sync; alert if stale",
		}),

		// Session risk endpoint (Phase 2)
		sessionRiskRequestsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "session_risk_requests_total",
			Help: "Total number of GET /api/v1/risk/session requests",
		}),
		sessionRiskRequestErrorsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "session_risk_request_errors_total",
			Help: "Total number of session risk request errors (4xx/5xx or handler errors)",
		}),

		// Intraday / P&L metrics — currency units (e.g. MXN); use decimal in aggregator, float for export
		dailyRealizedPnL: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: sharedMetrics.NameDailyRealizedPnL,
				Help: "Cumulative realized P&L for the current session (currency units)",
			},
			[]string{sharedMetrics.LabelCurrency},
		),
		dailyUnrealizedPnL: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: sharedMetrics.NameDailyUnrealizedPnL,
				Help: "Current unrealized P&L for the session (currency units)",
			},
			[]string{sharedMetrics.LabelCurrency},
		),
		drawdownPercent: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: sharedMetrics.NameDrawdownPercent,
				Help: "Current drawdown as percentage of peak equity (0-100)",
			},
			[]string{sharedMetrics.LabelCurrency},
		),
		drawdownAbsolute: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: sharedMetrics.NameDrawdownAbsolute,
				Help: "Current drawdown in currency units (peak - current equity)",
			},
			[]string{sharedMetrics.LabelCurrency},
		),
		peakEquity: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: sharedMetrics.NamePeakEquity,
				Help: "Peak equity observed in the session (currency units)",
			},
			[]string{sharedMetrics.LabelCurrency},
		),
		currentEquity: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: sharedMetrics.NameCurrentEquity,
				Help: "Current equity (currency units)",
			},
			[]string{sharedMetrics.LabelCurrency},
		),
		tradesToday: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: sharedMetrics.NameTradesToday,
				Help: "Total trades closed in the current session (gauge; resets at session boundary)",
			},
			[]string{sharedMetrics.LabelBook, sharedMetrics.LabelStrategy},
		),
		winsToday: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: sharedMetrics.NameWinsToday,
				Help: "Winning trades closed in the current session (realized P&L > 0)",
			},
			[]string{sharedMetrics.LabelBook, sharedMetrics.LabelStrategy},
		),
		lossesToday: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: sharedMetrics.NameLossesToday,
				Help: "Losing trades closed in the current session (realized P&L <= 0)",
			},
			[]string{sharedMetrics.LabelBook, sharedMetrics.LabelStrategy},
		),
	}

	return mc
}

// Order metrics

func (mc *MetricsCollector) RecordOrderCreated(book, strategy string) {
	mc.ordersCreated.WithLabelValues(book, strategy).Inc()
}

func (mc *MetricsCollector) RecordOrderFilled(book, strategy string) {
	mc.ordersFilled.WithLabelValues(book, strategy).Inc()
}

func (mc *MetricsCollector) RecordOrderCancelled(book, strategy, reason string) {
	mc.ordersCancelled.WithLabelValues(book, strategy, reason).Inc()
}

func (mc *MetricsCollector) RecordOrderRejected(book, strategy, reason string) {
	mc.ordersRejected.WithLabelValues(book, strategy, reason).Inc()
}

func (mc *MetricsCollector) SetActiveOrders(book string, count float64) {
	mc.ordersActive.WithLabelValues(book).Set(count)
}

func (mc *MetricsCollector) RecordOrderDuration(stage string, duration time.Duration) {
	mc.orderDuration.WithLabelValues(stage).Observe(duration.Seconds())
}

// Validation metrics

func (mc *MetricsCollector) RecordValidation(validationType, result string) {
	mc.validations.WithLabelValues(validationType, result).Inc()
}

func (mc *MetricsCollector) RecordValidationDuration(validationType string, duration time.Duration) {
	mc.validationDuration.WithLabelValues(validationType).Observe(duration.Seconds())
}

// Risk metrics

func (mc *MetricsCollector) RecordRiskCheck(checkType, result string) {
	mc.riskChecks.WithLabelValues(checkType, result).Inc()
}

func (mc *MetricsCollector) RecordRiskViolation(violationType string) {
	mc.riskViolations.WithLabelValues(violationType).Inc()
}

// Repository metrics

func (mc *MetricsCollector) RecordRepositoryOperation(operation, result string, duration time.Duration) {
	mc.repoOperations.WithLabelValues(operation, result).Inc()
	mc.repoDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// Publisher metrics

func (mc *MetricsCollector) RecordEventPublished(eventType string) {
	mc.eventsPublished.WithLabelValues(eventType).Inc()
}

func (mc *MetricsCollector) RecordEventFailed(eventType, reason string) {
	mc.eventsFailed.WithLabelValues(eventType, reason).Inc()
}

// Kafka metrics

func (mc *MetricsCollector) RecordMessageConsumed(topic string) {
	mc.messagesConsumed.WithLabelValues(topic).Inc()
}

func (mc *MetricsCollector) RecordMessageProduced(topic string) {
	mc.messagesProduced.WithLabelValues(topic).Inc()
}

func (mc *MetricsCollector) SetKafkaLag(topic, partition string, lag float64) {
	mc.kafkaLag.WithLabelValues(topic, partition).Set(lag)
}

// HTTP metrics

func (mc *MetricsCollector) RecordHTTPRequest(method, path, status string) {
	mc.httpRequestsTotal.WithLabelValues(method, path, status).Inc()
}

func (mc *MetricsCollector) RecordHTTPDuration(method, path string, duration time.Duration) {
	mc.httpRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}

func (mc *MetricsCollector) IncHTTPInFlight(method, path string) {
	mc.httpRequestsInFlight.WithLabelValues(method, path).Inc()
}

func (mc *MetricsCollector) DecHTTPInFlight(method, path string) {
	mc.httpRequestsInFlight.WithLabelValues(method, path).Dec()
}

// System metrics

func (mc *MetricsCollector) RecordServiceUptime(uptime time.Duration) {
	mc.serviceUptime.Set(uptime.Seconds())
}

func (mc *MetricsCollector) RecordServiceHealth(healthy bool) {
	if healthy {
		mc.serviceHealth.WithLabelValues("healthy").Set(1)
		mc.serviceHealth.WithLabelValues("unhealthy").Set(0)
	} else {
		mc.serviceHealth.WithLabelValues("healthy").Set(0)
		mc.serviceHealth.WithLabelValues("unhealthy").Set(1)
	}
}

// Bitso sync (Phase 2)

func (mc *MetricsCollector) RecordBitsoSyncAttempt() {
	mc.bitsoSyncAttemptsTotal.Inc()
}

func (mc *MetricsCollector) RecordBitsoSyncError() {
	mc.bitsoSyncErrorsTotal.Inc()
}

func (mc *MetricsCollector) SetBitsoSyncLastSuccessTimestamp(ts float64) {
	mc.bitsoSyncLastSuccessTimestamp.Set(ts)
}

// Session risk endpoint (Phase 2)

func (mc *MetricsCollector) RecordSessionRiskRequest() {
	mc.sessionRiskRequestsTotal.Inc()
}

func (mc *MetricsCollector) RecordSessionRiskRequestError() {
	mc.sessionRiskRequestErrorsTotal.Inc()
}

// IntradayMetricsWriter writes intraday/P&L gauges and counters (used by IntradayAggregator).
// Implemented by MetricsCollector for production; mock for tests.
type IntradayMetricsWriter interface {
	SetDailyRealizedPnL(currency string, value float64)
	SetDailyUnrealizedPnL(currency string, value float64)
	SetDrawdownPercent(currency string, percent float64)
	SetDrawdownAbsolute(currency string, value float64)
	SetPeakEquity(currency string, value float64)
	SetCurrentEquity(currency string, value float64)
	SetTradesToday(book, strategy string, count float64)
	SetWinsToday(book, strategy string, count float64)
	SetLossesToday(book, strategy string, count float64)
}

// Ensure MetricsCollector implements IntradayMetricsWriter.
var _ IntradayMetricsWriter = (*MetricsCollector)(nil)

func (mc *MetricsCollector) SetDailyRealizedPnL(currency string, value float64) {
	mc.dailyRealizedPnL.WithLabelValues(currency).Set(value)
}

func (mc *MetricsCollector) SetDailyUnrealizedPnL(currency string, value float64) {
	mc.dailyUnrealizedPnL.WithLabelValues(currency).Set(value)
}

func (mc *MetricsCollector) SetDrawdownPercent(currency string, percent float64) {
	mc.drawdownPercent.WithLabelValues(currency).Set(percent)
}

func (mc *MetricsCollector) SetDrawdownAbsolute(currency string, value float64) {
	mc.drawdownAbsolute.WithLabelValues(currency).Set(value)
}

func (mc *MetricsCollector) SetPeakEquity(currency string, value float64) {
	mc.peakEquity.WithLabelValues(currency).Set(value)
}

func (mc *MetricsCollector) SetCurrentEquity(currency string, value float64) {
	mc.currentEquity.WithLabelValues(currency).Set(value)
}

func (mc *MetricsCollector) SetTradesToday(book, strategy string, count float64) {
	mc.tradesToday.WithLabelValues(book, strategy).Set(count)
}

func (mc *MetricsCollector) SetWinsToday(book, strategy string, count float64) {
	mc.winsToday.WithLabelValues(book, strategy).Set(count)
}

func (mc *MetricsCollector) SetLossesToday(book, strategy string, count float64) {
	mc.lossesToday.WithLabelValues(book, strategy).Set(count)
}

// PrimeIntradayGauges registers initial label combinations so /metrics and Grafana show 0 instead of "no data"
// before the first trade or position feed. Use the same currency/book/strategy as RecordTradeClosed and feeds.
func (mc *MetricsCollector) PrimeIntradayGauges(currency, book, strategy string) {
	if currency == "" {
		currency = "MXN"
	}
	if book == "" {
		book = "btc_mxn"
	}
	if strategy == "" {
		strategy = "basic"
	}
	mc.dailyRealizedPnL.WithLabelValues(currency).Set(0)
	mc.dailyUnrealizedPnL.WithLabelValues(currency).Set(0)
	mc.drawdownPercent.WithLabelValues(currency).Set(0)
	mc.drawdownAbsolute.WithLabelValues(currency).Set(0)
	mc.peakEquity.WithLabelValues(currency).Set(0)
	mc.currentEquity.WithLabelValues(currency).Set(0)
	mc.tradesToday.WithLabelValues(book, strategy).Set(0)
	mc.winsToday.WithLabelValues(book, strategy).Set(0)
	mc.lossesToday.WithLabelValues(book, strategy).Set(0)
}
