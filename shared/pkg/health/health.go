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
	// StatusHealthy represents a healthy status
	StatusHealthy Status = "healthy"
	// StatusUnhealthy represents an unhealthy status
	StatusUnhealthy Status = "unhealthy"
	// StatusDegraded represents a degraded status
	StatusDegraded Status = "degraded"
)

// Check represents a health check
type Check struct {
	Name        string                 `json:"name"`
	Status      Status                 `json:"status"`
	Message     string                 `json:"message,omitempty"`
	Duration    time.Duration          `json:"duration,omitempty"`
	LastChecked time.Time              `json:"last_checked"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// HealthChecker defines the interface for health checks
type HealthChecker interface {
	Name() string
	Check(ctx context.Context) *Check
}

// HealthManager manages health checks
type HealthManager struct {
	checkers []HealthChecker
	logger   *log.Logger
	mu       sync.RWMutex
}

// NewHealthManager creates a new health manager
func NewHealthManager(logger *log.Logger) *HealthManager {
	if logger == nil {
		logger = log.New(log.Writer(), "[HEALTH] ", log.LstdFlags|log.Lshortfile)
	}

	return &HealthManager{
		checkers: make([]HealthChecker, 0),
		logger:   logger,
	}
}

// AddChecker adds a health checker
func (hm *HealthManager) AddChecker(checker HealthChecker) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.checkers = append(hm.checkers, checker)
}

// Check performs all health checks
func (hm *HealthManager) Check(ctx context.Context) map[string]*Check {
	hm.mu.RLock()
	checkers := make([]HealthChecker, len(hm.checkers))
	copy(checkers, hm.checkers)
	hm.mu.RUnlock()

	results := make(map[string]*Check)

	for _, checker := range checkers {
		start := time.Now()
		check := checker.Check(ctx)
		check.Duration = time.Since(start)
		check.LastChecked = time.Now()
		results[checker.Name()] = check
	}

	return results
}

// GetOverallStatus returns the overall health status
func (hm *HealthManager) GetOverallStatus(ctx context.Context) Status {
	checks := hm.Check(ctx)

	hasUnhealthy := false
	hasDegraded := false

	for _, check := range checks {
		switch check.Status {
		case StatusUnhealthy:
			hasUnhealthy = true
		case StatusDegraded:
			hasDegraded = true
		}
	}

	if hasUnhealthy {
		return StatusUnhealthy
	}

	if hasDegraded {
		return StatusDegraded
	}

	return StatusHealthy
}

// HTTPHandler returns an HTTP handler for health checks
func (hm *HealthManager) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		checks := hm.Check(ctx)
		overallStatus := hm.GetOverallStatus(ctx)

		response := map[string]interface{}{
			"status":    overallStatus,
			"timestamp": time.Now().UTC(),
			"checks":    checks,
		}

		w.Header().Set("Content-Type", "application/json")

		// Set HTTP status code based on overall status
		switch overallStatus {
		case StatusHealthy:
			w.WriteHeader(http.StatusOK)
		case StatusDegraded:
			w.WriteHeader(http.StatusOK)
		case StatusUnhealthy:
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(response)
	}
}

// LivenessHandler returns an HTTP handler for liveness checks
func (hm *HealthManager) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"status":    StatusHealthy,
			"timestamp": time.Now().UTC(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

// ReadinessHandler returns an HTTP handler for readiness checks
func (hm *HealthManager) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		overallStatus := hm.GetOverallStatus(ctx)

		response := map[string]interface{}{
			"status":    overallStatus,
			"timestamp": time.Now().UTC(),
		}

		w.Header().Set("Content-Type", "application/json")

		if overallStatus == StatusHealthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(response)
	}
}

// SimpleHealthChecker implements a simple health checker
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
func (shc *SimpleHealthChecker) Name() string {
	return shc.name
}

// Check performs the health check
func (shc *SimpleHealthChecker) Check(ctx context.Context) *Check {
	err := shc.checkFn(ctx)

	if err != nil {
		return &Check{
			Name:    shc.name,
			Status:  StatusUnhealthy,
			Message: err.Error(),
		}
	}

	return &Check{
		Name:   shc.name,
		Status: StatusHealthy,
	}
}

// DatabaseHealthChecker implements a database health checker
type DatabaseHealthChecker struct {
	name    string
	pingFn  func(ctx context.Context) error
	queryFn func(ctx context.Context) error
}

// NewDatabaseHealthChecker creates a new database health checker
func NewDatabaseHealthChecker(name string, pingFn func(ctx context.Context) error, queryFn func(ctx context.Context) error) *DatabaseHealthChecker {
	return &DatabaseHealthChecker{
		name:    name,
		pingFn:  pingFn,
		queryFn: queryFn,
	}
}

// Name returns the checker name
func (dhc *DatabaseHealthChecker) Name() string {
	return dhc.name
}

// Check performs the database health check
func (dhc *DatabaseHealthChecker) Check(ctx context.Context) *Check {
	// First, try to ping the database
	if err := dhc.pingFn(ctx); err != nil {
		return &Check{
			Name:    dhc.name,
			Status:  StatusUnhealthy,
			Message: fmt.Sprintf("ping failed: %v", err),
		}
	}

	// Then, try to execute a simple query
	if dhc.queryFn != nil {
		if err := dhc.queryFn(ctx); err != nil {
			return &Check{
				Name:    dhc.name,
				Status:  StatusDegraded,
				Message: fmt.Sprintf("query failed: %v", err),
			}
		}
	}

	return &Check{
		Name:   dhc.name,
		Status: StatusHealthy,
	}
}

// HTTPHealthChecker implements an HTTP health checker
type HTTPHealthChecker struct {
	name    string
	url     string
	client  *http.Client
	timeout time.Duration
}

// NewHTTPHealthChecker creates a new HTTP health checker
func NewHTTPHealthChecker(name, url string, timeout time.Duration) *HTTPHealthChecker {
	return &HTTPHealthChecker{
		name:    name,
		url:     url,
		client:  &http.Client{Timeout: timeout},
		timeout: timeout,
	}
}

// Name returns the checker name
func (hhc *HTTPHealthChecker) Name() string {
	return hhc.name
}

// Check performs the HTTP health check
func (hhc *HTTPHealthChecker) Check(ctx context.Context) *Check {
	req, err := http.NewRequestWithContext(ctx, "GET", hhc.url, nil)
	if err != nil {
		return &Check{
			Name:    hhc.name,
			Status:  StatusUnhealthy,
			Message: fmt.Sprintf("failed to create request: %v", err),
		}
	}

	resp, err := hhc.client.Do(req)
	if err != nil {
		return &Check{
			Name:    hhc.name,
			Status:  StatusUnhealthy,
			Message: fmt.Sprintf("request failed: %v", err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return &Check{
			Name:   hhc.name,
			Status: StatusHealthy,
		}
	}

	return &Check{
		Name:    hhc.name,
		Status:  StatusUnhealthy,
		Message: fmt.Sprintf("HTTP %d", resp.StatusCode),
	}
}
