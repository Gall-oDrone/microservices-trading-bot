package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

// HealthChecker is an interface for health checkers
type HealthChecker interface {
	Name() string
	Check(ctx context.Context) error
}

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
	checkers []HealthChecker
	mu       sync.RWMutex
	ready    bool
	readyMu  sync.RWMutex
	logger   *log.Logger
}

// NewHealthManager creates a new health manager
// logger parameter is optional (can be nil)
func NewHealthManager(logger *log.Logger) *HealthManager {
	return &HealthManager{
		checks:   make(map[string]Check),
		checkers: make([]HealthChecker, 0),
		ready:    true, // Default to ready
		logger:   logger,
	}
}

// NewHealthManagerWithConfig creates a new health manager with service info
func NewHealthManagerWithConfig(service, version string) *HealthManager {
	return &HealthManager{
		service:  service,
		version:  version,
		checks:   make(map[string]Check),
		checkers: make([]HealthChecker, 0),
		ready:    true,
	}
}

// RegisterCheck registers a health check function
func (h *HealthManager) RegisterCheck(name string, check Check) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = check
}

// AddChecker adds a health checker
func (h *HealthManager) AddChecker(checker HealthChecker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers = append(h.checkers, checker)
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

// Check runs all registered health checks and returns the results
func (h *HealthManager) Check(ctx context.Context) map[string]CheckResult {
	h.mu.RLock()
	checks := make(map[string]Check)
	for name, check := range h.checks {
		checks[name] = check
	}
	checkers := make([]HealthChecker, len(h.checkers))
	copy(checkers, h.checkers)
	h.mu.RUnlock()

	results := make(map[string]CheckResult)

	// Run function-based checks
	for name, check := range checks {
		result := CheckResult{
			Name:      name,
			Timestamp: time.Now(),
		}

		if err := check(ctx); err != nil {
			result.Status = StatusUnhealthy
			result.Message = err.Error()
		} else {
			result.Status = StatusHealthy
		}

		results[name] = result
	}

	// Run interface-based checkers
	for _, checker := range checkers {
		result := CheckResult{
			Name:      checker.Name(),
			Timestamp: time.Now(),
		}

		if err := checker.Check(ctx); err != nil {
			result.Status = StatusUnhealthy
			result.Message = err.Error()
		} else {
			result.Status = StatusHealthy
		}

		results[checker.Name()] = result
	}

	return results
}

// GetOverallStatus returns the overall health status based on all checks
func (h *HealthManager) GetOverallStatus(ctx context.Context) Status {
	checks := h.Check(ctx)

	for _, result := range checks {
		if result.Status == StatusUnhealthy {
			return StatusUnhealthy
		}
		if result.Status == StatusDegraded {
			return StatusDegraded
		}
	}

	return StatusHealthy
}

// SimpleHealthChecker is a simple health checker implementation
type SimpleHealthChecker struct {
	name    string
	checkFn func(ctx context.Context) error
}

// NewSimpleHealthChecker creates a new simple health checker
func NewSimpleHealthChecker(name string, checkFn func(ctx context.Context) error) *SimpleHealthChecker {
	return &SimpleHealthChecker{
		name:    name,
		checkFn: checkFn,
	}
}

// Name returns the checker name
func (c *SimpleHealthChecker) Name() string {
	return c.name
}

// Check performs the health check
func (c *SimpleHealthChecker) Check(ctx context.Context) error {
	return c.checkFn(ctx)
}

// HTTPHealthChecker checks health via HTTP endpoint
type HTTPHealthChecker struct {
	name    string
	url     string
	timeout time.Duration
	client  *http.Client
}

// NewHTTPHealthChecker creates a new HTTP health checker
func NewHTTPHealthChecker(name, url string, timeout time.Duration) *HTTPHealthChecker {
	return &HTTPHealthChecker{
		name:    name,
		url:     url,
		timeout: timeout,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Name returns the checker name
func (c *HTTPHealthChecker) Name() string {
	return c.name
}

// Check performs the HTTP health check
func (c *HTTPHealthChecker) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unhealthy status code: %d", resp.StatusCode)
	}

	return nil
}
