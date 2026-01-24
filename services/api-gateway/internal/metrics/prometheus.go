package metrics

import (
	"context"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsCollector collects and exposes Prometheus metrics
type MetricsCollector struct {
	// HTTP metrics
	httpRequestsTotal    *prometheus.CounterVec
	httpRequestDuration  *prometheus.HistogramVec
	httpRequestsInFlight *prometheus.GaugeVec
	httpRequestSize      *prometheus.HistogramVec
	httpResponseSize     *prometheus.HistogramVec

	// Backend client metrics
	backendCallsTotal   *prometheus.CounterVec
	backendCallDuration *prometheus.HistogramVec
	backendErrorsTotal  *prometheus.CounterVec

	// Circuit breaker metrics
	circuitBreakerState *prometheus.GaugeVec
	circuitBreakerOps   *prometheus.CounterVec

	// Rate limiter metrics
	rateLimitHitsTotal   *prometheus.CounterVec
	rateLimitAllowsTotal *prometheus.CounterVec

	// System metrics
	serviceUptime prometheus.Gauge
	serviceHealth *prometheus.GaugeVec
	goroutines    prometheus.Gauge
	memoryUsage   prometheus.Gauge
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(serviceName string) *MetricsCollector {
	// Sanitize service name for Prometheus (replace hyphens with underscores)
	// Prometheus metric names must match [a-zA-Z_:][a-zA-Z0-9_:]*
	sanitizedName := strings.ReplaceAll(serviceName, "-", "_")

	mc := &MetricsCollector{
		// HTTP metrics
		httpRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: sanitizedName,
				Name:      "http_requests_total",
				Help:      "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),
		httpRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: sanitizedName,
				Name:      "http_request_duration_seconds",
				Help:      "HTTP request duration in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),
		httpRequestsInFlight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: sanitizedName,
				Name:      "http_requests_in_flight",
				Help:      "Number of HTTP requests currently being served",
			},
			[]string{"path"},
		),
		httpRequestSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: sanitizedName,
				Name:      "http_request_size_bytes",
				Help:      "HTTP request size in bytes",
				Buckets:   prometheus.ExponentialBuckets(100, 10, 8),
			},
			[]string{"method", "path"},
		),
		httpResponseSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: sanitizedName,
				Name:      "http_response_size_bytes",
				Help:      "HTTP response size in bytes",
				Buckets:   prometheus.ExponentialBuckets(100, 10, 8),
			},
			[]string{"method", "path"},
		),

		// Backend client metrics
		backendCallsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: sanitizedName,
				Name:      "backend_calls_total",
				Help:      "Total number of backend service calls",
			},
			[]string{"service", "endpoint", "status"},
		),
		backendCallDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: sanitizedName,
				Name:      "backend_call_duration_seconds",
				Help:      "Backend service call duration in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"service", "endpoint"},
		),
		backendErrorsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: sanitizedName,
				Name:      "backend_errors_total",
				Help:      "Total number of backend service errors",
			},
			[]string{"service", "endpoint", "error_type"},
		),

		// Circuit breaker metrics
		circuitBreakerState: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: sanitizedName,
				Name:      "circuit_breaker_state",
				Help:      "Circuit breaker state (0=closed, 1=open, 2=half-open)",
			},
			[]string{"service"},
		),
		circuitBreakerOps: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: sanitizedName,
				Name:      "circuit_breaker_operations_total",
				Help:      "Total number of circuit breaker operations",
			},
			[]string{"service", "operation"},
		),

		// Rate limiter metrics
		rateLimitHitsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: sanitizedName,
				Name:      "rate_limit_hits_total",
				Help:      "Total number of rate limit hits",
			},
			[]string{"path", "client_ip"},
		),
		rateLimitAllowsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: sanitizedName,
				Name:      "rate_limit_allows_total",
				Help:      "Total number of allowed requests after rate limiting",
			},
			[]string{"path"},
		),

		// System metrics
		serviceUptime: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: sanitizedName,
				Name:      "service_uptime_seconds",
				Help:      "Service uptime in seconds",
			},
		),
		serviceHealth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: sanitizedName,
				Name:      "service_health",
				Help:      "Service health status (1=healthy, 0=unhealthy)",
			},
			[]string{"status"},
		),
		goroutines: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: sanitizedName,
				Name:      "goroutines",
				Help:      "Number of goroutines",
			},
		),
		memoryUsage: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: sanitizedName,
				Name:      "memory_usage_bytes",
				Help:      "Memory usage in bytes",
			},
		),
	}

	// Register all metrics
	prometheus.MustRegister(
		mc.httpRequestsTotal,
		mc.httpRequestDuration,
		mc.httpRequestsInFlight,
		mc.httpRequestSize,
		mc.httpResponseSize,
		mc.backendCallsTotal,
		mc.backendCallDuration,
		mc.backendErrorsTotal,
		mc.circuitBreakerState,
		mc.circuitBreakerOps,
		mc.rateLimitHitsTotal,
		mc.rateLimitAllowsTotal,
		mc.serviceUptime,
		mc.serviceHealth,
		mc.goroutines,
		mc.memoryUsage,
	)

	return mc
}

// HTTP Metrics Methods

// RecordHTTPRequest records an HTTP request
func (mc *MetricsCollector) RecordHTTPRequest(method, path, status string, duration time.Duration) {
	mc.httpRequestsTotal.WithLabelValues(method, path, status).Inc()
	mc.httpRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}

// RecordHTTPInFlight records in-flight HTTP requests
func (mc *MetricsCollector) RecordHTTPInFlight(path string, delta int) {
	if delta > 0 {
		mc.httpRequestsInFlight.WithLabelValues(path).Inc()
	} else {
		mc.httpRequestsInFlight.WithLabelValues(path).Dec()
	}
}

// RecordHTTPRequestSize records HTTP request size
func (mc *MetricsCollector) RecordHTTPRequestSize(method, path string, size int64) {
	mc.httpRequestSize.WithLabelValues(method, path).Observe(float64(size))
}

// RecordHTTPResponseSize records HTTP response size
func (mc *MetricsCollector) RecordHTTPResponseSize(method, path string, size int64) {
	mc.httpResponseSize.WithLabelValues(method, path).Observe(float64(size))
}

// Backend Client Metrics Methods

// RecordBackendCall records a backend service call
func (mc *MetricsCollector) RecordBackendCall(service, endpoint, status string, duration time.Duration) {
	mc.backendCallsTotal.WithLabelValues(service, endpoint, status).Inc()
	mc.backendCallDuration.WithLabelValues(service, endpoint).Observe(duration.Seconds())
}

// RecordBackendError records a backend service error
func (mc *MetricsCollector) RecordBackendError(service, endpoint, errorType string) {
	mc.backendErrorsTotal.WithLabelValues(service, endpoint, errorType).Inc()
}

// Circuit Breaker Metrics Methods

// RecordCircuitBreakerState records circuit breaker state
// state: 0=closed, 1=open, 2=half-open
func (mc *MetricsCollector) RecordCircuitBreakerState(service, state string) {
	var stateValue float64
	switch state {
	case "closed":
		stateValue = 0
	case "open":
		stateValue = 1
	case "half-open":
		stateValue = 2
	default:
		stateValue = -1
	}
	mc.circuitBreakerState.WithLabelValues(service).Set(stateValue)
}

// RecordCircuitBreakerOperation records a circuit breaker operation
func (mc *MetricsCollector) RecordCircuitBreakerOperation(service, operation string) {
	mc.circuitBreakerOps.WithLabelValues(service, operation).Inc()
}

// Rate Limiter Metrics Methods

// RecordRateLimitHit records a rate limit hit
func (mc *MetricsCollector) RecordRateLimitHit(path, clientIP string) {
	mc.rateLimitHitsTotal.WithLabelValues(path, clientIP).Inc()
}

// RecordRateLimitAllow records an allowed request
func (mc *MetricsCollector) RecordRateLimitAllow(path string) {
	mc.rateLimitAllowsTotal.WithLabelValues(path).Inc()
}

// System Metrics Methods

// RecordServiceUptime records service uptime
func (mc *MetricsCollector) RecordServiceUptime(uptime time.Duration) {
	mc.serviceUptime.Set(uptime.Seconds())
}

// RecordServiceHealth records service health
func (mc *MetricsCollector) RecordServiceHealth(healthy bool) {
	status := "unhealthy"
	value := 0.0
	if healthy {
		status = "healthy"
		value = 1.0
	}
	mc.serviceHealth.WithLabelValues(status).Set(value)
}

// StartSystemMetricsCollection starts collecting system metrics
func (mc *MetricsCollector) StartSystemMetricsCollection(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mc.collectSystemMetrics()
		case <-ctx.Done():
			return
		}
	}
}

// collectSystemMetrics collects system-level metrics
func (mc *MetricsCollector) collectSystemMetrics() {
	// Collect goroutine count
	mc.goroutines.Set(float64(runtime.NumGoroutine()))

	// Collect memory usage
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	mc.memoryUsage.Set(float64(m.Alloc))
}

// Handler returns the Prometheus HTTP handler
func (mc *MetricsCollector) Handler() http.Handler {
	return promhttp.Handler()
}
