// Package ordermgmt provides HTTP clients for order-management integration.
package ordermgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client calls order-management HTTP APIs (cancel-by-signal).
type Client struct {
	baseURL string
	hc      *http.Client
}

// NewClient returns a client for baseURL (e.g. http://order-management:8082). Empty baseURL disables calls.
func NewClient(baseURL string) *Client {
	u := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if u == "" {
		return &Client{}
	}
	return &Client{
		baseURL: u,
		hc:      &http.Client{Timeout: 20 * time.Second},
	}
}

// CancelOrderBySignalID POSTs /api/v1/orders/cancel-by-signal with signal_id (event_id).
func (c *Client) CancelOrderBySignalID(ctx context.Context, signalID string) error {
	if c == nil || c.baseURL == "" {
		return fmt.Errorf("order management client not configured")
	}
	signalID = strings.TrimSpace(signalID)
	if signalID == "" {
		return fmt.Errorf("signal_id is required")
	}
	body, err := json.Marshal(map[string]string{"signal_id": signalID})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/orders/cancel-by-signal", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("order-management request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		if errBody.Error != "" {
			return fmt.Errorf("order-management: %s", errBody.Error)
		}
		return fmt.Errorf("order-management: HTTP %d", resp.StatusCode)
	}
	return nil
}
