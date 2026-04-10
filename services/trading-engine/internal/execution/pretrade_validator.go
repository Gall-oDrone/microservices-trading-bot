package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PreTradeValidator validates orders with order-management before execution
type PreTradeValidator interface {
	ValidateOrder(ctx context.Context, req *OrderValidationRequest) (*OrderValidationResponse, error)
}

// OrderValidationRequest represents a pre-trade validation request
type OrderValidationRequest struct {
	Book     string  `json:"book"`
	Side     string  `json:"side"`
	Type     string  `json:"type"`
	Amount   float64 `json:"amount"`
	Price    float64 `json:"price"`
	SignalID string  `json:"signal_id,omitempty"`
	Strategy string  `json:"strategy,omitempty"`
}

// OrderValidationResponse represents the validation result from order-management
type OrderValidationResponse struct {
	Valid       bool     `json:"valid"`
	Approved    bool     `json:"approved"`
	Errors      []string `json:"errors,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	ValidatedAt string   `json:"validated_at"`
	// IdempotentReplay: OM already had the signal row; risk was applied at trading.signals ingest.
	IdempotentReplay bool `json:"idempotent_replay,omitempty"`
}

// HTTPPreTradeValidator implements PreTradeValidator using HTTP calls to order-management
type HTTPPreTradeValidator struct {
	baseURL    string
	httpClient *http.Client
}

// NewHTTPPreTradeValidator creates a new HTTP-based pre-trade validator.
// baseURL should be the order-management service URL (e.g., "http://order-management:8082")
func NewHTTPPreTradeValidator(baseURL string, timeout time.Duration) *HTTPPreTradeValidator {
	return &HTTPPreTradeValidator{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// ValidateOrder calls order-management's /api/v1/orders/validate endpoint
func (v *HTTPPreTradeValidator) ValidateOrder(ctx context.Context, req *OrderValidationRequest) (*OrderValidationResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/orders/validate", v.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := v.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result OrderValidationResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return &result, nil
}

// NoOpPreTradeValidator is a validator that always approves (for backward compatibility or dry-run)
type NoOpPreTradeValidator struct{}

// ValidateOrder always returns approved
func (v *NoOpPreTradeValidator) ValidateOrder(ctx context.Context, req *OrderValidationRequest) (*OrderValidationResponse, error) {
	return &OrderValidationResponse{
		Valid:       true,
		Approved:    true,
		ValidatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}
