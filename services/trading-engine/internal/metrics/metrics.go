package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Bounded failure reasons for orders_failed_total (keeps cardinality low).
const (
	ReasonValidation        = "validation"
	ReasonBitsoAPI          = "bitso_api"
	ReasonSessionRisk       = "session_risk"
	ReasonTickerFetch       = "ticker_fetch"
	ReasonPreTradeValidation = "pretrade_validation"
)

// Bounded reasons for signals_dropped_total.
const (
	DropReasonChannelFull   = "channel_full"
	DropReasonTimeout       = "timeout"
	DropReasonContextCancel = "context_cancelled"
)

// Outcome for signals_processed_total.
const (
	OutcomeSuccess = "success"
	OutcomeFailed  = "failed"
)

// Session risk check result for session_risk_checks_total.
const (
	SessionRiskAllowed   = "allowed"
	SessionRiskRejected  = "rejected"
)

// Pre-trade validation result for pretrade_validation_checks_total.
const (
	PreTradeApproved = "approved"
	PreTradeRejected = "rejected"
	PreTradeError    = "error"
)

// Collector exposes Prometheus metrics for the trading engine.
type Collector struct {
	ordersExecutedTotal   *prometheus.CounterVec
	ordersFailedTotal     *prometheus.CounterVec
	orderExecutionDuration *prometheus.HistogramVec
	bitsoAvailableBalance *prometheus.GaugeVec

	signalsReceivedTotal         prometheus.Counter
	signalsProcessedTotal        *prometheus.CounterVec
	signalsDroppedTotal          *prometheus.CounterVec
	signalProcessingDuration     prometheus.Histogram

	bitsoBalanceFetchErrorsTotal     prometheus.Counter
	bitsoBalanceLastSuccessTimestamp prometheus.Gauge

	sessionRiskChecksTotal    *prometheus.CounterVec
	sessionRiskRejectionsTotal prometheus.Counter

	preTradeValidationChecksTotal *prometheus.CounterVec
	preTradeValidationRejectionsTotal prometheus.Counter

	kafkaMessagesConsumedTotal     *prometheus.CounterVec
	kafkaConsumerErrorsTotal       prometheus.Counter
	orderPlacedEventsPublishedTotal prometheus.Counter
	orderPlacedEventsPublishErrorsTotal prometheus.Counter

	engineState     prometheus.Gauge
	tradingEngineDryRun prometheus.Gauge
	healthCheckFailuresTotal prometheus.Counter
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
		ordersFailedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orders_failed_total",
				Help: "Total number of order execution failures by reason",
			},
			[]string{"book", "strategy", "reason"},
		),
		orderExecutionDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "order_execution_duration_seconds",
				Help:    "PlaceOrder latency in seconds",
				Buckets: prometheus.DefBuckets,
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

		signalsReceivedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "signals_received_total",
			Help: "Total number of trade signals consumed from Kafka",
		}),
		signalsProcessedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "signals_processed_total",
				Help: "Total number of signals processed by outcome",
			},
			[]string{"book", "strategy", "outcome"},
		),
		signalsDroppedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "signals_dropped_total",
				Help: "Total number of signals dropped (channel full, timeout, context cancelled)",
			},
			[]string{"reason"},
		),
		signalProcessingDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "signal_processing_duration_seconds",
			Help:    "Time from signal dequeue to decision (receive to decision latency)",
			Buckets: prometheus.DefBuckets,
		}),

		bitsoBalanceFetchErrorsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bitso_balance_fetch_errors_total",
			Help: "Total number of Bitso balance API fetch failures",
		}),
		bitsoBalanceLastSuccessTimestamp: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "bitso_balance_last_success_timestamp_seconds",
			Help: "Unix timestamp of last successful balance fetch; alert if stale",
		}),

		sessionRiskChecksTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "session_risk_checks_total",
				Help: "Total session risk checks by result (allowed, rejected)",
			},
			[]string{"result"},
		),
		sessionRiskRejectionsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "session_risk_rejections_total",
			Help: "Total orders blocked by daily loss or drawdown limits",
		}),

		preTradeValidationChecksTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "pretrade_validation_checks_total",
				Help: "Total pre-trade validation checks by result (approved, rejected, error)",
			},
			[]string{"result"},
		),
		preTradeValidationRejectionsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "pretrade_validation_rejections_total",
			Help: "Total orders rejected by pre-trade validation from order-management",
		}),

		kafkaMessagesConsumedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kafka_messages_consumed_total",
				Help: "Total Kafka messages consumed by topic",
			},
			[]string{"topic"},
		),
		kafkaConsumerErrorsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "kafka_consumer_errors_total",
			Help: "Total Kafka consume errors",
		}),
		orderPlacedEventsPublishedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "order_placed_events_published_total",
			Help: "Total order-placed events published to Kafka for order-management sync",
		}),
		orderPlacedEventsPublishErrorsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "order_placed_events_publish_errors_total",
			Help: "Total order-placed event publish failures",
		}),

		engineState: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "engine_state",
			Help: "Engine state: 0=stopped, 1=initializing, 2=running",
		}),
		tradingEngineDryRun: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "trading_engine_dry_run",
			Help: "1 if dry-run mode, 0 if live",
		}),
		healthCheckFailuresTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "health_check_failures_total",
			Help: "Total Bitso/Redis health check failures",
		}),
	}
}

// RecordOrderExecuted increments the orders_executed_total counter.
func (c *Collector) RecordOrderExecuted(book, strategy string) {
	c.ordersExecutedTotal.WithLabelValues(book, strategy).Inc()
}

// RecordOrderFailed increments orders_failed_total with bounded reason (validation, bitso_api, session_risk, ticker_fetch).
func (c *Collector) RecordOrderFailed(book, strategy, reason string) {
	c.ordersFailedTotal.WithLabelValues(book, strategy, reason).Inc()
}

// ObserveOrderExecutionDuration records order placement latency.
func (c *Collector) ObserveOrderExecutionDuration(book, strategy string, d time.Duration) {
	c.orderExecutionDuration.WithLabelValues(book, strategy).Observe(d.Seconds())
}

// RecordBalances sets bitso_available_balance for each currency.
// Pass a map of currency code -> available amount (e.g. "MXN" -> 1000.50).
func (c *Collector) RecordBalances(currencyToAvailable map[string]float64) {
	for currency, available := range currencyToAvailable {
		c.bitsoAvailableBalance.WithLabelValues(currency).Set(available)
	}
}

// RecordSignalReceived increments signals_received_total (on consume from Kafka).
func (c *Collector) RecordSignalReceived() {
	c.signalsReceivedTotal.Inc()
}

// RecordSignalsProcessed increments signals_processed_total (outcome: success, failed).
func (c *Collector) RecordSignalsProcessed(book, strategy, outcome string) {
	c.signalsProcessedTotal.WithLabelValues(book, strategy, outcome).Inc()
}

// RecordSignalsDropped increments signals_dropped_total (reason: channel_full, timeout, context_cancelled).
func (c *Collector) RecordSignalsDropped(reason string) {
	c.signalsDroppedTotal.WithLabelValues(reason).Inc()
}

// ObserveSignalProcessingDuration records receive-to-decision latency.
func (c *Collector) ObserveSignalProcessingDuration(d time.Duration) {
	c.signalProcessingDuration.Observe(d.Seconds())
}

// RecordBalanceFetchError increments bitso_balance_fetch_errors_total.
func (c *Collector) RecordBalanceFetchError() {
	c.bitsoBalanceFetchErrorsTotal.Inc()
}

// SetBalanceLastSuccessTimestamp sets Unix time of last successful balance fetch.
func (c *Collector) SetBalanceLastSuccessTimestamp(ts float64) {
	c.bitsoBalanceLastSuccessTimestamp.Set(ts)
}

// RecordSessionRiskCheck records session_risk_checks_total (result: allowed, rejected).
func (c *Collector) RecordSessionRiskCheck(result string) {
	c.sessionRiskChecksTotal.WithLabelValues(result).Inc()
}

// RecordSessionRiskRejection increments session_risk_rejections_total.
func (c *Collector) RecordSessionRiskRejection() {
	c.sessionRiskRejectionsTotal.Inc()
}

// RecordPreTradeValidation records pretrade_validation_checks_total (result: approved, rejected, error).
func (c *Collector) RecordPreTradeValidation(result string) {
	c.preTradeValidationChecksTotal.WithLabelValues(result).Inc()
}

// RecordPreTradeRejection increments pretrade_validation_rejections_total.
func (c *Collector) RecordPreTradeRejection() {
	c.preTradeValidationRejectionsTotal.Inc()
}

// RecordKafkaMessageConsumed increments kafka_messages_consumed_total by topic.
func (c *Collector) RecordKafkaMessageConsumed(topic string) {
	c.kafkaMessagesConsumedTotal.WithLabelValues(topic).Inc()
}

// RecordKafkaConsumerError increments kafka_consumer_errors_total.
func (c *Collector) RecordKafkaConsumerError() {
	c.kafkaConsumerErrorsTotal.Inc()
}

// RecordOrderPlacedPublished increments order_placed_events_published_total.
func (c *Collector) RecordOrderPlacedPublished() {
	c.orderPlacedEventsPublishedTotal.Inc()
}

// RecordOrderPlacedPublishError increments order_placed_events_publish_errors_total.
func (c *Collector) RecordOrderPlacedPublishError() {
	c.orderPlacedEventsPublishErrorsTotal.Inc()
}

// SetEngineState sets engine_state (0=stopped, 1=initializing, 2=running).
func (c *Collector) SetEngineState(state float64) {
	c.engineState.Set(state)
}

// SetDryRun sets trading_engine_dry_run (1=dry-run, 0=live).
func (c *Collector) SetDryRun(dryRun bool) {
	if dryRun {
		c.tradingEngineDryRun.Set(1)
	} else {
		c.tradingEngineDryRun.Set(0)
	}
}

// RecordHealthCheckFailure increments health_check_failures_total.
func (c *Collector) RecordHealthCheckFailure() {
	c.healthCheckFailuresTotal.Inc()
}

// Handler returns the HTTP handler for /metrics.
func (c *Collector) Handler() http.Handler {
	return promhttp.Handler()
}
