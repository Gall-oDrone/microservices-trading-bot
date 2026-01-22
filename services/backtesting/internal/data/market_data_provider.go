package data

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/models"
	"bitso-trading-platform/shared/pkg/bitso"
)

// MarketDataProvider implements DataProvider using the market-data service
type MarketDataProvider struct {
	baseURL    string
	httpClient *http.Client
	cache      *Cache
	logger     logger.Logger
	retryCount int
	retryDelay time.Duration
}

// NewMarketDataProvider creates a new market-data service provider
func NewMarketDataProvider(baseURL string, cache *Cache, log logger.Logger, retryCount int, retryDelay time.Duration) *MarketDataProvider {
	return &MarketDataProvider{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		cache:      cache,
		logger:     log,
		retryCount: retryCount,
		retryDelay: retryDelay,
	}
}

// LoadHistoricalData loads historical data for the given request
func (p *MarketDataProvider) LoadHistoricalData(ctx context.Context, req *DataRequest) ([]models.MarketEvent, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	p.logger.Info("Loading historical data", map[string]interface{}{
		"book":        req.Book,
		"start_date":  req.StartDate.Format("2006-01-02"),
		"end_date":    req.EndDate.Format("2006-01-02"),
		"event_types": req.EventTypes,
	})

	// Check cache first
	if p.cache != nil {
		cacheKey := p.cache.generateKey(req.Book, req.StartDate, req.EndDate, string(req.EventTypes[0]))
		if events, err := p.cache.Get(ctx, cacheKey); err == nil {
			p.logger.Debug("Cache hit for historical data", map[string]interface{}{
				"cache_key": cacheKey,
				"count":     len(events),
			})
			return events, nil
		}
	}

	// Load data from API
	events := make([]models.MarketEvent, 0)

	for _, eventType := range req.EventTypes {
		switch eventType {
		case models.EventTypeTrade:
			trades, err := p.fetchTrades(ctx, req.Book, req.StartDate, req.EndDate, req.Limit)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch trades: %w", err)
			}
			for _, trade := range trades {
				events = append(events, *models.NewTradeEvent(&trade))
			}

		case models.EventTypeTicker:
			tickers, err := p.fetchTickers(ctx, req.Book, req.StartDate, req.EndDate)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch tickers: %w", err)
			}
			for _, ticker := range tickers {
				events = append(events, *models.NewTickerEvent(&ticker))
			}
		}
	}

	// Sort events by timestamp
	p.sortEventsByTimestamp(events)

	// Cache the results
	if p.cache != nil {
		cacheKey := p.cache.generateKey(req.Book, req.StartDate, req.EndDate, string(req.EventTypes[0]))
		if err := p.cache.Set(ctx, cacheKey, events); err != nil {
			p.logger.Warn("Failed to cache data", map[string]interface{}{"error": err})
		}
	}

	p.logger.Info("Historical data loaded", map[string]interface{}{
		"count": len(events),
	})

	return events, nil
}

// StreamData streams historical data through a channel
func (p *MarketDataProvider) StreamData(ctx context.Context, req *DataRequest) (<-chan models.MarketEvent, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	eventChan := make(chan models.MarketEvent, 100)

	go func() {
		defer close(eventChan)

		// Load all data
		events, err := p.LoadHistoricalData(ctx, req)
		if err != nil {
			p.logger.Error("Failed to load data for streaming", map[string]interface{}{"error": err})
			return
		}

		// Stream events
		for _, event := range events {
			select {
			case <-ctx.Done():
				return
			case eventChan <- event:
			}
		}
	}()

	return eventChan, nil
}

// GetDataRange returns the available date range for a book
func (p *MarketDataProvider) GetDataRange(ctx context.Context, book string) (*DateRange, error) {
	// For now, return a reasonable default range
	// In a real implementation, this would query the market-data service
	return &DateRange{
		FirstDate: time.Now().AddDate(-2, 0, 0), // 2 years ago
		LastDate:  time.Now(),
	}, nil
}

// fetchTrades fetches historical trades from the market-data service
func (p *MarketDataProvider) fetchTrades(ctx context.Context, book string, startDate, endDate time.Time, limit int) ([]bitso.Trade, error) {
	// Build URL
	endpoint := fmt.Sprintf("%s/api/v1/trades", p.baseURL)

	params := url.Values{}
	params.Add("book", book)
	params.Add("from", startDate.Format(time.RFC3339))
	params.Add("to", endDate.Format(time.RFC3339))
	if limit > 0 {
		params.Add("limit", fmt.Sprintf("%d", limit))
	} else {
		params.Add("limit", "10000") // Default limit
	}

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	p.logger.Debug("Fetching trades", map[string]interface{}{
		"url": fullURL,
	})

	// Make HTTP request with retries
	resp, err := p.makeRequestWithRetry(ctx, fullURL)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result struct {
		Success bool          `json:"success"`
		Data    []bitso.Trade `json:"data"`
		Error   string        `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("API error: %s", result.Error)
	}

	p.logger.Debug("Fetched trades", map[string]interface{}{
		"count": len(result.Data),
	})

	return result.Data, nil
}

// fetchTickers fetches historical tickers from the market-data service
func (p *MarketDataProvider) fetchTickers(ctx context.Context, book string, startDate, endDate time.Time) ([]bitso.Ticker, error) {
	// Build URL
	endpoint := fmt.Sprintf("%s/api/v1/ticker/history", p.baseURL)

	params := url.Values{}
	params.Add("book", book)
	params.Add("from", startDate.Format(time.RFC3339))
	params.Add("to", endDate.Format(time.RFC3339))

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	p.logger.Debug("Fetching tickers", map[string]interface{}{
		"url": fullURL,
	})

	// Make HTTP request with retries
	resp, err := p.makeRequestWithRetry(ctx, fullURL)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result struct {
		Success bool           `json:"success"`
		Data    []bitso.Ticker `json:"data"`
		Error   string         `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("API error: %s", result.Error)
	}

	return result.Data, nil
}

// makeRequestWithRetry makes an HTTP request with retry logic
func (p *MarketDataProvider) makeRequestWithRetry(ctx context.Context, url string) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= p.retryCount; attempt++ {
		if attempt > 0 {
			// Wait before retry with exponential backoff
			backoff := p.retryDelay * time.Duration(1<<uint(attempt-1))
			p.logger.Debug("Retrying request", map[string]interface{}{
				"attempt": attempt,
				"backoff": backoff,
			})

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		// Create request
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Set headers
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		// Execute request
		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = err
			p.logger.Warn("Request failed", map[string]interface{}{
				"attempt": attempt + 1,
				"error":   err,
			})
			continue
		}

		// Success
		return resp, nil
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", p.retryCount+1, lastErr)
}

// sortEventsByTimestamp sorts events by timestamp in ascending order
func (p *MarketDataProvider) sortEventsByTimestamp(events []models.MarketEvent) {
	// Simple bubble sort (can be optimized with sort.Slice if needed)
	n := len(events)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if events[j].Timestamp.After(events[j+1].Timestamp) {
				events[j], events[j+1] = events[j+1], events[j]
			}
		}
	}
}

// Close closes the provider and releases resources
func (p *MarketDataProvider) Close() error {
	p.httpClient.CloseIdleConnections()
	if p.cache != nil {
		return p.cache.Close()
	}
	return nil
}
