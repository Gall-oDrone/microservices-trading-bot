package agent

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrToolNotAllowed indicates policy rejection for a tool.
	ErrToolNotAllowed = errors.New("tool is not allowed by policy")
	// ErrBudgetExceeded indicates policy rejection due to usage limits.
	ErrBudgetExceeded = errors.New("agent budget exceeded")
)

// BudgetConfig defines simple run-level limits.
type BudgetConfig struct {
	MaxTotalTokens int
	MaxCostUSD     float64
}

// PolicyConfig configures execution safety and budgets.
type PolicyConfig struct {
	AllowedTools map[string]struct{}
	Budget       BudgetConfig
}

// StaticPolicyEngine enforces immutable allowlists and budgets.
type StaticPolicyEngine struct {
	config PolicyConfig
}

// NewStaticPolicyEngine builds a policy engine with static configuration.
func NewStaticPolicyEngine(cfg PolicyConfig) *StaticPolicyEngine {
	return &StaticPolicyEngine{config: cfg}
}

// ValidateToolCall checks whether a tool is explicitly allowlisted.
func (p *StaticPolicyEngine) ValidateToolCall(_ context.Context, req ToolCallRequest) error {
	if len(p.config.AllowedTools) == 0 {
		return fmt.Errorf("%w: no tools configured", ErrToolNotAllowed)
	}
	if _, ok := p.config.AllowedTools[req.ToolName]; !ok {
		return fmt.Errorf("%w: %s", ErrToolNotAllowed, req.ToolName)
	}
	return nil
}

// ValidateBudget checks run-level token and estimated cost caps.
func (p *StaticPolicyEngine) ValidateBudget(_ context.Context, usage UsageSnapshot) error {
	if p.config.Budget.MaxTotalTokens > 0 && usage.TotalTokens > p.config.Budget.MaxTotalTokens {
		return fmt.Errorf("%w: total tokens %d > %d", ErrBudgetExceeded, usage.TotalTokens, p.config.Budget.MaxTotalTokens)
	}
	if p.config.Budget.MaxCostUSD > 0 && usage.EstimatedCostUSD > p.config.Budget.MaxCostUSD {
		return fmt.Errorf("%w: cost %.4f > %.4f", ErrBudgetExceeded, usage.EstimatedCostUSD, p.config.Budget.MaxCostUSD)
	}
	return nil
}
