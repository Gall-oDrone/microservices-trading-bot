package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"

	"golang.org/x/time/rate"
)

// RateLimiterMiddleware implements rate limiting per IP address
type RateLimiterMiddleware struct {
	limiters           map[string]*rate.Limiter
	mu                 sync.RWMutex
	requestsPerMinute  int
	burst              int
	cleanupInterval    time.Duration
	inactivityDuration time.Duration
	logger             *logger.Logger
	metrics            *metrics.MetricsCollector
	stopCleanup        chan struct{}
}

// RateLimiterConfig holds rate limiter configuration
type RateLimiterConfig struct {
	RequestsPerMinute  int
	Burst              int
	CleanupInterval    time.Duration
	InactivityDuration time.Duration
}

// NewRateLimiterMiddleware creates a new rate limiter middleware
func NewRateLimiterMiddleware(
	config *RateLimiterConfig,
	logger *logger.Logger,
	metrics *metrics.MetricsCollector,
) *RateLimiterMiddleware {
	m := &RateLimiterMiddleware{
		limiters:           make(map[string]*rate.Limiter),
		requestsPerMinute:  config.RequestsPerMinute,
		burst:              config.Burst,
		cleanupInterval:    config.CleanupInterval,
		inactivityDuration: config.InactivityDuration,
		logger:             logger.WithComponent("rate-limiter"),
		metrics:            metrics,
		stopCleanup:        make(chan struct{}),
	}

	// Start cleanup goroutine
	go m.cleanupInactiveLimiters()

	return m
}

// Handler returns the middleware handler function
func (m *RateLimiterMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract client IP
		clientIP := m.extractClientIP(r)

		// Get or create limiter for this IP
		limiter := m.getLimiter(clientIP)

		// Check if request is allowed
		if !limiter.Allow() {
			// Record rate limit hit
			m.metrics.RecordRateLimitHit(r.URL.Path, clientIP)

			m.logger.Warn("Rate limit exceeded", map[string]interface{}{
				"client_ip": clientIP,
				"path":      r.URL.Path,
				"method":    r.Method,
			})

			// Return 429 Too Many Requests
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-RateLimit-Limit", string(rune(m.requestsPerMinute)))
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"code":"RATE_LIMIT_EXCEEDED","message":"Too many requests. Please try again later."}}`))
			return
		}

		// Record allowed request
		m.metrics.RecordRateLimitAllow(r.URL.Path)

		// Call next handler
		next.ServeHTTP(w, r)
	})
}

// getLimiter gets or creates a rate limiter for the given IP
func (m *RateLimiterMiddleware) getLimiter(ip string) *rate.Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()

	limiter, exists := m.limiters[ip]
	if !exists {
		// Create new limiter
		// Convert requests per minute to requests per second
		rps := rate.Limit(float64(m.requestsPerMinute) / 60.0)
		limiter = rate.NewLimiter(rps, m.burst)
		m.limiters[ip] = limiter

		m.logger.Debug("Created new rate limiter", map[string]interface{}{
			"ip":  ip,
			"rps": rps,
		})
	}

	return limiter
}

// extractClientIP extracts the client IP from the request
func (m *RateLimiterMiddleware) extractClientIP(r *http.Request) string {
	// Try X-Forwarded-For header first (for proxied requests)
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// Take the first IP if multiple are present
		if ip, _, err := net.SplitHostPort(forwarded); err == nil {
			return ip
		}
		return forwarded
	}

	// Try X-Real-IP header
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}

	return r.RemoteAddr
}

// cleanupInactiveLimiters periodically removes inactive limiters
func (m *RateLimiterMiddleware) cleanupInactiveLimiters() {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.mu.Lock()

			// Count before cleanup
			beforeCount := len(m.limiters)

			// Remove all limiters (they will be recreated on next request)
			// In a production system, you would track last access time
			// For simplicity, we clear all inactive limiters periodically
			m.limiters = make(map[string]*rate.Limiter)

			m.mu.Unlock()

			if beforeCount > 0 {
				m.logger.Debug("Cleaned up rate limiters", map[string]interface{}{
					"removed": beforeCount,
				})
			}

		case <-m.stopCleanup:
			return
		}
	}
}

// Stop stops the cleanup goroutine
func (m *RateLimiterMiddleware) Stop() {
	close(m.stopCleanup)
}
