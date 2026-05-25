package anthropic

import (
	"context"
	"errors"
	"os"
	"testing"

	"bitso-trading-platform/shared/pkg/agent"
)

func TestGenerateWithoutAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	p := NewProvider("claude-sonnet-4-5")
	_, err := p.Generate(context.Background(), agent.GenerateRequest{
		Messages: []agent.Message{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, agent.ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
	}
}

func TestToMessageParamsRejectsUnknownRole(t *testing.T) {
	_, err := toMessageParams([]agent.Message{{Role: "system", Content: "x"}})
	if err == nil {
		t.Fatal("expected error for system role")
	}
}

func TestResolveModelDefault(t *testing.T) {
	p := NewProvider("")
	if got := string(p.resolveModel("")); got == "" {
		t.Fatal("expected non-empty default model")
	}
}
