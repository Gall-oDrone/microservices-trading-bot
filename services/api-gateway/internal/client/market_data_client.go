package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
)

// MarketDataClient defines the interface for market data service
type MarketDataClient interface {
	// Trade endpoints
	GetRecentTrades(ctx context.Context, book string, limit int) ([]*models.TradeEvent, error)
	GetTrade(ctx context.Context, book string, tradeID uint64) (*models.TradeEvent, error)
	GetTradeStats(ctx context.Context, book string) (*TradeStats, error)

	// Order book endpoints
	GetOrderBook(ctx context.Context, book string) (*bitso.OrderBook, error)

	// Ticker endpoints
	GetTicker(ctx context.Context, book string) (*bitso.Ticker, error)

	// Market summary endpoints
	GetMarketSummary(ctx context.Context) (*MarketSummary, error)

	// Health check
	Health(ctx context.Context) error
}

// TradeStats represents trade statistics
type TradeStats struct {
	Book               string    `json:"book"`
	TotalTrades        int64     `json:"total_trades"`
	TotalVolume        float64   `json:"total_volume"`
	TotalValue         float64   `json:"total_value"`
	LastPrice          float64   `json:"last_price"`
	HighPrice          float64   `json:"high_price"`
	LowPrice           float64   `json:"low_price"`
	VWAP               float64   `json:"vwap"`
	PriceChange        float64   `json:"price_change"`
	PriceChangePercent float64   `json:"price_change_percent"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// MarketSummary represents market summary data
type MarketSummary struct {
	Books     []string                `json:"books"`
	Timestamp time.Time               `json:"timestamp"`
	Summary   map[string]*BookSummary `json:"summary"`
}

// BookSummary represents summary for a specific book
type BookSummary struct {
	Book      string  `json:"book"`
	LastPrice float64 `json:"last_price"`
	Volume24h float64 `json:"volume_24h"`
	High24h   float64 `json:"high_24h"`
	Low24h    float64 `json:"low_24h"`
	Change24h float64 `json:"change_24h"`
}

// marketDataClient implements MarketDataClient
type marketDataClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *logger.Logger
	metrics    *metrics.MetricsCollector
	config     *ClientConfig
}

// NewMarketDataClient creates a new market data client
func NewMarketDataClient(cfg *ClientConfig, logger *logger.Logger, metrics *metrics.MetricsCollector) (MarketDataClient, error) {
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

	return &marketDataClient{
		baseURL:    cfg.BaseURL,
		httpClient: httpClient,
		logger:     logger.WithComponent("market-data-client"),
		metrics:    metrics,
		config:     cfg,
	}, nil
}

// GetRecentTrades retrieves recent trades for a book
func (c *marketDataClient) GetRecentTrades(ctx context.Context, book string, limit int) ([]*models.TradeEvent, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("market-data", "GetRecentTrades", "200", time.Since(start))
	}()

	// Build URL with query parameters
	url := fmt.Sprintf("%s/api/v1/trades?book=%s&limit=%d", c.baseURL, book, limit)

	// Make request
	var trades []*models.TradeEvent
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &trades); err != nil {
		c.metrics.RecordBackendError("market-data", "GetRecentTrades", "request_error")
		return nil, fmt.Errorf("failed to get recent trades: %w", err)
	}

	c.logger.Debug("Retrieved recent trades", map[string]interface{}{
		"book":  book,
		"count": len(trades),
	})

	return trades, nil
}

// GetTrade retrieves a specific trade
func (c *marketDataClient) GetTrade(ctx context.Context, book string, tradeID uint64) (*models.TradeEvent, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("market-data", "GetTrade", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/trades/%s/%d", c.baseURL, book, tradeID)

	var trade models.TradeEvent
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &trade); err != nil {
		c.metrics.RecordBackendError("market-data", "GetTrade", "request_error")
		return nil, fmt.Errorf("failed to get trade: %w", err)
	}

	return &trade, nil
}

// GetTradeStats retrieves trade statistics
func (c *marketDataClient) GetTradeStats(ctx context.Context, book string) (*TradeStats, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("market-data", "GetTradeStats", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/trades/stats?book=%s", c.baseURL, book)

	var stats TradeStats
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &stats); err != nil {
		c.metrics.RecordBackendError("market-data", "GetTradeStats", "request_error")
		return nil, fmt.Errorf("failed to get trade stats: %w", err)
	}

	return &stats, nil
}

// GetOrderBook retrieves the current order book
func (c *marketDataClient) GetOrderBook(ctx context.Context, book string) (*bitso.OrderBook, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("market-data", "GetOrderBook", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/orderbook?book=%s", c.baseURL, book)

	var orderBook bitso.OrderBook
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &orderBook); err != nil {
		c.metrics.RecordBackendError("market-data", "GetOrderBook", "request_error")
		return nil, fmt.Errorf("failed to get order book: %w", err)
	}

	return &orderBook, nil
}

// GetTicker retrieves the current ticker
func (c *marketDataClient) GetTicker(ctx context.Context, book string) (*bitso.Ticker, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("market-data", "GetTicker", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/ticker?book=%s", c.baseURL, book)

	var ticker bitso.Ticker
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &ticker); err != nil {
		c.metrics.RecordBackendError("market-data", "GetTicker", "request_error")
		return nil, fmt.Errorf("failed to get ticker: %w", err)
	}

	return &ticker, nil
}

// GetMarketSummary retrieves market summary
func (c *marketDataClient) GetMarketSummary(ctx context.Context) (*MarketSummary, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("market-data", "GetMarketSummary", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/market/summary", c.baseURL)

	var summary MarketSummary
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &summary); err != nil {
		c.metrics.RecordBackendError("market-data", "GetMarketSummary", "request_error")
		return nil, fmt.Errorf("failed to get market summary: %w", err)
	}

	return &summary, nil
}

// Health checks the health of the market data service
func (c *marketDataClient) Health(ctx context.Context) error {
	start := time.Now()
	defer func() {
		c.metrics.RecordBackendCall("market-data", "Health", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/health", c.baseURL)

	var health HealthStatus
	if err := c.doRequest(ctx, http.MethodGet, url, nil, &health); err != nil {
		c.metrics.RecordBackendError("market-data", "Health", "request_error")
		return fmt.Errorf("health check failed: %w", err)
	}

	if health.Status != "healthy" {
		return fmt.Errorf("service is unhealthy: %s", health.Status)
	}

	return nil
}

// doRequest performs an HTTP request with retry logic
func (c *marketDataClient) doRequest(ctx context.Context, method, url string, body io.Reader, result interface{}) error {
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

// Helper function to format query parameters
func formatQueryParams(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}

	query := "?"
	first := true
	for key, value := range params {
		if !first {
			query += "&"
		}
		query += fmt.Sprintf("%s=%s", key, value)
		first = false
	}
	return query
}

// Helper function to convert int to string
func intToString(i int) string {
	return strconv.Itoa(i)
}
