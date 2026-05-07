package openai

import (
	"context"

	"bitso-trading-platform/shared/pkg/agent"
)

// Provider is a skeleton OpenAI adapter implementing agent.LLMProvider.
// Concrete SDK wiring is intentionally deferred to phase 1.
type Provider struct {
	model string
}

// NewProvider creates an OpenAI provider adapter.
func NewProvider(model string) *Provider {
	return &Provider{model: model}
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return "openai"
}

// Generate returns not implemented until SDK wiring is added.
func (p *Provider) Generate(_ context.Context, _ agent.GenerateRequest) (agent.GenerateResponse, error) {
	return agent.GenerateResponse{}, agent.ErrNotImplemented
}

// Stream returns not implemented until SDK wiring is added.
func (p *Provider) Stream(_ context.Context, _ agent.GenerateRequest) (<-chan agent.StreamEvent, error) {
	return nil, agent.ErrNotImplemented
}
