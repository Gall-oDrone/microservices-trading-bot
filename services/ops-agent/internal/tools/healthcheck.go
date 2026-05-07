package tools

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"bitso-trading-platform/shared/pkg/agent"
)

// HTTPHealthCheckTool is a read-only tool for endpoint status checks.
type HTTPHealthCheckTool struct {
	client *http.Client
}

func NewHTTPHealthCheckTool(timeout time.Duration) *HTTPHealthCheckTool {
	return &HTTPHealthCheckTool{
		client: &http.Client{Timeout: timeout},
	}
}

func (t *HTTPHealthCheckTool) Name() string {
	return "http_healthcheck"
}

func (t *HTTPHealthCheckTool) Description() string {
	return "Checks whether an HTTP endpoint responds successfully"
}

func (t *HTTPHealthCheckTool) Run(ctx context.Context, args map[string]any) (agent.ToolResult, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return agent.ToolResult{}, fmt.Errorf("url argument is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return agent.ToolResult{}, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return agent.ToolResult{
			Name:   t.Name(),
			Output: fmt.Sprintf("down: %v", err),
			Metadata: map[string]string{
				"url":    url,
				"status": "down",
			},
		}, nil
	}
	defer resp.Body.Close()

	status := "up"
	if resp.StatusCode >= 400 {
		status = "degraded"
	}

	return agent.ToolResult{
		Name:   t.Name(),
		Output: fmt.Sprintf("status_code=%d", resp.StatusCode),
		Metadata: map[string]string{
			"url":         url,
			"status":      status,
			"status_code": fmt.Sprintf("%d", resp.StatusCode),
		},
	}, nil
}
