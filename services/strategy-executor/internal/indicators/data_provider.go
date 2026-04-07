// Package indicators provides data providers for indicator computation.
package indicators

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPDataProvider implements DataProvider using HTTP calls to market-data service
type HTTPDataProvider struct {
	baseURL    string
	httpClient *http.Client
}

// NewHTTPDataProvider creates a new HTTP-based data provider
func NewHTTPDataProvider(baseURL string) *HTTPDataProvider {
	return &HTTPDataProvider{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// NewHTTPDataProviderWithClient creates a provider with custom HTTP client
func NewHTTPDataProviderWithClient(baseURL string, client *http.Client) *HTTPDataProvider {
	return &HTTPDataProvider{
		baseURL:    baseURL,
		httpClient: client,
	}
}

// GetRecentTrades fetches recent trades from market-data service
func (p *HTTPDataProvider) GetRecentTrades(ctx context.Context, book string, limit int) ([]Trade, error) {
	url := fmt.Sprintf("%s/api/v1/trades/%s?limit=%d", p.baseURL, book, limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Trades []struct {
			Timestamp string  `json:"timestamp"`
			Price     float64 `json:"price"`
			Amount    float64 `json:"amount"`
			Side      string  `json:"side"`
		} `json:"trades"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	trades := make([]Trade, len(response.Trades))
	for i, t := range response.Trades {
		ts, _ := time.Parse(time.RFC3339, t.Timestamp)
		trades[i] = Trade{
			Timestamp: ts,
			Price:     t.Price,
			Amount:    t.Amount,
			Side:      t.Side,
		}
	}

	return trades, nil
}

// GetRecentBars fetches recent OHLCV bars from market-data service
func (p *HTTPDataProvider) GetRecentBars(ctx context.Context, book string, interval string, limit int) ([]OHLCV, error) {
	url := fmt.Sprintf("%s/api/v1/bars/%s?interval=%s&limit=%d", p.baseURL, book, interval, limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Bars []struct {
			Timestamp string  `json:"timestamp"`
			Open      float64 `json:"open"`
			High      float64 `json:"high"`
			Low       float64 `json:"low"`
			Close     float64 `json:"close"`
			Volume    float64 `json:"volume"`
		} `json:"bars"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	bars := make([]OHLCV, len(response.Bars))
	for i, b := range response.Bars {
		ts, _ := time.Parse(time.RFC3339, b.Timestamp)
		bars[i] = OHLCV{
			Timestamp: ts,
			Open:      b.Open,
			High:      b.High,
			Low:       b.Low,
			Close:     b.Close,
			Volume:    b.Volume,
		}
	}

	return bars, nil
}

// MockDataProvider implements DataProvider for testing
type MockDataProvider struct {
	trades []Trade
	bars   []OHLCV
}

// NewMockDataProvider creates a mock data provider
func NewMockDataProvider() *MockDataProvider {
	return &MockDataProvider{
		trades: make([]Trade, 0),
		bars:   make([]OHLCV, 0),
	}
}

// SetTrades sets mock trade data
func (p *MockDataProvider) SetTrades(trades []Trade) {
	p.trades = trades
}

// SetBars sets mock bar data
func (p *MockDataProvider) SetBars(bars []OHLCV) {
	p.bars = bars
}

// AddTrade adds a single trade
func (p *MockDataProvider) AddTrade(trade Trade) {
	p.trades = append(p.trades, trade)
}

// AddBar adds a single bar
func (p *MockDataProvider) AddBar(bar OHLCV) {
	p.bars = append(p.bars, bar)
}

// GetRecentTrades returns mock trades
func (p *MockDataProvider) GetRecentTrades(ctx context.Context, book string, limit int) ([]Trade, error) {
	if len(p.trades) <= limit {
		return p.trades, nil
	}
	return p.trades[len(p.trades)-limit:], nil
}

// GetRecentBars returns mock bars
func (p *MockDataProvider) GetRecentBars(ctx context.Context, book string, interval string, limit int) ([]OHLCV, error) {
	if len(p.bars) <= limit {
		return p.bars, nil
	}
	return p.bars[len(p.bars)-limit:], nil
}

// GenerateMockTrades generates random-ish trades for testing
func GenerateMockTrades(book string, basePrice float64, count int) []Trade {
	trades := make([]Trade, count)
	price := basePrice

	for i := 0; i < count; i++ {
		change := (float64(i%5) - 2) * basePrice * 0.001
		price += change

		side := "buy"
		if i%3 == 0 {
			side = "sell"
		}

		trades[i] = Trade{
			Timestamp: time.Now().Add(-time.Duration(count-i) * time.Second),
			Price:     price,
			Amount:    0.01,
			Side:      side,
		}
	}

	return trades
}

// GenerateMockBars generates random-ish OHLCV bars for testing
func GenerateMockBars(book string, basePrice float64, count int) []OHLCV {
	bars := make([]OHLCV, count)
	price := basePrice

	for i := 0; i < count; i++ {
		change := (float64(i%7) - 3) * basePrice * 0.002
		open := price
		close := price + change
		high := max(open, close) + basePrice*0.001
		low := min(open, close) - basePrice*0.001

		bars[i] = OHLCV{
			Timestamp: time.Now().Add(-time.Duration(count-i) * time.Minute),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    float64(i%10+1) * 0.1,
		}

		price = close
	}

	return bars
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
