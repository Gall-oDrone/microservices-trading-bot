package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	// CircuitClosed means the circuit is closed and requests are allowed
	CircuitClosed CircuitState = iota
	// CircuitOpen means the circuit is open and requests are blocked
	CircuitOpen
	// CircuitHalfOpen means the circuit is half-open and testing requests
	CircuitHalfOpen
)

// CircuitBreaker implements a circuit breaker pattern
type CircuitBreaker struct {
	mu sync.RWMutex

	// Configuration
	failureThreshold int
	successThreshold int
	timeout          time.Duration

	// State
	state         CircuitState
	failureCount  int
	successCount  int
	lastFailTime  time.Time
	lastStateTime time.Time
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(failureThreshold, successThreshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		timeout:          timeout,
		state:            CircuitClosed,
	}
}

// Execute executes a function with circuit breaker protection
func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check if circuit is open
	if cb.state == CircuitOpen {
		if time.Since(cb.lastFailTime) >= cb.timeout {
			// Move to half-open state
			cb.state = CircuitHalfOpen
			cb.successCount = 0
			cb.lastStateTime = time.Now()
		} else {
			return fmt.Errorf("circuit breaker is open")
		}
	}

	// Execute the function
	err := fn()

	// Update state based on result
	if err != nil {
		cb.failureCount++
		cb.lastFailTime = time.Now()

		if cb.state == CircuitHalfOpen || cb.failureCount >= cb.failureThreshold {
			cb.state = CircuitOpen
			cb.lastStateTime = time.Now()
		}
	} else {
		cb.successCount++
		cb.failureCount = 0

		if cb.state == CircuitHalfOpen && cb.successCount >= cb.successThreshold {
			cb.state = CircuitClosed
			cb.lastStateTime = time.Now()
		}
	}

	return err
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats returns circuit breaker statistics
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"state":           cb.state,
		"failure_count":   cb.failureCount,
		"success_count":   cb.successCount,
		"last_fail_time":  cb.lastFailTime,
		"last_state_time": cb.lastStateTime,
	}
}

// CircuitBreakerMiddleware creates a circuit breaker middleware
func CircuitBreakerMiddleware(failureThreshold, successThreshold int, timeout time.Duration) func(http.Handler) http.Handler {
	cb := NewCircuitBreaker(failureThreshold, successThreshold, timeout)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var err error

			// Execute the next handler with circuit breaker protection
			cbErr := cb.Execute(func() error {
				// Create a response writer that captures the status code
				wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

				// Call the next handler
				next.ServeHTTP(wrapped, r)

				// Check if the response indicates an error
				if wrapped.statusCode >= 500 {
					return fmt.Errorf("server error: %d", wrapped.statusCode)
				}

				return nil
			})

			if cbErr != nil {
				// Circuit breaker is open or there was an error
				http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
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
