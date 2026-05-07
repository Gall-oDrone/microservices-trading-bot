package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/agent"
)

type Incident struct {
	Source      string            `json:"source"`
	Severity    string            `json:"severity"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
}

type Report struct {
	ID              string            `json:"id"`
	CreatedAt       time.Time         `json:"created_at"`
	Incident        Incident          `json:"incident"`
	Summary         string            `json:"summary"`
	Confidence      float64           `json:"confidence"`
	Recommendations []string          `json:"recommendations"`
	ToolOutputs     map[string]string `json:"tool_outputs"`
}

type OpsAgent struct {
	id       string
	provider agent.LLMProvider
	policy   agent.PolicyEngine
	tools    map[string]agent.Tool
}

func NewOpsAgent(provider agent.LLMProvider, policy agent.PolicyEngine, tools []agent.Tool) *OpsAgent {
	toolMap := make(map[string]agent.Tool, len(tools))
	for _, t := range tools {
		toolMap[t.Name()] = t
	}
	return &OpsAgent{
		id:       "ops-agent-v1",
		provider: provider,
		policy:   policy,
		tools:    toolMap,
	}
}

func (a *OpsAgent) ID() string {
	return a.id
}

func (a *OpsAgent) TriageIncident(ctx context.Context, id string, incident Incident) (Report, error) {
	outputs := map[string]string{}
	recommendations := []string{
		"Confirm alert scope in Prometheus and service health endpoints.",
		"Check recent deploys and service logs for the impacted component.",
	}

	// Phase-1 read-only execution: tool allowlist + deterministic checks.
	if t, ok := a.tools["http_healthcheck"]; ok {
		if err := a.policy.ValidateToolCall(ctx, agent.ToolCallRequest{ToolName: t.Name()}); err == nil {
			result, runErr := t.Run(ctx, map[string]any{"url": "http://api-gateway:8085/health"})
			if runErr == nil {
				outputs[result.Name] = result.Output
			} else {
				outputs[result.Name] = runErr.Error()
			}
		}
	}

	if t, ok := a.tools["prometheus_query"]; ok {
		if err := a.policy.ValidateToolCall(ctx, agent.ToolCallRequest{ToolName: t.Name()}); err == nil {
			result, runErr := t.Run(ctx, map[string]any{"query": "up"})
			if runErr == nil {
				outputs[result.Name] = result.Output
			} else {
				outputs[t.Name()] = runErr.Error()
			}
		}
	}

	summary := fmt.Sprintf("Incident '%s' (%s) from %s. Initial read-only diagnostics collected.",
		incident.Title, strings.ToLower(incident.Severity), incident.Source)

	// Provider call remains optional in phase 1; if unwired we still return useful output.
	if a.provider != nil {
		_, _ = a.provider.Generate(ctx, agent.GenerateRequest{
			Model:  "",
			System: "You are an SRE incident triage assistant.",
			Messages: []agent.Message{
				{Role: "user", Content: incident.Description},
			},
			Temperature: 0.1,
			MaxTokens:   400,
		})
	}

	return Report{
		ID:              id,
		CreatedAt:       time.Now().UTC(),
		Incident:        incident,
		Summary:         summary,
		Confidence:      0.65,
		Recommendations: recommendations,
		ToolOutputs:     outputs,
	}, nil
}
