package agent

import (
	"context"
	"os"
	"time"
)

// TraceRecord captures one workflow span for observability systems.
type TraceRecord struct {
	RunID       string
	Name        string
	Status      string
	StartedAt   time.Time
	FinishedAt  time.Time
	Metadata    map[string]string
	ErrorReason string
}

// TraceClient abstracts LangSmith or other tracing backends.
type TraceClient interface {
	StartRun(ctx context.Context, record TraceRecord) (string, error)
	FinishRun(ctx context.Context, runKey string, record TraceRecord) error
}

// LangSmithConfig holds minimal configuration needed to enable tracing.
type LangSmithConfig struct {
	Enabled bool
	APIKey  string
	Project string
	APIURL  string
}

// LoadLangSmithConfigFromEnv loads tracing settings from environment.
func LoadLangSmithConfigFromEnv() LangSmithConfig {
	return LangSmithConfig{
		Enabled: os.Getenv("LANGSMITH_TRACING") == "true",
		APIKey:  os.Getenv("LANGSMITH_API_KEY"),
		Project: getenvDefault("LANGSMITH_PROJECT", "microservices-trading-bot"),
		APIURL:  getenvDefault("LANGSMITH_ENDPOINT", "https://api.smith.langchain.com"),
	}
}

// NoopTraceClient is a safe default when tracing is disabled.
type NoopTraceClient struct{}

func (NoopTraceClient) StartRun(_ context.Context, _ TraceRecord) (string, error) {
	return "", nil
}

func (NoopTraceClient) FinishRun(_ context.Context, _ string, _ TraceRecord) error {
	return nil
}
