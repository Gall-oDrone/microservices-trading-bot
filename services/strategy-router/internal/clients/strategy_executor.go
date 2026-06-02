// Package clients holds the HTTP client to strategy-executor.
//
// The Client interface is the seam between the router engine and the
// outside world; production wires the HTTPClient backed by net/http, and
// tests inject a fake.
package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"bitso-trading-platform/strategy-router/internal/classifier"
)

// StrategyInfo is the subset of GET /api/v1/strategies entries the
// router needs.
type StrategyInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Book    string `json:"book"`
	Running bool   `json:"running"`
}

// StrategyState is the subset of GET /api/v1/strategies/{name}/state.
type StrategyState struct {
	Name        string `json:"name"`
	HasPosition bool   `json:"has_position"`
}

// Client talks to strategy-executor.
//
// All methods return ctx errors when the context is cancelled. Non-2xx
// responses are returned as plain `error` so the caller can degrade
// gracefully (the router treats any client error as a one-cycle skip).
type Client interface {
	GetSnapshot(ctx context.Context, book string) (classifier.Snapshot, error)
	ListStrategies(ctx context.Context) ([]StrategyInfo, error)
	GetStrategyState(ctx context.Context, name string) (StrategyState, error)
	StartStrategy(ctx context.Context, name string) error
	StopStrategy(ctx context.Context, name string) error
}

// HTTPClient is the production implementation of Client.
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewHTTPClient builds a Client pointing at the given strategy-executor
// base URL (e.g. http://strategy-executor:8081).
func NewHTTPClient(baseURL string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

// snapshotResponse is the wire payload returned by
// GET /api/v1/indicators/{book}/snapshot.
type snapshotResponse struct {
	Book      string `json:"book"`
	SMA       *valueField     `json:"sma,omitempty"`
	EMA       *valueField     `json:"ema,omitempty"`
	RSI       *valueField     `json:"rsi,omitempty"`
	ATR       *valueField     `json:"atr,omitempty"`
	Bollinger *bollingerField `json:"bollinger,omitempty"`
	VWAP      *valueField     `json:"vwap,omitempty"`
}

type valueField struct {
	Value float64 `json:"value"`
}

type bollingerField struct {
	// strategy-executor emits PascalCase (BollingerBands has no json tags).
	Upper  float64 `json:"Upper"`
	Middle float64 `json:"Middle"`
	Lower  float64 `json:"Lower"`
	// Legacy / doc snake_case (kept for forward-compatible snapshots).
	UpperBand  float64 `json:"upper_band"`
	MiddleBand float64 `json:"middle_band"`
	LowerBand  float64 `json:"lower_band"`
	CurrentPrice float64 `json:"current_price"`
}

// GetSnapshot fetches the indicator snapshot and maps it to the
// classifier-friendly struct.
func (c *HTTPClient) GetSnapshot(ctx context.Context, book string) (classifier.Snapshot, error) {
	var resp snapshotResponse
	url := fmt.Sprintf("%s/api/v1/indicators/%s/snapshot", c.baseURL, book)
	if err := c.getJSON(ctx, url, &resp); err != nil {
		return classifier.Snapshot{}, err
	}

	snap := classifier.Snapshot{Book: book}
	if resp.SMA != nil {
		snap.Price = resp.SMA.Value
	}
	if resp.Bollinger != nil {
		if resp.Bollinger.CurrentPrice > 0 {
			snap.Price = resp.Bollinger.CurrentPrice
		}
		upper := resp.Bollinger.Upper
		if upper == 0 {
			upper = resp.Bollinger.UpperBand
		}
		middle := resp.Bollinger.Middle
		if middle == 0 {
			middle = resp.Bollinger.MiddleBand
		}
		lower := resp.Bollinger.Lower
		if lower == 0 {
			lower = resp.Bollinger.LowerBand
		}
		snap.BBUpper = upper
		snap.BBMiddle = middle
		snap.BBLower = lower
	}
	if resp.ATR != nil {
		snap.ATR = resp.ATR.Value
	}
	if resp.EMA != nil {
		snap.EMA = resp.EMA.Value
	}
	if resp.RSI != nil {
		snap.RSI = resp.RSI.Value
	}
	return snap, nil
}

// listStrategiesResponse is the wire envelope used by strategy-executor.
type listStrategiesResponse struct {
	Strategies []StrategyInfo `json:"strategies"`
}

func (c *HTTPClient) ListStrategies(ctx context.Context) ([]StrategyInfo, error) {
	var resp listStrategiesResponse
	url := fmt.Sprintf("%s/api/v1/strategies", c.baseURL)
	if err := c.getJSON(ctx, url, &resp); err != nil {
		return nil, err
	}
	return resp.Strategies, nil
}

func (c *HTTPClient) GetStrategyState(ctx context.Context, name string) (StrategyState, error) {
	var state StrategyState
	url := fmt.Sprintf("%s/api/v1/strategies/%s/state", c.baseURL, name)
	if err := c.getJSON(ctx, url, &state); err != nil {
		return StrategyState{}, err
	}
	if state.Name == "" {
		state.Name = name
	}
	return state, nil
}

func (c *HTTPClient) StartStrategy(ctx context.Context, name string) error {
	url := fmt.Sprintf("%s/api/v1/strategies/%s/start", c.baseURL, name)
	return c.post(ctx, url)
}

func (c *HTTPClient) StopStrategy(ctx context.Context, name string) error {
	url := fmt.Sprintf("%s/api/v1/strategies/%s/stop", c.baseURL, name)
	return c.post(ctx, url)
}

func (c *HTTPClient) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: %s: %s", url, resp.Status, truncate(string(body), 200))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *HTTPClient) post(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: %s: %s", url, resp.Status, truncate(string(body), 200))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
