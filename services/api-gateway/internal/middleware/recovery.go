package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"bitso-trading-platform/api-gateway/internal/logger"
)

// RecoveryMiddleware recovers from panics and returns a 500 error
type RecoveryMiddleware struct {
	logger *logger.Logger
}

// NewRecoveryMiddleware creates a new recovery middleware
func NewRecoveryMiddleware(logger *logger.Logger) *RecoveryMiddleware {
	return &RecoveryMiddleware{
		logger: logger.WithComponent("recovery"),
	}
}

// Handler returns the middleware handler function
func (m *RecoveryMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// Log the panic with stack trace
				m.logger.Error("Panic recovered", map[string]interface{}{
					"error":       fmt.Sprintf("%v", err),
					"method":      r.Method,
					"path":        r.URL.Path,
					"remote_addr": r.RemoteAddr,
					"stack_trace": string(debug.Stack()),
				})

				// Return 500 Internal Server Error
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"Internal server error occurred"}}`))
			}
		}()

		// Call next handler
		next.ServeHTTP(w, r)
	})
}
