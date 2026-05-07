package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ChildAgent interface {
	Name() string
	Triage(ctx context.Context, incident Incident) (AgentRecommendation, error)
}

type HTTPOpsAgentClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPOpsAgentClient(baseURL string, timeout time.Duration) *HTTPOpsAgentClient {
	return &HTTPOpsAgentClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *HTTPOpsAgentClient) Name() string {
	return "ops-agent"
}

func (c *HTTPOpsAgentClient) Triage(ctx context.Context, incident Incident) (AgentRecommendation, error) {
	payload, err := json.Marshal(incident)
	if err != nil {
		return AgentRecommendation{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/ops-agent/run", bytes.NewReader(payload))
	if err != nil {
		return AgentRecommendation{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return AgentRecommendation{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return AgentRecommendation{}, fmt.Errorf("ops-agent returned status %d", resp.StatusCode)
	}

	var report struct {
		Summary         string   `json:"summary"`
		Confidence      float64  `json:"confidence"`
		Recommendations []string `json:"recommendations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return AgentRecommendation{}, err
	}

	return AgentRecommendation{
		Agent:           c.Name(),
		Summary:         report.Summary,
		Confidence:      report.Confidence,
		Recommendations: report.Recommendations,
	}, nil
}
