package agentruntime

import (
	"context"
	"testing"

	"bitso-trading-platform/shared/pkg/agent"
)

type mockTool struct {
	name string
}

func (m mockTool) Name() string        { return m.name }
func (m mockTool) Description() string { return "mock" }
func (m mockTool) Run(_ context.Context, _ map[string]any) (agent.ToolResult, error) {
	return agent.ToolResult{Name: m.name, Output: "ok"}, nil
}

func TestOpsAgent_TriageIncident(t *testing.T) {
	policy := agent.NewStaticPolicyEngine(agent.PolicyConfig{
		AllowedTools: map[string]struct{}{
			"http_healthcheck": {},
			"prometheus_query": {},
		},
	})

	a := NewOpsAgent(nil, policy, []agent.Tool{
		mockTool{name: "http_healthcheck"},
		mockTool{name: "prometheus_query"},
	})

	report, err := a.TriageIncident(context.Background(), "run-1", Incident{
		Source:      "alertmanager",
		Severity:    "critical",
		Title:       "Service down",
		Description: "api-gateway is unreachable",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.ID != "run-1" {
		t.Fatalf("unexpected report id: %s", report.ID)
	}
	if report.ToolOutputs["http_healthcheck"] == "" {
		t.Fatalf("expected tool output for http_healthcheck")
	}
	if len(report.Recommendations) == 0 {
		t.Fatalf("expected recommendations in report")
	}
}
