package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
)

// CircuitState represents the state of a circuit breaker
type CircuitState string

const (
	// StateClosed allows requests to pass through
	StateClosed CircuitState = "closed"
	// StateOpen rejects all requests
	StateOpen CircuitState = "open"
	// StateHalfOpen allows a limited number of requests to test if the service recovered
	StateHalfOpen CircuitState = "half-open"
)

// CircuitBreakerMiddleware implements circuit breaker pattern per service
type CircuitBreakerMiddleware struct {
	breakers map[string]*circuitBreaker
	mu       sync.RWMutex
	config   *CircuitBreakerConfig
	logger   *logger.Logger
	metrics  *metrics.MetricsCollector
}

// CircuitBreakerConfig holds circuit breaker configuration
type CircuitBreakerConfig struct {
	Threshold   int           // Number of failures before opening
	Timeout     time.Duration // Time to wait before attempting to close
	MaxRequests uint32        // Maximum requests allowed in half-open state
	Interval    time.Duration // Time window for counting failures
}

// circuitBreaker represents a circuit breaker for a single service
type circuitBreaker struct {
	state            CircuitState
	failures         int
	lastFailTime     time.Time
	lastStateChange  time.Time
	halfOpenRequests uint32
	mu               sync.RWMutex
	config           *CircuitBreakerConfig
}

// NewCircuitBreakerMiddleware creates a new circuit breaker middleware
func NewCircuitBreakerMiddleware(
	config *CircuitBreakerConfig,
	logger *logger.Logger,
	metrics *metrics.MetricsCollector,
) *CircuitBreakerMiddleware {
	return &CircuitBreakerMiddleware{
		breakers: make(map[string]*circuitBreaker),
		config:   config,
		logger:   logger.WithComponent("circuit-breaker"),
		metrics:  metrics,
	}
}

// Handler returns the middleware handler function
func (m *CircuitBreakerMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract service name from path
		service := m.extractServiceName(r.URL.Path)
		if service == "" {
			// No service identified, pass through
			next.ServeHTTP(w, r)
			return
		}

		// Get or create circuit breaker for this service
		breaker := m.getBreaker(service)

		// Check if circuit allows the request
		if !breaker.allowRequest() {
			m.logger.Warn("Circuit breaker open", map[string]interface{}{
				"service": service,
				"path":    r.URL.Path,
			})

			m.metrics.RecordCircuitBreakerOperation(service, "reject")

			// Return 503 Service Unavailable
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"code":"SERVICE_UNAVAILABLE","message":"Service temporarily unavailable. Circuit breaker is open."}}`))
			return
		}

		// Wrap response writer to capture status code
		wrapped := &circuitBreakerResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Call next handler
		next.ServeHTTP(wrapped, r)

		// Record success or failure based on status code
		if wrapped.statusCode >= 500 {
			breaker.recordFailure()
			m.logger.Debug("Circuit breaker recorded failure", map[string]interface{}{
				"service": service,
				"status":  wrapped.statusCode,
			})
		} else {
			breaker.recordSuccess()
		}

		// Update metrics
		m.metrics.RecordCircuitBreakerState(service, string(breaker.getState()))
	})
}

// getBreaker gets or creates a circuit breaker for the given service
func (m *CircuitBreakerMiddleware) getBreaker(service string) *circuitBreaker {
	m.mu.Lock()
	defer m.mu.Unlock()

	breaker, exists := m.breakers[service]
	if !exists {
		breaker = &circuitBreaker{
			state:           StateClosed,
			failures:        0,
			lastStateChange: time.Now(),
			config:          m.config,
		}
		m.breakers[service] = breaker

		m.logger.Info("Created circuit breaker", map[string]interface{}{
			"service": service,
		})
	}

	return breaker
}

// extractServiceName extracts the service name from the URL path
func (m *CircuitBreakerMiddleware) extractServiceName(path string) string {
	// Expected format: /api/v1/{service}/...
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "api" && parts[1] == "v1" {
		return parts[2]
	}
	return ""
}

// allowRequest checks if the circuit breaker allows the request
func (cb *circuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case StateClosed:
		// Always allow in closed state
		return true

	case StateOpen:
		// Check if timeout has elapsed
		if now.Sub(cb.lastStateChange) > cb.config.Timeout {
			// Transition to half-open
			cb.state = StateHalfOpen
			cb.halfOpenRequests = 0
			cb.lastStateChange = now
			return true
		}
		return false

	case StateHalfOpen:
		// Allow limited requests in half-open state
		if cb.halfOpenRequests < cb.config.MaxRequests {
			cb.halfOpenRequests++
			return true
		}
		return false

	default:
		return true
	}
}

// recordSuccess records a successful request
func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	// Reset failure count
	if now.Sub(cb.lastFailTime) > cb.config.Interval {
		cb.failures = 0
	}

	// Transition from half-open to closed
	if cb.state == StateHalfOpen {
		cb.state = StateClosed
		cb.failures = 0
		cb.lastStateChange = now
	}
}

// recordFailure records a failed request
func (cb *circuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	// Reset count if interval has passed
	if now.Sub(cb.lastFailTime) > cb.config.Interval {
		cb.failures = 0
	}

	cb.failures++
	cb.lastFailTime = now

	// Open circuit if threshold is reached
	if cb.failures >= cb.config.Threshold {
		if cb.state != StateOpen {
			cb.state = StateOpen
			cb.lastStateChange = now
		}
	}
}

// getState returns the current state of the circuit breaker
func (cb *circuitBreaker) getState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// circuitBreakerResponseWriter wraps http.ResponseWriter to capture status code
type circuitBreakerResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code
func (rw *circuitBreakerResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

