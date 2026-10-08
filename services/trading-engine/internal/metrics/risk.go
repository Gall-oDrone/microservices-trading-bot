package metrics

import (
	"math"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Trading-risk operations metrics (plan §6.4.4): the limit, halt and
// execution-quality series a trading desk's risk function watches — kill
// switch state, which policy is live, every limit's value, how close each
// order comes to its limits, why orders are blocked, how stale a signal is
// when it is acted on, and how far the order price is from the market.
//
// RiskMetrics registers on the Registerer it is given (prometheus.Default-
// Registerer in main, a fresh registry in tests), so it is independent of
// Collector's promauto series.

// Bounded results for pretrade_policy_checks_total.
const (
	PolicyAllowed = "allowed"
	PolicyBlocked = "blocked"
	PolicyError   = "error" // halt file unreadable/invalid or position unknown (fail closed)
)

// Bounded limit names for trading_risk_limit and order_limit_utilization.
const (
	LimitMaxOrderBTC      = "max_order_btc"
	LimitMaxOrderNotional = "max_order_notional"
	LimitMaxPositionBTC   = "max_position_btc"
	LimitMaxPriceDevBps   = "max_price_deviation_bps"
	LimitMaxDailyLoss     = "max_daily_loss"
	LimitMaxDrawdownPct   = "max_drawdown_pct"
)

// SessionBook is the book label used for session-wide limits.
const SessionBook = "session"

// RiskMetrics holds the trading-risk series.
type RiskMetrics struct {
	haltActive         prometheus.Gauge
	haltFilesInvalid   prometheus.Gauge
	haltFilesConfig    prometheus.Gauge
	haltLastCheck      prometheus.Gauge
	policyInfo         *prometheus.GaugeVec
	limit              *prometheus.GaugeVec
	policyChecks       *prometheus.CounterVec
	policyRejections   *prometheus.CounterVec
	limitUtilization   *prometheus.HistogramVec
	priceDeviationBps  *prometheus.HistogramVec
	signalAge          *prometheus.HistogramVec
	signalToOrder      *prometheus.HistogramVec
	sessionUtilization *prometheus.GaugeVec
}

// NewRiskMetrics creates and registers the trading-risk series on reg.
func NewRiskMetrics(reg prometheus.Registerer) *RiskMetrics {
	m := &RiskMetrics{
		haltActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "trading_halt_active",
			Help: "1 when new orders are blocked by an operator halt file (halted, unreadable or invalid); 0 otherwise",
		}),
		haltFilesInvalid: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "trading_halt_files_invalid",
			Help: "Number of configured halt files that cannot be read or parsed (each blocks every order)",
		}),
		haltFilesConfig: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "trading_halt_files_configured",
			Help: "Number of halt files the engine watches (TRADING_HALT_FILES); 0 means the kill switch cannot reach it",
		}),
		haltLastCheck: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "trading_halt_last_check_timestamp_seconds",
			Help: "Unix time the halt files were last evaluated",
		}),
		policyInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "trading_risk_policy_info",
			Help: "Always 1; labels name the live risk policy version and its source (built-in or file)",
		}, []string{"version", "source"}),
		limit: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "trading_risk_limit",
			Help: "Configured limit value by book and limit (book=session for session-wide limits); 0 = disabled",
		}, []string{"book", "limit"}),
		policyChecks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pretrade_policy_checks_total",
			Help: "Per-order shared-policy checks by result (allowed, blocked, error)",
		}, []string{"book", "result"}),
		policyRejections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pretrade_policy_rejections_total",
			Help: "Blocking findings by rule (halted, max_order_btc, max_order_notional, max_position_btc, max_price_deviation_bps, invalid_order, position_unknown)",
		}, []string{"book", "rule"}),
		limitUtilization: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "order_limit_utilization_ratio",
			Help:    "Order value / limit for each per-order limit (1 = at the limit)",
			Buckets: []float64{0.1, 0.25, 0.5, 0.75, 0.9, 1, 1.25, 2},
		}, []string{"book", "limit"}),
		priceDeviationBps: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "order_price_deviation_bps",
			Help:    "Distance of the order price from the touch mid at decision time, in bps (arrival-price slippage proxy)",
			Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000},
		}, []string{"book", "side"}),
		signalAge: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "signal_age_at_decision_seconds",
			Help:    "Age of a trade signal (now - signal timestamp) when the engine checks it",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		}, []string{"book"}),
		signalToOrder: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "signal_to_order_latency_seconds",
			Help:    "Signal timestamp to order accepted by the exchange (tick-to-trade)",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		}, []string{"book"}),
		sessionUtilization: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "trading_session_limit_utilization_ratio",
			Help: "Session loss / max_daily_loss and drawdown / max_drawdown_pct at the last check (1 = limit hit, orders blocked)",
		}, []string{"limit"}),
	}
	reg.MustRegister(m.haltActive, m.haltFilesInvalid, m.haltFilesConfig, m.haltLastCheck,
		m.policyInfo, m.limit, m.policyChecks, m.policyRejections, m.limitUtilization,
		m.priceDeviationBps, m.signalAge, m.signalToOrder, m.sessionUtilization)
	return m
}

// SetPolicy records the live policy version and source.
func (m *RiskMetrics) SetPolicy(version, source string) {
	m.policyInfo.Reset()
	m.policyInfo.WithLabelValues(version, source).Set(1)
}

// SetLimit records one configured limit.
func (m *RiskMetrics) SetLimit(book, limit string, v float64) {
	m.limit.WithLabelValues(book, limit).Set(v)
}

// SetHaltState records the result of evaluating the halt files.
func (m *RiskMetrics) SetHaltState(configured, invalid int, halted bool, at time.Time) {
	m.haltFilesConfig.Set(float64(configured))
	m.haltFilesInvalid.Set(float64(invalid))
	if halted || invalid > 0 {
		m.haltActive.Set(1)
	} else {
		m.haltActive.Set(0)
	}
	m.haltLastCheck.Set(float64(at.Unix()))
}

// RecordPolicyCheck counts one check and its blocking rules.
func (m *RiskMetrics) RecordPolicyCheck(book, result string, rules []string) {
	m.policyChecks.WithLabelValues(book, result).Inc()
	for _, r := range rules {
		m.policyRejections.WithLabelValues(book, r).Inc()
	}
}

// ObserveUtilization records value/limit for one per-order limit (skipped
// when the limit is disabled).
func (m *RiskMetrics) ObserveUtilization(book, limit string, value, max float64) {
	if max <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	m.limitUtilization.WithLabelValues(book, limit).Observe(math.Abs(value) / max)
}

// ObservePriceDeviation records the order's distance from the mid in bps.
func (m *RiskMetrics) ObservePriceDeviation(book, side string, bps float64) {
	if math.IsNaN(bps) || math.IsInf(bps, 0) || bps < 0 {
		return
	}
	m.priceDeviationBps.WithLabelValues(book, side).Observe(bps)
}

// ObserveSignalAge records the signal's age at the risk check.
func (m *RiskMetrics) ObserveSignalAge(book string, age time.Duration) {
	if age < 0 {
		age = 0
	}
	m.signalAge.WithLabelValues(book).Observe(age.Seconds())
}

// ObserveSignalToOrder records signal-to-exchange-ack latency.
func (m *RiskMetrics) ObserveSignalToOrder(book string, d time.Duration) {
	if d < 0 {
		d = 0
	}
	m.signalToOrder.WithLabelValues(book).Observe(d.Seconds())
}

// SetSessionUtilization records session loss and drawdown against limits.
func (m *RiskMetrics) SetSessionUtilization(limit string, v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	m.sessionUtilization.WithLabelValues(limit).Set(v)
}
