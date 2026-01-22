package metrics

import (
	"strconv"
	"sync"
	"time"
)

// Counter represents a counter metric
type Counter struct {
	value float64
	mu    sync.RWMutex
}

// Inc increments the counter by 1
func (c *Counter) Inc() {
	c.Add(1)
}

// Add adds the given value to the counter
func (c *Counter) Add(delta float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value += delta
}

// Get returns the current counter value
func (c *Counter) Get() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.value
}

// Gauge represents a gauge metric
type Gauge struct {
	value float64
	mu    sync.RWMutex
}

// Set sets the gauge value
func (g *Gauge) Set(value float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value = value
}

// Add adds the given value to the gauge
func (g *Gauge) Add(delta float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value += delta
}

// Get returns the current gauge value
func (g *Gauge) Get() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.value
}

// SetToCurrentTime sets the gauge to the current Unix timestamp
func (g *Gauge) SetToCurrentTime() {
	g.Set(float64(time.Now().Unix()))
}

// CounterVec represents a vector of counters with labels
type CounterVec struct {
	counters map[string]*Counter
	mu       sync.RWMutex
}

// NewCounterVec creates a new counter vector
func NewCounterVec() *CounterVec {
	return &CounterVec{
		counters: make(map[string]*Counter),
	}
}

// WithLabelValues returns a counter with the given label values
func (cv *CounterVec) WithLabelValues(lvs ...string) *Counter {
	key := joinLabels(lvs)

	cv.mu.Lock()
	defer cv.mu.Unlock()

	if counter, exists := cv.counters[key]; exists {
		return counter
	}

	counter := &Counter{}
	cv.counters[key] = counter
	return counter
}

// GaugeVec represents a vector of gauges with labels
type GaugeVec struct {
	gauges map[string]*Gauge
	mu     sync.RWMutex
}

// NewGaugeVec creates a new gauge vector
func NewGaugeVec() *GaugeVec {
	return &GaugeVec{
		gauges: make(map[string]*Gauge),
	}
}

// WithLabelValues returns a gauge with the given label values
func (gv *GaugeVec) WithLabelValues(lvs ...string) *Gauge {
	key := joinLabels(lvs)

	gv.mu.Lock()
	defer gv.mu.Unlock()

	if gauge, exists := gv.gauges[key]; exists {
		return gauge
	}

	gauge := &Gauge{}
	gv.gauges[key] = gauge
	return gauge
}

// HistogramVec represents a vector of histograms with labels
type HistogramVec struct {
	histograms map[string]*Histogram
	mu         sync.RWMutex
}

// NewHistogramVec creates a new histogram vector
func NewHistogramVec() *HistogramVec {
	return &HistogramVec{
		histograms: make(map[string]*Histogram),
	}
}

// WithLabelValues returns a histogram with the given label values
func (hv *HistogramVec) WithLabelValues(lvs ...string) *Histogram {
	key := joinLabels(lvs)

	hv.mu.Lock()
	defer hv.mu.Unlock()

	if histogram, exists := hv.histograms[key]; exists {
		return histogram
	}

	histogram := &Histogram{
		buckets: make(map[float64]float64),
	}
	hv.histograms[key] = histogram
	return histogram
}

// Histogram represents a histogram metric
type Histogram struct {
	count   float64
	sum     float64
	buckets map[float64]float64
	mu      sync.RWMutex
}

// NewHistogram creates a new histogram
func NewHistogram() *Histogram {
	return &Histogram{
		buckets: make(map[float64]float64),
	}
}

// Observe records a value in the histogram
func (h *Histogram) Observe(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.count++
	h.sum += value

	// Simple bucket implementation
	buckets := []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1.0, 2.5, 5.0, 7.5, 10.0}
	for _, bucket := range buckets {
		if value <= bucket {
			h.buckets[bucket]++
		}
	}
}

// GetCount returns the total count of observations
func (h *Histogram) GetCount() float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.count
}

// GetSum returns the sum of all observations
func (h *Histogram) GetSum() float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sum
}

// GetBuckets returns the bucket values
func (h *Histogram) GetBuckets() map[float64]float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	buckets := make(map[float64]float64)
	for k, v := range h.buckets {
		buckets[k] = v
	}
	return buckets
}

// Metrics holds all metrics for the strategy executor service
type Metrics struct {
	// Service metrics
	ServiceStartTime    *Gauge
	ServiceUptime       *Gauge
	ServiceHealthStatus *Gauge

	// Strategy metrics
	ActiveStrategies   *Gauge
	StrategyExecutions *CounterVec
	StrategyErrors     *CounterVec
	StrategyLatency    *HistogramVec
	StrategySignals    *CounterVec

	// Market data metrics
	MarketDataMessagesReceived  *CounterVec
	MarketDataMessagesProcessed *CounterVec
	MarketDataProcessingLatency *HistogramVec
	MarketDataErrors            *CounterVec

	// Signal processing metrics
	SignalsGenerated        *CounterVec
	SignalsPublished        *CounterVec
	SignalsFailed           *CounterVec
	SignalProcessingLatency *HistogramVec

	// Risk management metrics
	RiskChecksPerformed *CounterVec
	RiskViolations      *CounterVec
	RiskCheckLatency    *HistogramVec

	// Kafka metrics
	KafkaMessagesConsumed *CounterVec
	KafkaMessagesProduced *CounterVec
	KafkaConsumerLag      *GaugeVec
	KafkaProducerLatency  *HistogramVec

	// HTTP API metrics
	HTTPRequestsTotal    *CounterVec
	HTTPRequestDuration  *HistogramVec
	HTTPRequestsInFlight *Gauge

	// Business metrics
	TradingSignalsTotal *Counter
	BuySignalsTotal     *Counter
	SellSignalsTotal    *Counter
	HoldSignalsTotal    *Counter
	SignalsPerStrategy  *CounterVec
	SignalsPerBook      *CounterVec
}

// New creates a new Metrics instance
func New(serviceName string) *Metrics {
	return &Metrics{
		// Service metrics
		ServiceStartTime:    &Gauge{},
		ServiceUptime:       &Gauge{},
		ServiceHealthStatus: &Gauge{},

		// Strategy metrics
		ActiveStrategies:   &Gauge{},
		StrategyExecutions: NewCounterVec(),
		StrategyErrors:     NewCounterVec(),
		StrategyLatency:    NewHistogramVec(),
		StrategySignals:    NewCounterVec(),

		// Market data metrics
		MarketDataMessagesReceived:  NewCounterVec(),
		MarketDataMessagesProcessed: NewCounterVec(),
		MarketDataProcessingLatency: NewHistogramVec(),
		MarketDataErrors:            NewCounterVec(),

		// Signal processing metrics
		SignalsGenerated:        NewCounterVec(),
		SignalsPublished:        NewCounterVec(),
		SignalsFailed:           NewCounterVec(),
		SignalProcessingLatency: NewHistogramVec(),

		// Risk management metrics
		RiskChecksPerformed: NewCounterVec(),
		RiskViolations:      NewCounterVec(),
		RiskCheckLatency:    NewHistogramVec(),

		// Kafka metrics
		KafkaMessagesConsumed: NewCounterVec(),
		KafkaMessagesProduced: NewCounterVec(),
		KafkaConsumerLag:      NewGaugeVec(),
		KafkaProducerLatency:  NewHistogramVec(),

		// HTTP API metrics
		HTTPRequestsTotal:    NewCounterVec(),
		HTTPRequestDuration:  NewHistogramVec(),
		HTTPRequestsInFlight: &Gauge{},

		// Business metrics
		TradingSignalsTotal: &Counter{},
		BuySignalsTotal:     &Counter{},
		SellSignalsTotal:    &Counter{},
		HoldSignalsTotal:    &Counter{},
		SignalsPerStrategy:  NewCounterVec(),
		SignalsPerBook:      NewCounterVec(),
	}
}

// RecordServiceStart records the service start time
func (m *Metrics) RecordServiceStart() {
	m.ServiceStartTime.SetToCurrentTime()
}

// RecordServiceUptime records the current uptime
func (m *Metrics) RecordServiceUptime(uptime time.Duration) {
	m.ServiceUptime.Set(uptime.Seconds())
}

// RecordServiceHealth records the service health status
func (m *Metrics) RecordServiceHealth(healthy bool) {
	if healthy {
		m.ServiceHealthStatus.Set(1)
	} else {
		m.ServiceHealthStatus.Set(0)
	}
}

// RecordActiveStrategies records the number of active strategies
func (m *Metrics) RecordActiveStrategies(count int) {
	m.ActiveStrategies.Set(float64(count))
}

// RecordStrategyExecution records a strategy execution
func (m *Metrics) RecordStrategyExecution(strategy, book string, duration time.Duration) {
	m.StrategyExecutions.WithLabelValues(strategy, book).Inc()
	m.StrategyLatency.WithLabelValues(strategy, book).Observe(duration.Seconds())
}

// RecordStrategyError records a strategy error
func (m *Metrics) RecordStrategyError(strategy, book, errorType string) {
	m.StrategyErrors.WithLabelValues(strategy, book, errorType).Inc()
}

// RecordStrategySignal records a strategy signal
func (m *Metrics) RecordStrategySignal(strategy, book, signalType string) {
	m.StrategySignals.WithLabelValues(strategy, book, signalType).Inc()
}

// RecordMarketDataMessageReceived records a received market data message
func (m *Metrics) RecordMarketDataMessageReceived(topic, book string) {
	m.MarketDataMessagesReceived.WithLabelValues(topic, book).Inc()
}

// RecordMarketDataMessageProcessed records a processed market data message
func (m *Metrics) RecordMarketDataMessageProcessed(topic, book string, duration time.Duration) {
	m.MarketDataMessagesProcessed.WithLabelValues(topic, book).Inc()
	m.MarketDataProcessingLatency.WithLabelValues(topic, book).Observe(duration.Seconds())
}

// RecordMarketDataError records a market data processing error
func (m *Metrics) RecordMarketDataError(topic, book, errorType string) {
	m.MarketDataErrors.WithLabelValues(topic, book, errorType).Inc()
}

// RecordSignalGenerated records a generated signal
func (m *Metrics) RecordSignalGenerated(strategy, book, signalType string, duration time.Duration) {
	m.SignalsGenerated.WithLabelValues(strategy, book, signalType).Inc()
	m.SignalProcessingLatency.WithLabelValues(strategy, book, signalType).Observe(duration.Seconds())

	// Update business metrics
	m.TradingSignalsTotal.Inc()
	m.SignalsPerStrategy.WithLabelValues(strategy).Inc()
	m.SignalsPerBook.WithLabelValues(book).Inc()

	switch signalType {
	case "BUY":
		m.BuySignalsTotal.Inc()
	case "SELL":
		m.SellSignalsTotal.Inc()
	case "HOLD":
		m.HoldSignalsTotal.Inc()
	}
}

// RecordSignalPublished records a published signal
func (m *Metrics) RecordSignalPublished(strategy, book, signalType string) {
	m.SignalsPublished.WithLabelValues(strategy, book, signalType).Inc()
}

// RecordSignalFailed records a failed signal publication
func (m *Metrics) RecordSignalFailed(strategy, book, signalType, errorType string) {
	m.SignalsFailed.WithLabelValues(strategy, book, signalType, errorType).Inc()
}

// RecordRiskCheck records a risk check
func (m *Metrics) RecordRiskCheck(checkType string, duration time.Duration) {
	m.RiskChecksPerformed.WithLabelValues(checkType).Inc()
	m.RiskCheckLatency.WithLabelValues(checkType).Observe(duration.Seconds())
}

// RecordRiskViolation records a risk violation
func (m *Metrics) RecordRiskViolation(violationType string) {
	m.RiskViolations.WithLabelValues(violationType).Inc()
}

// RecordKafkaMessageConsumed records a consumed Kafka message
func (m *Metrics) RecordKafkaMessageConsumed(topic string, partition int) {
	m.KafkaMessagesConsumed.WithLabelValues(topic, strconv.Itoa(partition)).Inc()
}

// RecordKafkaMessageProduced records a produced Kafka message
func (m *Metrics) RecordKafkaMessageProduced(topic string, partition int) {
	m.KafkaMessagesProduced.WithLabelValues(topic, strconv.Itoa(partition)).Inc()
}

// RecordKafkaConsumerLag records Kafka consumer lag
func (m *Metrics) RecordKafkaConsumerLag(topic string, partition int, lag int64) {
	m.KafkaConsumerLag.WithLabelValues(topic, strconv.Itoa(partition)).Set(float64(lag))
}

// RecordKafkaProducerLatency records Kafka producer latency
func (m *Metrics) RecordKafkaProducerLatency(topic string, duration time.Duration) {
	m.KafkaProducerLatency.WithLabelValues(topic).Observe(duration.Seconds())
}

// RecordHTTPRequest records an HTTP request
func (m *Metrics) RecordHTTPRequest(method, endpoint, statusCode string, duration time.Duration) {
	m.HTTPRequestsTotal.WithLabelValues(method, endpoint, statusCode).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
}

// RecordHTTPRequestInFlight records an in-flight HTTP request
func (m *Metrics) RecordHTTPRequestInFlight(delta float64) {
	m.HTTPRequestsInFlight.Add(delta)
}

// Helper function to join label values
func joinLabels(lvs []string) string {
	if len(lvs) == 0 {
		return ""
	}

	result := lvs[0]
	for i := 1; i < len(lvs); i++ {
		result += "|" + lvs[i]
	}
	return result
}
