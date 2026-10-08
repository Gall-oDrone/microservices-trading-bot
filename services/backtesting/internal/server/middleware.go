package server

import (
	"net/http"
	"os"
	"time"

	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/metrics"
	"bitso-trading-platform/shared/pkg/httpcors"
)

// loggingMiddleware logs HTTP requests
func loggingMiddleware(log logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Create response wrapper to capture status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Process request
			next.ServeHTTP(wrapped, r)

			// Log request
			duration := time.Since(start)
			log.Info("HTTP request", map[string]interface{}{
				"method":      r.Method,
				"path":        r.URL.Path,
				"status":      wrapped.statusCode,
				"duration_ms": duration.Milliseconds(),
				"remote_addr": r.RemoteAddr,
			})
		})
	}
}

// metricsMiddleware records metrics for HTTP requests
func metricsMiddleware(collector *metrics.MetricsCollector) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)
			// Record metrics (would need additional metric definitions)
			_ = duration // Use duration for metrics
		})
	}
}

// recoveryMiddleware recovers from panics
func recoveryMiddleware(log logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					log.Error("Panic recovered", map[string]interface{}{
						"error": err,
						"path":  r.URL.Path,
					})

					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"success":false,"error":{"code":"INTERNAL_ERROR","message":"Internal server error"}}`))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// corsMiddleware applies the platform CORS allowlist (CORS_ALLOWED_ORIGINS,
// shared/pkg/httpcors). Unset or invalid: no CORS headers, same-origin only.
func corsMiddleware(log logger.Logger) func(http.Handler) http.Handler {
	cors, err := httpcors.FromEnv(os.Getenv)
	if err != nil {
		log.Warn("CORS disabled", map[string]interface{}{"error": err.Error()})
		cors = nil
	} else if len(cors.Origins()) > 0 {
		log.Info("CORS allowed origins", map[string]interface{}{"origins": cors.Origins()})
	}
	return cors.Middleware
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
