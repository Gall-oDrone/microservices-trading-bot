package metrics

import (
	"context"
	"log"
	"net/http"
	"runtime"
	"time"

	"bitso-trading-platform/shared/pkg/models"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics
type Metrics struct {
	// Trade metrics
	TradeCounter              prometheus.Counter
	TradeLatency              prometheus.Histogram
	TradeVolume               prometheus.Counter
	TradeValue                prometheus.Counter
	LastTradeAgeSeconds       prometheus.Gauge
	TradeSilenceReconnects    prometheus.Counter

	// Order book metrics
	OrderBookUpdates prometheus.Counter
	OrderBookDepth   prometheus.Gauge

	// Ticker metrics
	TickerUpdates prometheus.Counter
	TickerLatency prometheus.Histogram

	// Cache metrics
	CacheHits       prometheus.Counter
	CacheMisses     prometheus.Counter
	CacheOperations prometheus.Counter

	// Storage metrics
	StorageOperations prometheus.Counter
	StorageLatency    prometheus.Histogram
	StorageErrors     prometheus.Counter

	// WebSocket metrics
	WebSocketConnections     prometheus.Gauge
	WebSocketMessages        prometheus.Counter
	WebSocketErrors          prometheus.Counter
	WebSocketReconnects      prometheus.Counter
	WebSocketSubscribeErrors prometheus.Counter // Phase 2

	// Historical API (Phase 2)
	HistoricalRequestsTotal     prometheus.Counter
	HistoricalRequestDuration   prometheus.Histogram
	HistoricalErrorsTotal       prometheus.Counter

	// API metrics
	APIRequests     prometheus.Counter
	APIResponseTime prometheus.Histogram
	APIErrors       prometheus.Counter

	// System metrics
	MemoryUsage    prometheus.Gauge
	CPUUsage       prometheus.Gauge
	GoroutineCount prometheus.Gauge
}

// NewMetrics creates a new metrics instance
func NewMetrics() *Metrics {
	return &Metrics{
		// Trade metrics
		TradeCounter: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_trades_total",
			Help: "Total number of trades processed",
		}),

		TradeLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "market_data_trade_latency_seconds",
			Help:    "Trade processing latency",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to 32s
		}),

		TradeVolume: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_trade_volume_total",
			Help: "Total trade volume processed",
		}),

		TradeValue: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_trade_value_total",
			Help: "Total trade value processed",
		}),

		LastTradeAgeSeconds: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "market_data_last_trade_age_seconds",
			Help: "Seconds since the most recent trade was received from the WebSocket stream",
		}),

		TradeSilenceReconnects: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_trade_silence_reconnects_total",
			Help: "Total number of WebSocket reconnects triggered by the trade silence watchdog",
		}),

		// Order book metrics
		OrderBookUpdates: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_orderbook_updates_total",
			Help: "Total number of order book updates",
		}),

		OrderBookDepth: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "market_data_orderbook_depth",
			Help: "Current order book depth",
		}),

		// Ticker metrics
		TickerUpdates: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_ticker_updates_total",
			Help: "Total number of ticker updates",
		}),

		TickerLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "market_data_ticker_latency_seconds",
			Help:    "Ticker processing latency",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
		}),

		// Cache metrics
		CacheHits: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_cache_hits_total",
			Help: "Total number of cache hits",
		}),

		CacheMisses: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_cache_misses_total",
			Help: "Total number of cache misses",
		}),

		CacheOperations: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_cache_operations_total",
			Help: "Total number of cache operations",
		}),

		// Storage metrics
		StorageOperations: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_storage_operations_total",
			Help: "Total number of storage operations",
		}),

		StorageLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "market_data_storage_latency_seconds",
			Help:    "Storage operation latency",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
		}),

		StorageErrors: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_storage_errors_total",
			Help: "Total number of storage errors",
		}),

		// WebSocket metrics
		WebSocketConnections: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "market_data_websocket_connections",
			Help: "Current number of WebSocket connections",
		}),

		WebSocketMessages: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_websocket_messages_total",
			Help: "Total number of WebSocket messages received",
		}),

		WebSocketErrors: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_websocket_errors_total",
			Help: "Total number of WebSocket errors",
		}),

		WebSocketReconnects: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_websocket_reconnects_total",
			Help: "Total number of WebSocket reconnections",
		}),

		WebSocketSubscribeErrors: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_websocket_subscribe_errors_total",
			Help: "Total number of WebSocket subscribe errors",
		}),

		// Historical API (Phase 2; for backtesting consumers)
		HistoricalRequestsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_historical_requests_total",
			Help: "Total number of historical data API requests",
		}),
		HistoricalRequestDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "market_data_historical_request_duration_seconds",
			Help:    "Historical data request duration in seconds",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 12),
		}),
		HistoricalErrorsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_historical_errors_total",
			Help: "Total number of historical data request errors",
		}),

		// API metrics
		APIRequests: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_api_requests_total",
			Help: "Total number of API requests",
		}),

		APIResponseTime: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "market_data_api_response_time_seconds",
			Help:    "API response time",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
		}),

		APIErrors: promauto.NewCounter(prometheus.CounterOpts{
			Name: "market_data_api_errors_total",
			Help: "Total number of API errors",
		}),

		// System metrics
		MemoryUsage: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "market_data_memory_usage_bytes",
			Help: "Current memory usage in bytes",
		}),

		CPUUsage: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "market_data_cpu_usage_percent",
			Help: "Current CPU usage percentage",
		}),

		GoroutineCount: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "market_data_goroutines",
			Help: "Current number of goroutines",
		}),
	}
}

// TradeMetrics handles trade-related metrics
type TradeMetrics struct {
	metrics *Metrics
}

// NewTradeMetrics creates a new trade metrics instance
func NewTradeMetrics(metrics *Metrics) *TradeMetrics {
	return &TradeMetrics{
		metrics: metrics,
	}
}

// RecordTrade records trade metrics
func (tm *TradeMetrics) RecordTrade(trade *models.TradeEvent, latency time.Duration) {
	if trade == nil {
		return
	}

	// Increment trade counter
	tm.metrics.TradeCounter.Inc()

	// Record trade latency
	tm.metrics.TradeLatency.Observe(latency.Seconds())

	// Record trade volume and value
	tm.metrics.TradeVolume.Add(trade.Amount)
	tm.metrics.TradeValue.Add(trade.Value)
}

// OrderBookMetrics handles order book-related metrics
type OrderBookMetrics struct {
	metrics *Metrics
}

// NewOrderBookMetrics creates a new order book metrics instance
func NewOrderBookMetrics(metrics *Metrics) *OrderBookMetrics {
	return &OrderBookMetrics{
		metrics: metrics,
	}
}

// RecordOrderBookUpdate records order book update metrics
func (obm *OrderBookMetrics) RecordOrderBookUpdate(depth int) {
	obm.metrics.OrderBookUpdates.Inc()
	obm.metrics.OrderBookDepth.Set(float64(depth))
}

// TickerMetrics handles ticker-related metrics
type TickerMetrics struct {
	metrics *Metrics
}

// NewTickerMetrics creates a new ticker metrics instance
func NewTickerMetrics(metrics *Metrics) *TickerMetrics {
	return &TickerMetrics{
		metrics: metrics,
	}
}

// RecordTickerUpdate records ticker update metrics
func (tm *TickerMetrics) RecordTickerUpdate(latency time.Duration) {
	tm.metrics.TickerUpdates.Inc()
	tm.metrics.TickerLatency.Observe(latency.Seconds())
}

// CacheMetrics handles cache-related metrics
type CacheMetrics struct {
	metrics *Metrics
}

// NewCacheMetrics creates a new cache metrics instance
func NewCacheMetrics(metrics *Metrics) *CacheMetrics {
	return &CacheMetrics{
		metrics: metrics,
	}
}

// RecordCacheHit records a cache hit
func (cm *CacheMetrics) RecordCacheHit() {
	cm.metrics.CacheHits.Inc()
	cm.metrics.CacheOperations.Inc()
}

// RecordCacheMiss records a cache miss
func (cm *CacheMetrics) RecordCacheMiss() {
	cm.metrics.CacheMisses.Inc()
	cm.metrics.CacheOperations.Inc()
}

// StorageMetrics handles storage-related metrics
type StorageMetrics struct {
	metrics *Metrics
}

// NewStorageMetrics creates a new storage metrics instance
func NewStorageMetrics(metrics *Metrics) *StorageMetrics {
	return &StorageMetrics{
		metrics: metrics,
	}
}

// RecordStorageOperation records a storage operation
func (sm *StorageMetrics) RecordStorageOperation(latency time.Duration, success bool) {
	sm.metrics.StorageOperations.Inc()
	sm.metrics.StorageLatency.Observe(latency.Seconds())

	if !success {
		sm.metrics.StorageErrors.Inc()
	}
}

// WebSocketMetrics handles WebSocket-related metrics
type WebSocketMetrics struct {
	metrics *Metrics
}

// NewWebSocketMetrics creates a new WebSocket metrics instance
func NewWebSocketMetrics(metrics *Metrics) *WebSocketMetrics {
	return &WebSocketMetrics{
		metrics: metrics,
	}
}

// RecordWebSocketConnection records a WebSocket connection
func (wsm *WebSocketMetrics) RecordWebSocketConnection(connected bool) {
	if connected {
		wsm.metrics.WebSocketConnections.Inc()
	} else {
		wsm.metrics.WebSocketConnections.Dec()
	}
}

// RecordWebSocketMessage records a WebSocket message
func (wsm *WebSocketMetrics) RecordWebSocketMessage() {
	wsm.metrics.WebSocketMessages.Inc()
}

// RecordWebSocketError records a WebSocket error
func (wsm *WebSocketMetrics) RecordWebSocketError() {
	wsm.metrics.WebSocketErrors.Inc()
}

// RecordWebSocketReconnect records a WebSocket reconnection
func (wsm *WebSocketMetrics) RecordWebSocketReconnect() {
	wsm.metrics.WebSocketReconnects.Inc()
}

// RecordWebSocketSubscribeError records a WebSocket subscribe error (Phase 2)
func (wsm *WebSocketMetrics) RecordWebSocketSubscribeError() {
	wsm.metrics.WebSocketSubscribeErrors.Inc()
}

// APIMetrics handles API-related metrics
type APIMetrics struct {
	metrics *Metrics
}

// NewAPIMetrics creates a new API metrics instance
func NewAPIMetrics(metrics *Metrics) *APIMetrics {
	return &APIMetrics{
		metrics: metrics,
	}
}

// RecordAPIRequest records an API request
func (am *APIMetrics) RecordAPIRequest(responseTime time.Duration, success bool) {
	am.metrics.APIRequests.Inc()
	am.metrics.APIResponseTime.Observe(responseTime.Seconds())

	if !success {
		am.metrics.APIErrors.Inc()
	}
}

// SystemMetrics handles system-related metrics
type SystemMetrics struct {
	metrics *Metrics
}

// NewSystemMetrics creates a new system metrics instance
func NewSystemMetrics(metrics *Metrics) *SystemMetrics {
	return &SystemMetrics{
		metrics: metrics,
	}
}

// RecordSystemMetrics records system metrics
func (sm *SystemMetrics) RecordSystemMetrics(memoryUsage int64, cpuUsage float64, goroutineCount int) {
	sm.metrics.MemoryUsage.Set(float64(memoryUsage))
	sm.metrics.CPUUsage.Set(cpuUsage)
	sm.metrics.GoroutineCount.Set(float64(goroutineCount))
}

// MetricsCollector collects and manages all metrics
type MetricsCollector struct {
	metrics *Metrics
	logger  *log.Logger
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(logger *log.Logger) *MetricsCollector {
	if logger == nil {
		logger = log.New(log.Writer(), "[METRICS] ", log.LstdFlags|log.Lshortfile)
	}

	return &MetricsCollector{
		metrics: NewMetrics(),
		logger:  logger,
	}
}

// GetMetrics returns the metrics instance
func (mc *MetricsCollector) GetMetrics() *Metrics {
	return mc.metrics
}

// StartSystemMetricsCollection starts collecting system metrics
func (mc *MetricsCollector) StartSystemMetricsCollection(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Collect system metrics
			// This is a simplified implementation
			// In production, you would use proper system monitoring libraries

			// Get memory usage (simplified)
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			memoryUsage := int64(memStats.Alloc)

			// Get goroutine count
			goroutineCount := runtime.NumGoroutine()

			// Get CPU usage (simplified)
			cpuUsage := 0.0 // This would be calculated using proper CPU monitoring

			// Record metrics
			systemMetrics := NewSystemMetrics(mc.metrics)
			systemMetrics.RecordSystemMetrics(memoryUsage, cpuUsage, goroutineCount)
		}
	}
}

// GetPrometheusHandler returns the Prometheus HTTP handler
func (mc *MetricsCollector) GetPrometheusHandler() http.Handler {
	return promhttp.Handler()
}

// RecordTradeMetrics records trade metrics
func (mc *MetricsCollector) RecordTradeMetrics(trade *models.TradeEvent, latency time.Duration) {
	tradeMetrics := NewTradeMetrics(mc.metrics)
	tradeMetrics.RecordTrade(trade, latency)
}

// RecordOrderBookMetrics records order book metrics
func (mc *MetricsCollector) RecordOrderBookMetrics(depth int) {
	orderBookMetrics := NewOrderBookMetrics(mc.metrics)
	orderBookMetrics.RecordOrderBookUpdate(depth)
}

// RecordTickerMetrics records ticker metrics
func (mc *MetricsCollector) RecordTickerMetrics(latency time.Duration) {
	tickerMetrics := NewTickerMetrics(mc.metrics)
	tickerMetrics.RecordTickerUpdate(latency)
}

// RecordCacheMetrics records cache metrics
func (mc *MetricsCollector) RecordCacheMetrics(hit bool) {
	cacheMetrics := NewCacheMetrics(mc.metrics)
	if hit {
		cacheMetrics.RecordCacheHit()
	} else {
		cacheMetrics.RecordCacheMiss()
	}
}

// RecordStorageMetrics records storage metrics
func (mc *MetricsCollector) RecordStorageMetrics(latency time.Duration, success bool) {
	storageMetrics := NewStorageMetrics(mc.metrics)
	storageMetrics.RecordStorageOperation(latency, success)
}

// RecordWebSocketMetrics records WebSocket metrics
func (mc *MetricsCollector) RecordWebSocketMetrics(connection bool, message bool, error bool, reconnect bool) {
	wsMetrics := NewWebSocketMetrics(mc.metrics)

	if connection {
		wsMetrics.RecordWebSocketConnection(true)
	}
	if message {
		wsMetrics.RecordWebSocketMessage()
	}
	if error {
		wsMetrics.RecordWebSocketError()
	}
	if reconnect {
		wsMetrics.RecordWebSocketReconnect()
	}
}

// RecordAPIMetrics records API metrics
func (mc *MetricsCollector) RecordAPIMetrics(responseTime time.Duration, success bool) {
	apiMetrics := NewAPIMetrics(mc.metrics)
	apiMetrics.RecordAPIRequest(responseTime, success)
}

// RecordWebSocketSubscribeError records a WebSocket subscribe error (Phase 2)
func (mc *MetricsCollector) RecordWebSocketSubscribeError() {
	mc.metrics.WebSocketSubscribeErrors.Inc()
}

// RecordHistoricalRequest records a historical data API request (Phase 2). success false increments errors.
func (mc *MetricsCollector) RecordHistoricalRequest(duration time.Duration, success bool) {
	mc.metrics.HistoricalRequestsTotal.Inc()
	mc.metrics.HistoricalRequestDuration.Observe(duration.Seconds())
	if !success {
		mc.metrics.HistoricalErrorsTotal.Inc()
	}
}

// SetLastTradeAgeSeconds records how long ago the last trade was received.
func (mc *MetricsCollector) SetLastTradeAgeSeconds(ageSec float64) {
	mc.metrics.LastTradeAgeSeconds.Set(ageSec)
}

// RecordTradeSilenceReconnect records a watchdog-triggered WebSocket reconnect.
func (mc *MetricsCollector) RecordTradeSilenceReconnect() {
	mc.metrics.TradeSilenceReconnects.Inc()
}
