package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// exposureResponse matches order-management GET /api/v1/risk/exposure.
type exposureResponse struct {
	Book         string  `json:"book"`
	PositionSize float64 `json:"position_size"`
}

// Position returns order-management's base-currency position for book
// (GET /api/v1/risk/exposure?book=…), the position its own risk check uses.
func (p *OrderManagementRiskProvider) Position(ctx context.Context, book string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		p.baseURL+"/api/v1/risk/exposure?book="+url.QueryEscape(book), nil)
	if err != nil {
		return 0, err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("exposure request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("exposure status %d", resp.StatusCode)
	}
	var body exposureResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("exposure decode: %w", err)
	}
	return body.PositionSize, nil
}
