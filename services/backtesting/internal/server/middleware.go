package server

import (
	"net/http"
	"time"

	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/metrics"
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

// corsMiddleware adds CORS headers
func corsMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			
			// Handle preflight
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			
			next.ServeHTTP(w, r)
		})
	}
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

