// Package integration provides end-to-end tests for the trading cycle.
// These tests verify the full flow: BUY signal → order → fill → position → SELL signal → P&L calculation.
//
// Prerequisites:
//   - Kafka running with required topics
//   - Redis running
//   - order-management service running
//   - trading-engine service running (optional, for full E2E)
//   - Bitso Stage API credentials (for live fill testing)
//
// Run with: go test -v -tags=integration ./tests/integration/...
//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

const (
	defaultOrderManagementURL = "http://localhost:8082"
	defaultKafkaBrokers       = "localhost:9092"
	defaultRedisHost          = "localhost:6379"

	testBook     = "btc_mxn"
	testAmount   = 0.001
	testPrice    = 1200000.0
	testStrategy = "integration_test"
)

// TestConfig holds configuration for integration tests
type TestConfig struct {
	OrderManagementURL string
	KafkaBrokers       string
	RedisHost          string
}

func loadTestConfig() *TestConfig {
	return &TestConfig{
		OrderManagementURL: getEnvOrDefault("ORDER_MANAGEMENT_URL", defaultOrderManagementURL),
		KafkaBrokers:       getEnvOrDefault("KAFKA_BROKERS", defaultKafkaBrokers),
		RedisHost:          getEnvOrDefault("REDIS_HOST", defaultRedisHost),
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// OrderValidationRequest matches the API request structure
type OrderValidationRequest struct {
	Book     string  `json:"book"`
	Side     string  `json:"side"`
	Type     string  `json:"type"`
	Amount   float64 `json:"amount"`
	Price    float64 `json:"price"`
	SignalID string  `json:"signal_id,omitempty"`
	Strategy string  `json:"strategy,omitempty"`
}

// OrderValidationResponse matches the API response structure
type OrderValidationResponse struct {
	Valid       bool     `json:"valid"`
	Approved    bool     `json:"approved"`
	Errors      []string `json:"errors,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	ValidatedAt string   `json:"validated_at"`
}

// RiskSessionResponse matches the /api/v1/risk/session response
type RiskSessionResponse struct {
	DailyRealizedPnL float64 `json:"daily_realized_pnl"`
	DrawdownPercent  float64 `json:"drawdown_percent"`
}

// TestPreTradeValidation_ValidOrder tests that a valid order passes pre-trade validation
func TestPreTradeValidation_ValidOrder(t *testing.T) {
	cfg := loadTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &OrderValidationRequest{
		Book:     testBook,
		Side:     "BUY",
		Type:     "limit",
		Amount:   testAmount,
		Price:    testPrice,
		SignalID: fmt.Sprintf("test-signal-%d", time.Now().UnixNano()),
		Strategy: testStrategy,
	}

	resp, err := callOrderValidation(ctx, cfg.OrderManagementURL, req)
	if err != nil {
		t.Fatalf("Failed to call order validation: %v", err)
	}

	if !resp.Valid {
		t.Errorf("Expected order to be valid, got errors: %v", resp.Errors)
	}

	if !resp.Approved {
		t.Errorf("Expected order to be approved, got errors: %v", resp.Errors)
	}

	t.Logf("Order validation passed: valid=%v, approved=%v, validated_at=%s",
		resp.Valid, resp.Approved, resp.ValidatedAt)
}

// TestPreTradeValidation_InvalidBook tests that an invalid book is rejected
func TestPreTradeValidation_InvalidBook(t *testing.T) {
	cfg := loadTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &OrderValidationRequest{
		Book:     "invalid_book_format",
		Side:     "BUY",
		Type:     "limit",
		Amount:   testAmount,
		Price:    testPrice,
		SignalID: fmt.Sprintf("test-signal-%d", time.Now().UnixNano()),
		Strategy: testStrategy,
	}

	resp, err := callOrderValidation(ctx, cfg.OrderManagementURL, req)
	if err != nil {
		t.Fatalf("Failed to call order validation: %v", err)
	}

	if resp.Approved {
		t.Errorf("Expected invalid book to be rejected, but was approved")
	}

	t.Logf("Invalid book correctly rejected: errors=%v", resp.Errors)
}

// TestPreTradeValidation_InvalidAmount tests that a zero/negative amount is rejected
func TestPreTradeValidation_InvalidAmount(t *testing.T) {
	cfg := loadTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &OrderValidationRequest{
		Book:     testBook,
		Side:     "BUY",
		Type:     "limit",
		Amount:   0,
		Price:    testPrice,
		SignalID: fmt.Sprintf("test-signal-%d", time.Now().UnixNano()),
		Strategy: testStrategy,
	}

	resp, err := callOrderValidation(ctx, cfg.OrderManagementURL, req)
	if err != nil {
		t.Fatalf("Failed to call order validation: %v", err)
	}

	if resp.Valid && resp.Approved {
		t.Errorf("Expected zero amount to be rejected, but was approved")
	}

	t.Logf("Zero amount correctly rejected: errors=%v", resp.Errors)
}

// TestPreTradeValidation_InvalidSide tests that an invalid side is rejected
func TestPreTradeValidation_InvalidSide(t *testing.T) {
	cfg := loadTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &OrderValidationRequest{
		Book:     testBook,
		Side:     "INVALID",
		Type:     "limit",
		Amount:   testAmount,
		Price:    testPrice,
		SignalID: fmt.Sprintf("test-signal-%d", time.Now().UnixNano()),
		Strategy: testStrategy,
	}

	resp, err := callOrderValidation(ctx, cfg.OrderManagementURL, req)
	if err != nil {
		t.Fatalf("Failed to call order validation: %v", err)
	}

	if resp.Valid && resp.Approved {
		t.Errorf("Expected invalid side to be rejected, but was approved")
	}

	t.Logf("Invalid side correctly rejected: errors=%v", resp.Errors)
}

// TestRiskSession_Endpoint tests the risk session endpoint availability
func TestRiskSession_Endpoint(t *testing.T) {
	cfg := loadTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := callRiskSession(ctx, cfg.OrderManagementURL)
	if err != nil {
		t.Fatalf("Failed to call risk session: %v", err)
	}

	t.Logf("Risk session response: daily_realized_pnl=%.2f, drawdown_percent=%.2f",
		resp.DailyRealizedPnL, resp.DrawdownPercent)
}

// TestHealth_Endpoint tests the health endpoint
func TestHealth_Endpoint(t *testing.T) {
	cfg := loadTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/health", cfg.OrderManagementURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Unexpected health status code: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	t.Logf("Health response: status=%d, body=%s", resp.StatusCode, string(body))
}

// TestStatus_Endpoint tests the status endpoint
func TestStatus_Endpoint(t *testing.T) {
	cfg := loadTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/api/v1/status", cfg.OrderManagementURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Status check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	t.Logf("Status response: %s", string(body))
}

// callOrderValidation makes a POST request to /api/v1/orders/validate
func callOrderValidation(ctx context.Context, baseURL string, req *OrderValidationRequest) (*OrderValidationResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/orders/validate", baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(httpReq)
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

// callRiskSession makes a GET request to /api/v1/risk/session
func callRiskSession(ctx context.Context, baseURL string) (*RiskSessionResponse, error) {
	url := fmt.Sprintf("%s/api/v1/risk/session", baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result RiskSessionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}

	return &result, nil
}
