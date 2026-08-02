package health

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"bitso-trading-platform/data-collector/internal/clock"
)

// Checker reports health based on trade staleness (dead-man's switch).
type Checker struct {
	clock      clock.Clock
	staleAfter time.Duration

	mu          sync.RWMutex
	lastTradeAt time.Time
	hasTrade    bool
	startedAt   time.Time
}

// NewChecker creates a health checker. Until the first trade, health is
// considered starting (healthy) for up to staleAfter from process start so
// cold starts do not immediately fail; after that window with no trades,
// or after a trade then silence longer than staleAfter, it is unhealthy.
func NewChecker(clk clock.Clock, staleAfter time.Duration) *Checker {
	if clk == nil {
		clk = clock.RealClock{}
	}
	now := clk.Now()
	return &Checker{
		clock:      clk,
		staleAfter: staleAfter,
		startedAt:  now,
	}
}

// RecordTrade marks that a trade was received at the given time.
func (c *Checker) RecordTrade(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastTradeAt = at.UTC()
	c.hasTrade = true
}

// Status returns whether the collector is healthy and a reason.
func (c *Checker) Status() (healthy bool, reason string, age time.Duration) {
	now := c.clock.Now()
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.hasTrade {
		age = now.Sub(c.startedAt)
		if age > c.staleAfter {
			return false, "no trades received since start", age
		}
		return true, "waiting for first trade", age
	}

	age = now.Sub(c.lastTradeAt)
	if age > c.staleAfter {
		return false, "no trade received within staleness window", age
	}
	return true, "ok", age
}

// Handler returns an HTTP handler for /healthz.
func (c *Checker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		healthy, reason, age := c.Status()
		body := map[string]interface{}{
			"status":             map[bool]string{true: "healthy", false: "unhealthy"}[healthy],
			"reason":             reason,
			"seconds_since_last": age.Seconds(),
			"stale_after_seconds": c.staleAfter.Seconds(),
		}
		w.Header().Set("Content-Type", "application/json")
		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_ = json.NewEncoder(w).Encode(body)
	}
}
