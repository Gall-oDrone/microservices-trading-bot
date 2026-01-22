package middleware

import (
	"net/http"
	"strconv"
	"time"

	"bitso-trading-platform/api-gateway/internal/metrics"
)

// MetricsMiddleware records HTTP metrics
type MetricsMiddleware struct {
	metrics *metrics.MetricsCollector
}

// NewMetricsMiddleware creates a new metrics middleware
func NewMetricsMiddleware(metrics *metrics.MetricsCollector) *MetricsMiddleware {
	return &MetricsMiddleware{
		metrics: metrics,
	}
}

// Handler returns the middleware handler function
func (m *MetricsMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Increment in-flight requests
		m.metrics.RecordHTTPInFlight(r.URL.Path, 1)
		defer m.metrics.RecordHTTPInFlight(r.URL.Path, -1)

		// Record request size
		if r.ContentLength > 0 {
			m.metrics.RecordHTTPRequestSize(r.Method, r.URL.Path, r.ContentLength)
		}

		// Wrap response writer to capture status code and size
		wrapped := &metricsResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
			size:           0,
		}

		// Call next handler
		next.ServeHTTP(wrapped, r)

		// Calculate duration
		duration := time.Since(start)

		// Record metrics
		m.metrics.RecordHTTPRequest(
			r.Method,
			r.URL.Path,
			strconv.Itoa(wrapped.statusCode),
			duration,
		)

		// Record response size
		if wrapped.size > 0 {
			m.metrics.RecordHTTPResponseSize(r.Method, r.URL.Path, int64(wrapped.size))
		}
	})
}

// metricsResponseWriter wraps http.ResponseWriter to capture status code and size
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int
}

// WriteHeader captures the status code
func (rw *metricsResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

// Write captures the response size
func (rw *metricsResponseWriter) Write(b []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(b)
	rw.size += size
	return size, err
}
