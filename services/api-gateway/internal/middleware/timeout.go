package middleware

import (
	"context"
	"net/http"
	"time"

	"bitso-trading-platform/api-gateway/internal/logger"
)

// TimeoutMiddleware enforces request timeout
type TimeoutMiddleware struct {
	timeout time.Duration
	logger  *logger.Logger
}

// NewTimeoutMiddleware creates a new timeout middleware
func NewTimeoutMiddleware(timeout time.Duration, logger *logger.Logger) *TimeoutMiddleware {
	return &TimeoutMiddleware{
		timeout: timeout,
		logger:  logger.WithComponent("timeout"),
	}
}

// Handler returns the middleware handler function
func (m *TimeoutMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create context with timeout
		ctx, cancel := context.WithTimeout(r.Context(), m.timeout)
		defer cancel()

		// Create a channel to signal when handler completes
		done := make(chan struct{})

		// Wrap response writer to detect if response was written
		wrapped := &timeoutResponseWriter{
			ResponseWriter: w,
			written:        false,
		}

		// Run handler in goroutine
		go func() {
			next.ServeHTTP(wrapped, r.WithContext(ctx))
			close(done)
		}()

		// Wait for either completion or timeout
		select {
		case <-done:
			// Handler completed successfully
			return

		case <-ctx.Done():
			// Timeout or cancellation occurred
			if ctx.Err() == context.DeadlineExceeded {
				m.logger.Warn("Request timeout", map[string]interface{}{
					"method":  r.Method,
					"path":    r.URL.Path,
					"timeout": m.timeout,
				})

				// Only write error if response hasn't been written yet
				if !wrapped.written {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusGatewayTimeout)
					w.Write([]byte(`{"error":{"code":"TIMEOUT","message":"Request timeout exceeded"}}`))
				}
			}
			return
		}
	})
}

// timeoutResponseWriter wraps http.ResponseWriter to track if response was written
type timeoutResponseWriter struct {
	http.ResponseWriter
	written bool
}

// WriteHeader marks response as written
func (rw *timeoutResponseWriter) WriteHeader(statusCode int) {
	if !rw.written {
		rw.written = true
		rw.ResponseWriter.WriteHeader(statusCode)
	}
}

// Write marks response as written
func (rw *timeoutResponseWriter) Write(b []byte) (int, error) {
	rw.written = true
	return rw.ResponseWriter.Write(b)
}
