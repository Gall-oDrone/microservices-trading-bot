package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ResearchAgentClient calls the cold-path research-agent HTTP API.
type ResearchAgentClient interface {
	RunResearch(ctx context.Context, ticker, tradeDate string, includeNews bool) (map[string]interface{}, error)
}

type researchAgentHTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewResearchAgentClient creates a client for research-agent. Returns nil if baseURL is empty.
func NewResearchAgentClient(baseURL string, timeout time.Duration) ResearchAgentClient {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &researchAgentHTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

type researchRunBody struct {
	Ticker             string `json:"ticker"`
	TradeDate          string `json:"trade_date"`
	IncludeNewsContext bool   `json:"include_news_context"`
}

func (c *researchAgentHTTPClient) RunResearch(ctx context.Context, ticker, tradeDate string, includeNews bool) (map[string]interface{}, error) {
	body, err := json.Marshal(researchRunBody{
		Ticker:             ticker,
		TradeDate:          tradeDate,
		IncludeNewsContext: includeNews,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/research/run", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("research-agent request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("research-agent status %d: %s", resp.StatusCode, string(data))
	}

	var memo map[string]interface{}
	if err := json.Unmarshal(data, &memo); err != nil {
		return nil, fmt.Errorf("decode research memo: %w", err)
	}
	return memo, nil
}
