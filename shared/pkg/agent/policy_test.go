package agent

import (
	"context"
	"errors"
	"testing"
)

func TestStaticPolicyEngine_ValidateToolCall(t *testing.T) {
	policy := NewStaticPolicyEngine(PolicyConfig{
		AllowedTools: map[string]struct{}{
			"prometheus_query": {},
		},
	})

	err := policy.ValidateToolCall(context.Background(), ToolCallRequest{ToolName: "prometheus_query"})
	if err != nil {
		t.Fatalf("expected allowed tool to pass, got error: %v", err)
	}

	err = policy.ValidateToolCall(context.Background(), ToolCallRequest{ToolName: "k8s_logs"})
	if !errors.Is(err, ErrToolNotAllowed) {
		t.Fatalf("expected ErrToolNotAllowed, got: %v", err)
	}
}

func TestStaticPolicyEngine_ValidateBudget(t *testing.T) {
	policy := NewStaticPolicyEngine(PolicyConfig{
		Budget: BudgetConfig{
			MaxTotalTokens: 5000,
			MaxCostUSD:     0.50,
		},
	})

	okUsage := UsageSnapshot{
		TotalTokens:      1000,
		EstimatedCostUSD: 0.10,
	}
	if err := policy.ValidateBudget(context.Background(), okUsage); err != nil {
		t.Fatalf("expected valid budget usage, got error: %v", err)
	}

	tooManyTokens := UsageSnapshot{
		TotalTokens:      6000,
		EstimatedCostUSD: 0.10,
	}
	if err := policy.ValidateBudget(context.Background(), tooManyTokens); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected token budget error, got: %v", err)
	}

	tooExpensive := UsageSnapshot{
		TotalTokens:      1000,
		EstimatedCostUSD: 1.20,
	}
	if err := policy.ValidateBudget(context.Background(), tooExpensive); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected cost budget error, got: %v", err)
	}
}
