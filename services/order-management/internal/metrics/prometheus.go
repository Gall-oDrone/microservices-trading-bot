package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
