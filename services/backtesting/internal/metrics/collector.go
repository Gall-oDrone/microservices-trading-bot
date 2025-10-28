package metrics

import "time"

// StartBacktestTimer starts a timer and returns a function to record completion
func (m *MetricsCollector) StartBacktestTimer() func(status string) {
	start := time.Now()
	return func(status string) {
		duration := time.Since(start)
		m.RecordBacktestCompleted(status, duration)
	}
}

// StartDataLoadTimer starts a timer for data loading
func (m *MetricsCollector) StartDataLoadTimer(source string) func() {
	start := time.Now()
	return func() {
		duration := time.Since(start)
		m.RecordDataLoadDuration(source, duration)
	}
}

// StartMetricsTimer starts a timer for metrics calculation
func (m *MetricsCollector) StartMetricsTimer(metric string) func() {
	start := time.Now()
	return func() {
		duration := time.Since(start)
		m.RecordMetricsCalculation(metric, duration)
	}
}

// RecordBacktestProgress records backtest progress (optional, for monitoring)
// This could be extended to expose progress gauges if needed
func (m *MetricsCollector) RecordBacktestProgress(backtestID string, progress float64) {
	// Optional: Add progress tracking
	// For now, this is a placeholder for future enhancement
}

// RecordAPIRequest records HTTP API request metrics
// This can be used by the HTTP server middleware
func (m *MetricsCollector) RecordAPIRequest(endpoint, method string, duration time.Duration, statusCode int) {
	// Optional: Add API request metrics
	// This would require additional prometheus metrics to be defined
	// For now, this is a placeholder for future enhancement
}

// RecordCacheHit records cache hit/miss
func (m *MetricsCollector) RecordCacheHit(hit bool) {
	// Optional: Add cache metrics
	// This would require additional prometheus metrics to be defined
	// For now, this is a placeholder for future enhancement
}

// RecordStorageOperation records storage operation metrics
func (m *MetricsCollector) RecordStorageOperation(operation string, duration time.Duration, success bool) {
	// Optional: Add storage metrics
	// This would require additional prometheus metrics to be defined
	// For now, this is a placeholder for future enhancement
}

// IncrementActiveBacktests increments the active backtests counter
func (m *MetricsCollector) IncrementActiveBacktests() {
	m.activeBacktests.Inc()
}

// DecrementActiveBacktests decrements the active backtests counter
func (m *MetricsCollector) DecrementActiveBacktests() {
	m.activeBacktests.Dec()
}

// RecordTradesProcessed records the number of trades processed
func (m *MetricsCollector) RecordTradesProcessed(count int) {
	m.RecordEventsProcessed("trade", count)
}

// RecordTickersProcessed records the number of tickers processed
func (m *MetricsCollector) RecordTickersProcessed(count int) {
	m.RecordEventsProcessed("ticker", count)
}

// RecordOrderBooksProcessed records the number of order books processed
func (m *MetricsCollector) RecordOrderBooksProcessed(count int) {
	m.RecordEventsProcessed("orderbook", count)
}
