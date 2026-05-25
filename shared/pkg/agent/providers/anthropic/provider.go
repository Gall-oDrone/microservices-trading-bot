package anthropic

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"bitso-trading-platform/shared/pkg/agent"
)

// Provider implements agent.LLMProvider using the official Anthropic Go SDK.
type Provider struct {
	client anthropic.Client
	model  string
}

// NewProvider creates an Anthropic provider adapter.
// API key is read from ANTHROPIC_API_KEY (SDK default via option.WithAPIKey).
func NewProvider(model string) *Provider {
	return &Provider{
		client: anthropic.NewClient(
			option.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
		),
		model: model,
	}
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return "anthropic"
}

// Generate calls the Messages API and returns normalized text.
func (p *Provider) Generate(ctx context.Context, req agent.GenerateRequest) (agent.GenerateResponse, error) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return agent.GenerateResponse{}, agent.ErrNotImplemented
	}

	messages, err := toMessageParams(req.Messages)
	if err != nil {
		return agent.GenerateResponse{}, err
	}

	maxTokens := int64(req.MaxTokens)
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	params := anthropic.MessageNewParams{
		Model:     p.resolveModel(req.Model),
		MaxTokens: maxTokens,
		Messages:  messages,
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if req.Temperature > 0 {
		params.Temperature = anthropic.Float(req.Temperature)
	}

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return agent.GenerateResponse{}, err
	}

	var text strings.Builder
	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(tb.Text)
		}
	}

	return agent.GenerateResponse{
		Text:  text.String(),
		RawID: msg.ID,
		Usage: agent.UsageSnapshot{
			PromptTokens:     int(msg.Usage.InputTokens),
			CompletionTokens: int(msg.Usage.OutputTokens),
			TotalTokens:      int(msg.Usage.InputTokens + msg.Usage.OutputTokens),
		},
	}, nil
}

// Stream streams assistant text deltas through agent.StreamEvent.
func (p *Provider) Stream(ctx context.Context, req agent.GenerateRequest) (<-chan agent.StreamEvent, error) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return nil, agent.ErrNotImplemented
	}

	messages, err := toMessageParams(req.Messages)
	if err != nil {
		return nil, err
	}

	maxTokens := int64(req.MaxTokens)
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	params := anthropic.MessageNewParams{
		Model:     p.resolveModel(req.Model),
		MaxTokens: maxTokens,
		Messages:  messages,
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if req.Temperature > 0 {
		params.Temperature = anthropic.Float(req.Temperature)
	}

	stream := p.client.Messages.NewStreaming(ctx, params)
	ch := make(chan agent.StreamEvent, 32)

	go func() {
		defer close(ch)
		defer stream.Close()

		for stream.Next() {
			event := stream.Current()
			switch ev := event.AsAny().(type) {
			case anthropic.ContentBlockDeltaEvent:
				if delta, ok := ev.Delta.AsAny().(anthropic.TextDelta); ok && delta.Text != "" {
					select {
					case <-ctx.Done():
						ch <- agent.StreamEvent{Err: ctx.Err()}
						return
					case ch <- agent.StreamEvent{DeltaText: delta.Text}:
					}
				}
			}
		}
		if err := stream.Err(); err != nil {
			ch <- agent.StreamEvent{Err: err}
			return
		}
		ch <- agent.StreamEvent{Done: true}
	}()

	return ch, nil
}

func (p *Provider) resolveModel(model string) anthropic.Model {
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = string(anthropic.ModelClaudeSonnet4_5)
	}
	return anthropic.Model(model)
}

func toMessageParams(messages []agent.Message) ([]anthropic.MessageParam, error) {
	out := make([]anthropic.MessageParam, 0, len(messages))
	for _, m := range messages {
		switch strings.ToLower(strings.TrimSpace(m.Role)) {
		case "user":
			out = append(out, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case "assistant":
			out = append(out, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		default:
			return nil, errors.New("anthropic provider: unsupported message role " + m.Role)
		}
	}
	return out, nil
}
