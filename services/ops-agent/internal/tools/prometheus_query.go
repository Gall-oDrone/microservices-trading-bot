package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"bitso-trading-platform/shared/pkg/agent"
)

// PrometheusQueryTool runs read-only instant queries against Prometheus.
type PrometheusQueryTool struct {
	baseURL string
	client  *http.Client
}

func NewPrometheusQueryTool(baseURL string, timeout time.Duration) *PrometheusQueryTool {
	return &PrometheusQueryTool{
		baseURL: baseURL,
		client:  &http.Client{Timeout: timeout},
	}
}

func (t *PrometheusQueryTool) Name() string {
	return "prometheus_query"
}

func (t *PrometheusQueryTool) Description() string {
	return "Runs read-only PromQL instant queries via Prometheus HTTP API"
}

func (t *PrometheusQueryTool) Run(ctx context.Context, args map[string]any) (agent.ToolResult, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return agent.ToolResult{}, fmt.Errorf("query argument is required")
	}

	endpoint := fmt.Sprintf("%s/api/v1/query?query=%s", t.baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return agent.ToolResult{}, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return agent.ToolResult{}, err
	}
	defer resp.Body.Close()

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return agent.ToolResult{}, err
	}

	return agent.ToolResult{
		Name:   t.Name(),
		Output: "query executed",
		Metadata: map[string]string{
			"http_status": fmt.Sprintf("%d", resp.StatusCode),
		},
	}, nil
}
