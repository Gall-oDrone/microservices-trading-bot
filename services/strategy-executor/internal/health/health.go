package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Status represents the health status of a component
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusUnhealthy Status = "unhealthy"
	StatusDegraded  Status = "degraded"
	StatusUnknown   Status = "unknown"
)

// Check represents a health check
type Check interface {
	Name() string
	Check(ctx context.Context) CheckResult
}

// CheckResult represents the result of a health check
type CheckResult struct {
	Name      string                 `json:"name"`
	Status    Status                 `json:"status"`
	Message   string                 `json:"message,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Duration  time.Duration          `json:"duration"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// Health represents the overall health status
type Health struct {
	Status    Status                 `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Checks    map[string]CheckResult `json:"checks"`
	Version   string                 `json:"version,omitempty"`
	Service   string                 `json:"service,omitempty"`
}

// Manager manages health checks
type Manager struct {
	checks  map[string]Check
	timeout time.Duration
	version string
	service string
	mu      sync.RWMutex
}

// NewManager creates a new health manager
func NewManager(service, version string, timeout time.Duration) *Manager {
	return &Manager{
		checks:  make(map[string]Check),
		timeout: timeout,
		version: version,
		service: service,
	}
}

// RegisterCheck registers a health check
func (m *Manager) RegisterCheck(check Check) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checks[check.Name()] = check
}

// UnregisterCheck unregisters a health check
func (m *Manager) UnregisterCheck(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.checks, name)
}

// GetHealth returns the current health status
func (m *Manager) GetHealth(ctx context.Context) *Health {
	m.mu.RLock()
	checks := make(map[string]Check)
	for name, check := range m.checks {
		checks[name] = check
	}
	m.mu.RUnlock()

	results := make(map[string]CheckResult)
	overallStatus := StatusHealthy

	// Run all checks concurrently
	var wg sync.WaitGroup
	var mu sync.Mutex

	for name, check := range checks {
		wg.Add(1)
		go func(name string, check Check) {
			defer wg.Done()

			checkCtx, cancel := context.WithTimeout(ctx, m.timeout)
			defer cancel()

			start := time.Now()
			result := check.Check(checkCtx)
			result.Duration = time.Since(start)

			mu.Lock()
			results[name] = result

			// Determine overall status
			switch result.Status {
			case StatusUnhealthy:
				overallStatus = StatusUnhealthy
			case StatusDegraded:
				if overallStatus != StatusUnhealthy {
					overallStatus = StatusDegraded
				}
			case StatusUnknown:
				if overallStatus == StatusHealthy {
					overallStatus = StatusDegraded
				}
			}
			mu.Unlock()
		}(name, check)
	}

	wg.Wait()

	return &Health{
		Status:    overallStatus,
		Timestamp: time.Now(),
		Checks:    results,
		Version:   m.version,
		Service:   m.service,
	}
}

// IsHealthy returns true if all checks are healthy
func (m *Manager) IsHealthy(ctx context.Context) bool {
	health := m.GetHealth(ctx)
	return health.Status == StatusHealthy
}

// SimpleCheck is a simple health check implementation
type SimpleCheck struct {
	name    string
	checkFn func(ctx context.Context) error
}

// NewSimpleCheck creates a new simple health check
func NewSimpleCheck(name string, checkFn func(ctx context.Context) error) *SimpleCheck {
	return &SimpleCheck{
		name:    name,
		checkFn: checkFn,
	}
}

// Name returns the check name
func (c *SimpleCheck) Name() string {
	return c.name
}

// Check performs the health check
func (c *SimpleCheck) Check(ctx context.Context) CheckResult {
	err := c.checkFn(ctx)

	result := CheckResult{
		Name:      c.name,
		Timestamp: time.Now(),
	}

	if err != nil {
		result.Status = StatusUnhealthy
		result.Message = err.Error()
	} else {
		result.Status = StatusHealthy
	}

	return result
}

// HTTPCheck is a health check that makes an HTTP request
type HTTPCheck struct {
	name    string
	url     string
	timeout time.Duration
}

// NewHTTPCheck creates a new HTTP health check
func NewHTTPCheck(name, url string, timeout time.Duration) *HTTPCheck {
	return &HTTPCheck{
		name:    name,
		url:     url,
		timeout: timeout,
	}
}

// Name returns the check name
func (c *HTTPCheck) Name() string {
	return c.name
}

// Check performs the HTTP health check
func (c *HTTPCheck) Check(ctx context.Context) CheckResult {
	result := CheckResult{
		Name:      c.name,
		Timestamp: time.Now(),
	}

	// Create a timeout context
	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Make HTTP request
	req, err := http.NewRequestWithContext(checkCtx, "GET", c.url, nil)
	if err != nil {
		result.Status = StatusUnhealthy
		result.Message = fmt.Sprintf("Failed to create request: %v", err)
		return result
	}

	client := &http.Client{Timeout: c.timeout}
	resp, err := client.Do(req)
	if err != nil {
		result.Status = StatusUnhealthy
		result.Message = fmt.Sprintf("HTTP request failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Status = StatusHealthy
		result.Details = map[string]interface{}{
			"status_code": resp.StatusCode,
		}
	} else {
		result.Status = StatusUnhealthy
		result.Message = fmt.Sprintf("HTTP status: %d", resp.StatusCode)
		result.Details = map[string]interface{}{
			"status_code": resp.StatusCode,
		}
	}

	return result
}

// DatabaseCheck is a health check for database connectivity
type DatabaseCheck struct {
	name    string
	pingFn  func(ctx context.Context) error
	queryFn func(ctx context.Context) error
}

// NewDatabaseCheck creates a new database health check
func NewDatabaseCheck(name string, pingFn, queryFn func(ctx context.Context) error) *DatabaseCheck {
	return &DatabaseCheck{
		name:    name,
		pingFn:  pingFn,
		queryFn: queryFn,
	}
}

// Name returns the check name
func (c *DatabaseCheck) Name() string {
	return c.name
}

// Check performs the database health check
func (c *DatabaseCheck) Check(ctx context.Context) CheckResult {
	result := CheckResult{
		Name:      c.name,
		Timestamp: time.Now(),
	}

	// Test ping
	if c.pingFn != nil {
		if err := c.pingFn(ctx); err != nil {
			result.Status = StatusUnhealthy
			result.Message = fmt.Sprintf("Database ping failed: %v", err)
			return result
		}
	}

	// Test query
	if c.queryFn != nil {
		if err := c.queryFn(ctx); err != nil {
			result.Status = StatusDegraded
			result.Message = fmt.Sprintf("Database query failed: %v", err)
			return result
		}
	}

	result.Status = StatusHealthy
	return result
}

// KafkaCheck is a health check for Kafka connectivity
type KafkaCheck struct {
	name      string
	pingFn    func(ctx context.Context) error
	produceFn func(ctx context.Context) error
}

// NewKafkaCheck creates a new Kafka health check
func NewKafkaCheck(name string, pingFn, produceFn func(ctx context.Context) error) *KafkaCheck {
	return &KafkaCheck{
		name:      name,
		pingFn:    pingFn,
		produceFn: produceFn,
	}
}

// Name returns the check name
func (c *KafkaCheck) Name() string {
	return c.name
}

// Check performs the Kafka health check
func (c *KafkaCheck) Check(ctx context.Context) CheckResult {
	result := CheckResult{
		Name:      c.name,
		Timestamp: time.Now(),
	}

	// Test connectivity
	if c.pingFn != nil {
		if err := c.pingFn(ctx); err != nil {
			result.Status = StatusUnhealthy
			result.Message = fmt.Sprintf("Kafka connectivity failed: %v", err)
			return result
		}
	}

	// Test produce capability
	if c.produceFn != nil {
		if err := c.produceFn(ctx); err != nil {
			result.Status = StatusDegraded
			result.Message = fmt.Sprintf("Kafka produce failed: %v", err)
			return result
		}
	}

	result.Status = StatusHealthy
	return result
}

// ToJSON converts health to JSON
func (h *Health) ToJSON() ([]byte, error) {
	return json.Marshal(h)
}

// ToJSON converts check result to JSON
func (r *CheckResult) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// IsHealthy returns true if the health status is healthy
func (h *Health) IsHealthy() bool {
	return h.Status == StatusHealthy
}

// IsDegraded returns true if the health status is degraded
func (h *Health) IsDegraded() bool {
	return h.Status == StatusDegraded
}

// IsUnhealthy returns true if the health status is unhealthy
func (h *Health) IsUnhealthy() bool {
	return h.Status == StatusUnhealthy
}

// GetFailedChecks returns a list of failed checks
func (h *Health) GetFailedChecks() []CheckResult {
	var failed []CheckResult
	for _, check := range h.Checks {
		if check.Status == StatusUnhealthy {
			failed = append(failed, check)
		}
	}
	return failed
}

// GetDegradedChecks returns a list of degraded checks
func (h *Health) GetDegradedChecks() []CheckResult {
	var degraded []CheckResult
	for _, check := range h.Checks {
		if check.Status == StatusDegraded {
			degraded = append(degraded, check)
		}
	}
	return degraded
}
