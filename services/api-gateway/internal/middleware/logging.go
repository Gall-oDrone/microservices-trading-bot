package middleware

import (
	"context"
	"net/http"
	"time"

	"bitso-trading-platform/api-gateway/internal/logger"

	"github.com/google/uuid"
)

// LoggingMiddleware logs HTTP requests and responses
type LoggingMiddleware struct {
	logger *logger.Logger
}

// NewLoggingMiddleware creates a new logging middleware
func NewLoggingMiddleware(logger *logger.Logger) *LoggingMiddleware {
	return &LoggingMiddleware{
		logger: logger,
	}
}

// Handler returns the middleware handler function
func (m *LoggingMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Generate request ID if not present
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Create logger with request context
		requestLogger := m.logger.WithFields(map[string]interface{}{
			"request_id":   requestID,
			"method":       r.Method,
			"path":         r.URL.Path,
			"remote_addr":  r.RemoteAddr,
			"user_agent":   r.UserAgent(),
			"content_type": r.Header.Get("Content-Type"),
		})

		// Log request start
		requestLogger.Debug("Request started", nil)

		// Wrap response writer to capture status code and size
		wrapped := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
			size:           0,
		}

		// Add request ID to response header
		wrapped.Header().Set("X-Request-ID", requestID)

		// Store request ID in context for other middleware
		ctx := r.Context()
		ctx = contextWithRequestID(ctx, requestID)
		r = r.WithContext(ctx)

		// Call next handler
		next.ServeHTTP(wrapped, r)

		// Calculate duration
		duration := time.Since(start)

		// Log request completion
		logFields := map[string]interface{}{
			"status":   wrapped.statusCode,
			"size":     wrapped.size,
			"duration": duration,
		}

		// Choose log level based on status code
		if wrapped.statusCode >= 500 {
			requestLogger.Error("Request completed with server error", logFields)
		} else if wrapped.statusCode >= 400 {
			requestLogger.Warn("Request completed with client error", logFields)
		} else {
			requestLogger.Info("Request completed", logFields)
		}
	})
}

// responseWriter wraps http.ResponseWriter to capture status code and size
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int
}

// WriteHeader captures the status code
func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

// Write captures the response size
func (rw *responseWriter) Write(b []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(b)
	rw.size += size
	return size, err
}

// Context key for request ID
type contextKey string

const requestIDKey contextKey = "request_id"

// contextWithRequestID adds request ID to context
func contextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext extracts request ID from context
func RequestIDFromContext(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}
	return ""
}
