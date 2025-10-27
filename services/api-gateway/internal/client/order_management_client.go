package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
)

// OrderManagementClient defines the interface for order management service
type OrderManagementClient interface {
	// Order endpoints
	ListOrders(ctx context.Context, filters *OrderFilters) (*OrderList, error)
	GetOrder(ctx context.Context, orderID string) (*Order, error)
	CancelOrder(ctx context.Context, orderID string) error
	GetActiveOrders(ctx context.Context) ([]*Order, error)
	GetOrderHistory(ctx context.Context, filters *OrderFilters) ([]*Order, error)

	// Position endpoints
	ListPositions(ctx context.Context, filters *PositionFilters) ([]*Position, error)
	GetPosition(ctx context.Context, book string) (*Position, error)
	GetPositionSummary(ctx context.Context) (*PositionSummary, error)

	// Health check
	Health(ctx context.Context) error
}

// Order represents a trading order
type Order struct {
	ID              string                 `json:"id"`
	ClientOrderID   string                 `json:"client_order_id,omitempty"`
	SignalID        string                 `json:"signal_id,omitempty"`
	Book            string                 `json:"book"`
	Side            string                 `json:"side"`
	Type            string                 `json:"type"`
	Status          string                 `json:"status"`
	Price           float64                `json:"price"`
	Amount          float64                `json:"amount"`
	FilledAmount    float64                `json:"filled_amount"`
	RemainingAmount float64                `json:"remaining_amount"`
	Strategy        string                 `json:"strategy,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// OrderList represents a list of orders with pagination
type OrderList struct {
	Orders []*Order `json:"orders"`
	Total  int      `json:"total"`
	Limit  int      `json:"limit"`
	Offset int      `json:"offset"`
}

// OrderFilters represents filters for querying orders
type OrderFilters struct {
	Book      string
	Status    string
	Strategy  string
	Side      string
	From      *time.Time
	To        *time.Time
	Limit     int
	Offset    int
	SortBy    string
	SortOrder string
}

// Position represents a trading position
type Position struct {
	Book            string    `json:"book"`
	Side            string    `json:"side"`
	Size            float64   `json:"size"`
	EntryPrice      float64   `json:"entry_price"`
	CurrentPrice    float64   `json:"current_price"`
	UnrealizedPnL   float64   `json:"unrealized_pnl"`
	RealizedPnL     float64   `json:"realized_pnl"`
	TotalPnL        float64   `json:"total_pnl"`
	Status          string    `json:"status"`
	OpenedAt        time.Time `json:"opened_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PositionSummary represents a summary of all positions
type PositionSummary struct {
	TotalPositions    int                        `json:"total_positions"`
	OpenPositions     int                        `json:"open_positions"`
	ClosedPositions   int                        `json:"closed_positions"`
	TotalUnrealizedPnL float64                   `json:"total_unrealized_pnl"`
	TotalRealizedPnL  float64                    `json:"total_realized_pnl"`
	TotalPnL          float64                    `json:"total_pnl"`
	PositionsByBook   map[string]*Position       `json:"positions_by_book"`
}

// PositionFilters represents filters for querying positions
type PositionFilters struct {
	Book   string
	Status string
	Limit  int
	Offset int
}

// orderManagementClient implements OrderManagementClient
type orderManagementClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *logger.Logger
	metrics    *metrics.MetricsCollector
	config     *ClientConfig
}

// NewOrderManagementClient creates a new order management client
func NewOrderManagementClient(cfg *ClientConfig, logger *logger.Logger, metrics *metrics.MetricsCollector) (OrderManagementClient, error) {
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

	return &orderManagementClient{
		baseURL:    cfg.BaseURL,
		httpClient: httpClient,
		logger:     logger.WithComponent("order-management-client"),
		metrics:    metrics,
		config:     cfg,
	}, nil
}

// ListOrders retrieves a list of orders with filters
func (c *orderManagementClient) ListOrders(ctx context.Context, filters *OrderFilters) (*OrderList, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("order-management", "ListOrders", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/orders%s", c.baseURL, buildOrderQueryString(filters))

	var orderList OrderList
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &orderList); err != nil {
		c.metrics.RecordBackendError("order-management", "ListOrders", "request_error")
		return nil, fmt.Errorf("failed to list orders: %w", err)
	}

	return &orderList, nil
}

// GetOrder retrieves a specific order
func (c *orderManagementClient) GetOrder(ctx context.Context, orderID string) (*Order, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("order-management", "GetOrder", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/orders/%s", c.baseURL, orderID)

	var order Order
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &order); err != nil {
		c.metrics.RecordBackendError("order-management", "GetOrder", "request_error")
		return nil, fmt.Errorf("failed to get order: %w", err)
	}

	return &order, nil
}

// CancelOrder cancels an order
func (c *orderManagementClient) CancelOrder(ctx context.Context, orderID string) error {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("order-management", "CancelOrder", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/orders/%s/cancel", c.baseURL, orderID)

	if err := c.doRequest(ctx, http.MethodPost, url, nil, nil); err != nil {
		c.metrics.RecordBackendError("order-management", "CancelOrder", "request_error")
		return fmt.Errorf("failed to cancel order: %w", err)
	}

	c.logger.Info("Order cancelled", map[string]interface{}{
		"order_id": orderID,
	})

	return nil
}

// GetActiveOrders retrieves all active orders
func (c *orderManagementClient) GetActiveOrders(ctx context.Context) ([]*Order, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("order-management", "GetActiveOrders", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/orders/active", c.baseURL)

	var orderList OrderList
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &orderList); err != nil {
		c.metrics.RecordBackendError("order-management", "GetActiveOrders", "request_error")
		return nil, fmt.Errorf("failed to get active orders: %w", err)
	}

	return orderList.Orders, nil
}

// GetOrderHistory retrieves order history
func (c *orderManagementClient) GetOrderHistory(ctx context.Context, filters *OrderFilters) ([]*Order, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("order-management", "GetOrderHistory", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/orders/history%s", c.baseURL, buildOrderQueryString(filters))

	var orderList OrderList
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &orderList); err != nil {
		c.metrics.RecordBackendError("order-management", "GetOrderHistory", "request_error")
		return nil, fmt.Errorf("failed to get order history: %w", err)
	}

	return orderList.Orders, nil
}

// ListPositions retrieves a list of positions with filters
func (c *orderManagementClient) ListPositions(ctx context.Context, filters *PositionFilters) ([]*Position, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("order-management", "ListPositions", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/positions%s", c.baseURL, buildPositionQueryString(filters))

	var positions []*Position
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &positions); err != nil {
		c.metrics.RecordBackendError("order-management", "ListPositions", "request_error")
		return nil, fmt.Errorf("failed to list positions: %w", err)
	}

	return positions, nil
}

// GetPosition retrieves a position for a specific book
func (c *orderManagementClient) GetPosition(ctx context.Context, book string) (*Position, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("order-management", "GetPosition", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/positions/%s", c.baseURL, book)

	var position Position
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &position); err != nil {
		c.metrics.RecordBackendError("order-management", "GetPosition", "request_error")
		return nil, fmt.Errorf("failed to get position: %w", err)
	}

	return &position, nil
}

// GetPositionSummary retrieves position summary
func (c *orderManagementClient) GetPositionSummary(ctx context.Context) (*PositionSummary, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("order-management", "GetPositionSummary", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/positions/summary", c.baseURL)

	var summary PositionSummary
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &summary); err != nil {
		c.metrics.RecordBackendError("order-management", "GetPositionSummary", "request_error")
		return nil, fmt.Errorf("failed to get position summary: %w", err)
	}

	return &summary, nil
}

// Health checks the health of the order management service
func (c *orderManagementClient) Health(ctx context.Context) error {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("order-management", "Health", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/health", c.baseURL)

	var health HealthStatus
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &health); err != nil {
		c.metrics.RecordBackendError("order-management", "Health", "request_error")
		return fmt.Errorf("health check failed: %w", err)
	}

	if health.Status != "healthy" {
		return fmt.Errorf("service is unhealthy: %s", health.Status)
	}

	return nil
}

// doRequest performs an HTTP request with retry logic
func (c *orderManagementClient) doRequest(ctx context.Context, method, url string, body io.Reader, result interface{}) error {
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

// buildOrderQueryString builds query string from order filters
func buildOrderQueryString(filters *OrderFilters) string {
	if filters == nil {
		return ""
	}

	params := []string{}

	if filters.Book != "" {
		params = append(params, fmt.Sprintf("book=%s", filters.Book))
	}
	if filters.Status != "" {
		params = append(params, fmt.Sprintf("status=%s", filters.Status))
	}
	if filters.Strategy != "" {
		params = append(params, fmt.Sprintf("strategy=%s", filters.Strategy))
	}
	if filters.Side != "" {
		params = append(params, fmt.Sprintf("side=%s", filters.Side))
	}
	if filters.From != nil {
		params = append(params, fmt.Sprintf("from=%s", filters.From.Format(time.RFC3339)))
	}
	if filters.To != nil {
		params = append(params, fmt.Sprintf("to=%s", filters.To.Format(time.RFC3339)))
	}
	if filters.Limit > 0 {
		params = append(params, fmt.Sprintf("limit=%d", filters.Limit))
	}
	if filters.Offset > 0 {
		params = append(params, fmt.Sprintf("offset=%d", filters.Offset))
	}
	if filters.SortBy != "" {
		params = append(params, fmt.Sprintf("sort_by=%s", filters.SortBy))
	}
	if filters.SortOrder != "" {
		params = append(params, fmt.Sprintf("sort_order=%s", filters.SortOrder))
	}

	if len(params) == 0 {
		return ""
	}

	return "?" + strings.Join(params, "&")
}

// buildPositionQueryString builds query string from position filters
func buildPositionQueryString(filters *PositionFilters) string {
	if filters == nil {
		return ""
	}

	params := []string{}

	if filters.Book != "" {
		params = append(params, fmt.Sprintf("book=%s", filters.Book))
	}
	if filters.Status != "" {
		params = append(params, fmt.Sprintf("status=%s", filters.Status))
	}
	if filters.Limit > 0 {
		params = append(params, fmt.Sprintf("limit=%d", filters.Limit))
	}
	if filters.Offset > 0 {
		params = append(params, fmt.Sprintf("offset=%d", filters.Offset))
	}

	if len(params) == 0 {
		return ""
	}

	return "?" + strings.Join(params, "&")
}

