package agent

import "testing"

func TestLoadLangSmithConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv("LANGSMITH_TRACING", "")
	t.Setenv("LANGSMITH_API_KEY", "")
	t.Setenv("LANGSMITH_PROJECT", "")
	t.Setenv("LANGSMITH_ENDPOINT", "")

	cfg := LoadLangSmithConfigFromEnv()
	if cfg.Enabled {
		t.Fatal("expected tracing disabled by default")
	}
	if cfg.Project != "microservices-trading-bot" {
		t.Fatalf("unexpected default project: %s", cfg.Project)
	}
	if cfg.APIURL != "https://api.smith.langchain.com" {
		t.Fatalf("unexpected default endpoint: %s", cfg.APIURL)
	}
}

func TestLoadLangSmithConfigFromEnv_Overrides(t *testing.T) {
	t.Setenv("LANGSMITH_TRACING", "true")
	t.Setenv("LANGSMITH_API_KEY", "key-123")
	t.Setenv("LANGSMITH_PROJECT", "ops-prod")
	t.Setenv("LANGSMITH_ENDPOINT", "https://example.langsmith.local")

	cfg := LoadLangSmithConfigFromEnv()
	if !cfg.Enabled {
		t.Fatal("expected tracing enabled")
	}
	if cfg.APIKey != "key-123" {
		t.Fatalf("unexpected key: %s", cfg.APIKey)
	}
	if cfg.Project != "ops-prod" {
		t.Fatalf("unexpected project: %s", cfg.Project)
	}
	if cfg.APIURL != "https://example.langsmith.local" {
		t.Fatalf("unexpected endpoint: %s", cfg.APIURL)
	}
}
