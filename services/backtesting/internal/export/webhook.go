package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const webhookTimeout = 10 * time.Second

// WebhookNotifier POSTs the completion event as JSON to a URL
type WebhookNotifier struct {
	url    string
	client *http.Client
}

// NewWebhookNotifier creates a webhook notifier
func NewWebhookNotifier(url string) *WebhookNotifier {
	return &WebhookNotifier{
		url: url,
		client: &http.Client{
			Timeout: webhookTimeout,
		},
	}
}

// Notify sends the event to the webhook URL
func (w *WebhookNotifier) Notify(ctx context.Context, event *BacktestCompletionEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// Name returns the notifier name
func (w *WebhookNotifier) Name() string {
	return "webhook"
}
