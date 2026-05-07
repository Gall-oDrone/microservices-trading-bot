package agent

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotImplemented is returned by provider skeletons until wired.
	ErrNotImplemented = errors.New("agent provider method not implemented")
)

// Message represents a normalized provider-agnostic conversation message.
type Message struct {
	Role    string
	Content string
}

// ToolCall represents a tool invocation requested by an LLM.
type ToolCall struct {
	Name      string
	Arguments map[string]any
}

// GenerateRequest is the common request shape used by providers.
type GenerateRequest struct {
	Model       string
	System      string
	Messages    []Message
	Temperature float64
	MaxTokens   int
	Metadata    map[string]string
}

// GenerateResponse is the common response shape used by providers.
type GenerateResponse struct {
	Text      string
	ToolCalls []ToolCall
	Usage     UsageSnapshot
	RawID     string
}

// StreamEvent represents a provider stream chunk.
type StreamEvent struct {
	DeltaText string
	Done      bool
	Err       error
}

// UsageSnapshot tracks budget-sensitive metrics for one execution.
type UsageSnapshot struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	EstimatedCostUSD float64
	RunDuration      time.Duration
}

// ToolResult is a normalized tool output.
type ToolResult struct {
	Name     string
	Output   string
	Metadata map[string]string
}

// Tool defines a guarded capability exposed to agents.
type Tool interface {
	Name() string
	Description() string
	Run(ctx context.Context, args map[string]any) (ToolResult, error)
}

// ToolCallRequest is evaluated by policy prior to execution.
type ToolCallRequest struct {
	ToolName string
	Args     map[string]any
}

// PolicyEngine applies safety and budget guardrails.
type PolicyEngine interface {
	ValidateToolCall(ctx context.Context, req ToolCallRequest) error
	ValidateBudget(ctx context.Context, usage UsageSnapshot) error
}

// LLMProvider is a provider adapter interface (Anthropic/OpenAI/etc.).
type LLMProvider interface {
	Name() string
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
	Stream(ctx context.Context, req GenerateRequest) (<-chan StreamEvent, error)
}

// AgentInput is the runtime request to one agent instance.
type AgentInput struct {
	RunID        string
	Instructions string
	Context      map[string]any
}

// AgentResult is the normalized outcome of one agent run.
type AgentResult struct {
	Summary    string
	Confidence float64
	Usage      UsageSnapshot
}

// Agent is the executable interface for concrete agents.
type Agent interface {
	ID() string
	Run(ctx context.Context, input AgentInput) (AgentResult, error)
}
