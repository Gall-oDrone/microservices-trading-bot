package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/strategy-executor/internal/logger"
	"bitso-trading-platform/strategy-executor/internal/metrics"
)

// MarketDataClient provides HTTP client for market-data service
type MarketDataClient interface {
	GetRecentTrades(ctx context.Context, book string, limit int) ([]*models.TradeEvent, error)
	GetTrade(ctx context.Context, book string, tradeID uint64) (*models.TradeEvent, error)
	GetTicker(ctx context.Context, book string) (*bitso.Ticker, error)
	GetOrderBook(ctx context.Context, book string) (*bitso.OrderBook, error)
	GetTradeHistory(ctx context.Context, book string, start, end time.Time) ([]*models.TradeEvent, error)
	GetTradeStats(ctx context.Context, book string) (*TradeStats, error)
	Health(ctx context.Context) error
}

// Client implements MarketDataClient
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *logger.Logger
	metrics    *metrics.Metrics
}

// ClientConfig holds configuration for the market data client
type ClientConfig struct {
	BaseURL    string
	Timeout    time.Duration
	RetryCount int
	RetryDelay time.Duration
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

// APIResponse represents a generic API response
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// NewClient creates a new market data client
func NewClient(config *ClientConfig, logger *logger.Logger, metrics *metrics.Metrics) (*Client, error) {
	if config == nil {
		return nil, fmt.Errorf("client config cannot be nil")
	}

	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	if metrics == nil {
		return nil, fmt.Errorf("metrics cannot be nil")
	}

	httpClient := &http.Client{
		Timeout: config.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	return &Client{
		baseURL:    config.BaseURL,
		httpClient: httpClient,
		logger:     logger,
		metrics:    metrics,
	}, nil
}

// GetRecentTrades retrieves recent trades for a book
func (c *Client) GetRecentTrades(ctx context.Context, book string, limit int) ([]*models.TradeEvent, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordHTTPRequest("GET", "/api/v1/trades/recent", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/trades/%s/recent?limit=%d", c.baseURL, book, limit)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var trades []*models.TradeEvent
	if err := json.Unmarshal(body, &trades); err != nil {
		return nil, fmt.Errorf("failed to unmarshal trades: %w", err)
	}

	c.logger.Debugf("Retrieved %d recent trades for book %s", len(trades), book)
	return trades, nil
}

// GetTrade retrieves a specific trade by ID
func (c *Client) GetTrade(ctx context.Context, book string, tradeID uint64) (*models.TradeEvent, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordHTTPRequest("GET", "/api/v1/trades/trade", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/trades/%s/%d", c.baseURL, book, tradeID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("trade %d not found for book %s", tradeID, book)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var trade models.TradeEvent
	if err := json.Unmarshal(body, &trade); err != nil {
		return nil, fmt.Errorf("failed to unmarshal trade: %w", err)
	}

	c.logger.Debugf("Retrieved trade %d for book %s", tradeID, book)
	return &trade, nil
}

// GetTicker retrieves current ticker for a book
func (c *Client) GetTicker(ctx context.Context, book string) (*bitso.Ticker, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordHTTPRequest("GET", "/api/v1/ticker", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/ticker/%s", c.baseURL, book)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var ticker bitso.Ticker
	if err := json.Unmarshal(body, &ticker); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ticker: %w", err)
	}

	c.logger.Debugf("Retrieved ticker for book %s", book)
	return &ticker, nil
}

// GetOrderBook retrieves current order book for a book
func (c *Client) GetOrderBook(ctx context.Context, book string) (*bitso.OrderBook, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordHTTPRequest("GET", "/api/v1/orderbook", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/orderbook/%s", c.baseURL, book)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var orderBook bitso.OrderBook
	if err := json.Unmarshal(body, &orderBook); err != nil {
		return nil, fmt.Errorf("failed to unmarshal order book: %w", err)
	}

	c.logger.Debugf("Retrieved order book for book %s", book)
	return &orderBook, nil
}

// GetTradeHistory retrieves trade history for a book within a time range
func (c *Client) GetTradeHistory(ctx context.Context, book string, start, end time.Time) ([]*models.TradeEvent, error) {
	startTime := time.Now()
	defer func() {
		c.metrics.RecordHTTPRequest("GET", "/api/v1/trades/history", "200", time.Since(startTime))
	}()

	url := fmt.Sprintf("%s/api/v1/trades/%s/history?start=%s&end=%s",
		c.baseURL, book, start.Format(time.RFC3339), end.Format(time.RFC3339))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var trades []*models.TradeEvent
	if err := json.Unmarshal(body, &trades); err != nil {
		return nil, fmt.Errorf("failed to unmarshal trades: %w", err)
	}

	c.logger.Debugf("Retrieved %d historical trades for book %s", len(trades), book)
	return trades, nil
}

// GetTradeStats retrieves trade statistics for a book
func (c *Client) GetTradeStats(ctx context.Context, book string) (*TradeStats, error) {
	start := time.Now()
	defer func() {
		c.metrics.RecordHTTPRequest("GET", "/api/v1/trades/stats", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/api/v1/trades/%s/stats", c.baseURL, book)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var stats TradeStats
	if err := json.Unmarshal(body, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal trade stats: %w", err)
	}

	c.logger.Debugf("Retrieved trade stats for book %s", book)
	return &stats, nil
}

// Health checks the health of the market data service
func (c *Client) Health(ctx context.Context) error {
	start := time.Now()
	defer func() {
		c.metrics.RecordHTTPRequest("GET", "/health", "200", time.Since(start))
	}()

	url := fmt.Sprintf("%s/health", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status %d", resp.StatusCode)
	}

	c.logger.Debug("Market data service health check passed")
	return nil
}

// WithRetry executes a function with retry logic
func (c *Client) WithRetry(ctx context.Context, fn func() error) error {
	var lastErr error

	for i := 0; i < 3; i++ { // Default retry count
		if err := fn(); err != nil {
			lastErr = err
			if i < 2 { // Don't sleep on last attempt
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Second * time.Duration(i+1)): // Exponential backoff
					continue
				}
			}
		} else {
			return nil
		}
	}

	return lastErr
}
