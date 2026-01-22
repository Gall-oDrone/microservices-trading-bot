package middleware

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter
type RateLimiter struct {
	requests chan time.Time
	mu       sync.RWMutex
	limit    int
	window   time.Duration
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(chan time.Time, limit),
		limit:    limit,
		window:   window,
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

// Allow checks if a request is allowed
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Check if we have capacity
	if len(rl.requests) < rl.limit {
		select {
		case rl.requests <- now:
			return true
		default:
			return false
		}
	}

	// Check if oldest request is outside the window
	select {
	case oldest := <-rl.requests:
		if now.Sub(oldest) >= rl.window {
			rl.requests <- now
			return true
		}
		// Put it back
		rl.requests <- oldest
		return false
	default:
		return false
	}
}

// cleanup removes old requests from the bucket
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.window / 2)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()

		// Remove old requests
		for {
			select {
			case oldest := <-rl.requests:
				if now.Sub(oldest) < rl.window {
					// Put it back
					rl.requests <- oldest
					rl.mu.Unlock()
					return
				}
				// Remove old request
			default:
				rl.mu.Unlock()
				return
			}
		}
	}
}

// RateLimitMiddleware creates a rate limiting middleware
func RateLimitMiddleware(limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(limit, window)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IPRateLimiter implements per-IP rate limiting
type IPRateLimiter struct {
	limiters map[string]*RateLimiter
	mu       sync.RWMutex
	limit    int
	window   time.Duration
}

// NewIPRateLimiter creates a new IP-based rate limiter
func NewIPRateLimiter(limit int, window time.Duration) *IPRateLimiter {
	return &IPRateLimiter{
		limiters: make(map[string]*RateLimiter),
		limit:    limit,
		window:   window,
	}
}

// GetLimiter gets or creates a rate limiter for an IP
func (irl *IPRateLimiter) GetLimiter(ip string) *RateLimiter {
	irl.mu.Lock()
	defer irl.mu.Unlock()

	limiter, exists := irl.limiters[ip]
	if !exists {
		limiter = NewRateLimiter(irl.limit, irl.window)
		irl.limiters[ip] = limiter
	}

	return limiter
}

// IPRateLimitMiddleware creates a per-IP rate limiting middleware
func IPRateLimitMiddleware(limit int, window time.Duration) func(http.Handler) http.Handler {
	ipLimiter := NewIPRateLimiter(limit, window)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)
			limiter := ipLimiter.GetLimiter(ip)

			if !limiter.Allow() {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Use RemoteAddr as fallback
	return r.RemoteAddr
}
