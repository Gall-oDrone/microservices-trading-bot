package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
)

// StrategyExecutorClient defines the interface for strategy executor service
type StrategyExecutorClient interface {
	// Service status
	GetStatus(ctx context.Context) (*ServiceStatus, error)

	// Strategy endpoints
	ListStrategies(ctx context.Context) ([]*Strategy, error)
	GetStrategy(ctx context.Context, name string) (*Strategy, error)
	StartStrategy(ctx context.Context, name string) error
	StopStrategy(ctx context.Context, name string) error
	UpdateStrategyConfig(ctx context.Context, name string, config map[string]interface{}) error

	// Health check
	Health(ctx context.Context) error
}

// ServiceStatus represents service status information
type ServiceStatus struct {
	Service string    `json:"service"`
	Status  string    `json:"status"`
	Time    time.Time `json:"time"`
	Version string    `json:"version,omitempty"`
}

// Strategy represents a trading strategy
type Strategy struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Status      string                 `json:"status"`
	Book        string                 `json:"book"`
	Config      map[string]interface{} `json:"config,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	StoppedAt   *time.Time             `json:"stopped_at,omitempty"`
	SignalsCount int64                 `json:"signals_count,omitempty"`
	LastSignal   *time.Time             `json:"last_signal,omitempty"`
}

// StrategyConfigUpdate represents a strategy configuration update
type StrategyConfigUpdate struct {
	Config map[string]interface{} `json:"config"`
}

// strategyExecutorClient implements StrategyExecutorClient
type strategyExecutorClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *logger.Logger
	metrics    *metrics.MetricsCollector
	config     *ClientConfig
}

// NewStrategyExecutorClient creates a new strategy executor client
func NewStrategyExecutorClient(cfg *ClientConfig, logger *logger.Logger, metrics *metrics.MetricsCollector) (StrategyExecutorClient, error) {
	if cfg == nil {
		return nil, fmt.Errorf("client config cannot be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}
	if metrics == nil {
		return nil, fmt.Errorf("metrics cannot be nil")
	}

	httpClient := &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        cfg.MaxIdleConns,
			MaxIdleConnsPerHost: cfg.MaxConnsPerHost,
			MaxConnsPerHost:     cfg.MaxConnsPerHost,
			IdleConnTimeout:     cfg.IdleConnTimeout,
			DisableKeepAlives:   cfg.DisableKeepAlives,
			DisableCompression:  cfg.DisableCompression,
		},
	}

	return &strategyExecutorClient{
		baseURL:    cfg.BaseURL,
		httpClient: httpClient,
		logger:     logger.WithComponent("strategy-executor-client"),
		metrics:    metrics,
		config:     cfg,
	}, nil
}

// GetStatus retrieves service status
func (c *strategyExecutorClient) GetStatus(ctx context.Context) (*ServiceStatus, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("strategy-executor", "GetStatus", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/status", c.baseURL)

	var status ServiceStatus
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &status); err != nil {
		c.metrics.RecordBackendError("strategy-executor", "GetStatus", "request_error")
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	return &status, nil
}

// ListStrategies retrieves all strategies
func (c *strategyExecutorClient) ListStrategies(ctx context.Context) ([]*Strategy, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("strategy-executor", "ListStrategies", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/strategies", c.baseURL)

	var strategies []*Strategy
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &strategies); err != nil {
		c.metrics.RecordBackendError("strategy-executor", "ListStrategies", "request_error")
		return nil, fmt.Errorf("failed to list strategies: %w", err)
	}

	c.logger.Debug("Retrieved strategies", map[string]interface{}{
		"count": len(strategies),
	})

	return strategies, nil
}

// GetStrategy retrieves a specific strategy
func (c *strategyExecutorClient) GetStrategy(ctx context.Context, name string) (*Strategy, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("strategy-executor", "GetStrategy", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/strategies/%s", c.baseURL, name)

	var strategy Strategy
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &strategy); err != nil {
		c.metrics.RecordBackendError("strategy-executor", "GetStrategy", "request_error")
		return nil, fmt.Errorf("failed to get strategy: %w", err)
	}

	return &strategy, nil
}

// StartStrategy starts a strategy
func (c *strategyExecutorClient) StartStrategy(ctx context.Context, name string) error {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("strategy-executor", "StartStrategy", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/strategies/%s/start", c.baseURL, name)

	if err := c.doRequest(ctx, http.MethodPost, url, nil, nil); err != nil {
		c.metrics.RecordBackendError("strategy-executor", "StartStrategy", "request_error")
		return fmt.Errorf("failed to start strategy: %w", err)
	}

	c.logger.Info("Strategy started", map[string]interface{}{
		"strategy": name,
	})

	return nil
}

// StopStrategy stops a strategy
func (c *strategyExecutorClient) StopStrategy(ctx context.Context, name string) error {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("strategy-executor", "StopStrategy", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/strategies/%s/stop", c.baseURL, name)

	if err := c.doRequest(ctx, http.MethodPost, url, nil, nil); err != nil {
		c.metrics.RecordBackendError("strategy-executor", "StopStrategy", "request_error")
		return fmt.Errorf("failed to stop strategy: %w", err)
	}

	c.logger.Info("Strategy stopped", map[string]interface{}{
		"strategy": name,
	})

	return nil
}

// UpdateStrategyConfig updates strategy configuration
func (c *strategyExecutorClient) UpdateStrategyConfig(ctx context.Context, name string, config map[string]interface{}) error {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("strategy-executor", "UpdateStrategyConfig", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/strategies/%s/config", c.baseURL, name)

	// Marshal request body
	requestBody := StrategyConfigUpdate{
		Config: config,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := c.doRequest(ctx, http.MethodPut, url, bytes.NewReader(jsonBody), nil); err != nil {
		c.metrics.RecordBackendError("strategy-executor", "UpdateStrategyConfig", "request_error")
		return fmt.Errorf("failed to update strategy config: %w", err)
	}

	c.logger.Info("Strategy config updated", map[string]interface{}{
		"strategy": name,
	})

	return nil
}

// Health checks the health of the strategy executor service
func (c *strategyExecutorClient) Health(ctx context.Context) error {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("strategy-executor", "Health", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/health", c.baseURL)

	var health HealthStatus
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &health); err != nil {
		c.metrics.RecordBackendError("strategy-executor", "Health", "request_error")
		return fmt.Errorf("health check failed: %w", err)
	}

	if health.Status != "healthy" {
		return fmt.Errorf("service is unhealthy: %s", health.Status)
	}

	return nil
}

// doRequest performs an HTTP request with retry logic
func (c *strategyExecutorClient) doRequest(ctx context.Context, method, url string, body io.Reader, result interface{}) error {
	var lastErr error

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-time.After(c.config.RetryDelay * time.Duration(attempt)):
			case <-ctx.Done():
				return ctx.Err()
			}

			c.logger.Debug("Retrying request", map[string]interface{}{
				"attempt": attempt,
				"url":     url,
			})
		}

		// Create request
		req, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		// Execute request
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = NewAPIError(0, "REQUEST_FAILED", "request failed", err)
			if attempt < c.config.MaxRetries {
				continue
			}
			return lastErr
		}

		// Read response body
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = NewAPIError(resp.StatusCode, "READ_FAILED", "failed to read response", err)
			if attempt < c.config.MaxRetries {
				continue
			}
			return lastErr
		}

		// Check status code
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			// Try to parse error response
			var apiResp APIResponse
			if err := json.Unmarshal(respBody, &apiResp); err == nil && apiResp.Error != nil {
				lastErr = &APIError{
					StatusCode: resp.StatusCode,
					Code:       apiResp.Error.Code,
					Message:    apiResp.Error.Message,
					Details:    apiResp.Error.Details,
				}
			} else {
				lastErr = NewAPIError(resp.StatusCode, "HTTP_ERROR", string(respBody), nil)
			}

			// Retry on retryable errors
			if apiErr, ok := lastErr.(*APIError); ok && apiErr.IsRetryable() && attempt < c.config.MaxRetries {
				continue
			}
			return lastErr
		}

		// Parse successful response
		if result != nil {
			// Try to parse as APIResponse first
			var apiResp APIResponse
			if err := json.Unmarshal(respBody, &apiResp); err == nil && apiResp.Data != nil {
				// Extract data field
				if err := json.Unmarshal(apiResp.Data, result); err != nil {
					return fmt.Errorf("failed to unmarshal data: %w", err)
				}
			} else {
				// Parse directly
				if err := json.Unmarshal(respBody, result); err != nil {
					return fmt.Errorf("failed to unmarshal response: %w", err)
				}
			}
		}

		return nil
	}

	return lastErr
}

