package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Status represents the health status
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusUnhealthy Status = "unhealthy"
	StatusDegraded  Status = "degraded"
)

// Check represents a health check function
type Check func(ctx context.Context) error

// CheckResult represents the result of a health check
type CheckResult struct {
	Name      string    `json:"name"`
	Status    Status    `json:"status"`
	Message   string    `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    Status                 `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Service   string                 `json:"service,omitempty"`
	Version   string                 `json:"version,omitempty"`
	Checks    map[string]CheckResult `json:"checks,omitempty"`
}

// HealthManager manages health checks
type HealthManager struct {
	service  string
	version  string
	checks   map[string]Check
	mu       sync.RWMutex
	ready    bool
	readyMu  sync.RWMutex
}

// NewHealthManager creates a new health manager
func NewHealthManager(service, version string) *HealthManager {
	return &HealthManager{
		service: service,
		version: version,
		checks:  make(map[string]Check),
		ready:   false,
	}
}

// RegisterCheck registers a health check
func (h *HealthManager) RegisterCheck(name string, check Check) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = check
}

// SetReady sets the ready status
func (h *HealthManager) SetReady(ready bool) {
	h.readyMu.Lock()
	defer h.readyMu.Unlock()
	h.ready = ready
}

// IsReady returns the ready status
func (h *HealthManager) IsReady() bool {
	h.readyMu.RLock()
	defer h.readyMu.RUnlock()
	return h.ready
}

// GetHealth returns the current health status
func (h *HealthManager) GetHealth(ctx context.Context) *HealthResponse {
	h.mu.RLock()
	checks := make(map[string]Check)
	for name, check := range h.checks {
		checks[name] = check
	}
	h.mu.RUnlock()

	results := make(map[string]CheckResult)
	overallStatus := StatusHealthy

	for name, check := range checks {
		result := CheckResult{
			Name:      name,
			Timestamp: time.Now(),
		}

		if err := check(ctx); err != nil {
			result.Status = StatusUnhealthy
			result.Message = err.Error()
			overallStatus = StatusUnhealthy
		} else {
			result.Status = StatusHealthy
		}

		results[name] = result
	}

	return &HealthResponse{
		Status:    overallStatus,
		Timestamp: time.Now(),
		Service:   h.service,
		Version:   h.version,
		Checks:    results,
	}
}

// HTTPHandler returns an http.HandlerFunc for the health endpoint
func (h *HealthManager) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		health := h.GetHealth(ctx)

		w.Header().Set("Content-Type", "application/json")

		if health.Status == StatusHealthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(health)
	}
}

// LivenessHandler returns an http.HandlerFunc for the liveness probe
func (h *HealthManager) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "alive",
		})
	}
}

// ReadinessHandler returns an http.HandlerFunc for the readiness probe
func (h *HealthManager) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if h.IsReady() {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "ready",
			})
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "not ready",
			})
		}
	}
}
