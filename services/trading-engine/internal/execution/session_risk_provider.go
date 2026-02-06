package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OrderManagementRiskProvider calls order-management GET /api/v1/risk/session for session risk metrics
type OrderManagementRiskProvider struct {
	baseURL    string
	httpClient *http.Client
}

// riskSessionResponse matches order-management GET /api/v1/risk/session response
type riskSessionResponse struct {
	DailyRealizedPnL float64 `json:"daily_realized_pnl"`
	DrawdownPercent  float64 `json:"drawdown_percent"`
}

// NewOrderManagementRiskProvider creates a provider that calls the given order-management base URL
func NewOrderManagementRiskProvider(baseURL string) *OrderManagementRiskProvider {
	if baseURL == "" {
		return nil
	}
	return &OrderManagementRiskProvider{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// GetSessionRisk returns daily realized P&L and drawdown % from order-management
func (p *OrderManagementRiskProvider) GetSessionRisk(ctx context.Context) (dailyRealizedPnL, drawdownPct float64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/v1/risk/session", nil)
	if err != nil {
		return 0, 0, err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("risk session request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("risk session status %d", resp.StatusCode)
	}
	var body riskSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, 0, fmt.Errorf("risk session decode: %w", err)
	}
	return body.DailyRealizedPnL, body.DrawdownPercent, nil
}
