package config

import "testing"

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("OPS_AGENT_SERVICE_NAME", "")
	t.Setenv("OPS_AGENT_HOST", "")
	t.Setenv("OPS_AGENT_PORT", "")
	t.Setenv("OPS_AGENT_PROVIDER", "")
	t.Setenv("OPS_AGENT_MODEL", "")
	t.Setenv("OPS_AGENT_LANGCHAIN_ENABLED", "")
	t.Setenv("OPS_AGENT_LANGCHAIN_URL", "")

	cfg := Load()
	if cfg.ServiceName != "ops-agent" {
		t.Fatalf("unexpected default service name: %s", cfg.ServiceName)
	}
	if cfg.Port != 8090 {
		t.Fatalf("unexpected default port: %d", cfg.Port)
	}
	if cfg.Provider != "anthropic" {
		t.Fatalf("unexpected default provider: %s", cfg.Provider)
	}
	if cfg.EnableLangChain {
		t.Fatalf("expected langchain disabled by default")
	}
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("OPS_AGENT_PORT", "9010")
	t.Setenv("OPS_AGENT_PROVIDER", "openai")
	t.Setenv("OPS_AGENT_LANGCHAIN_ENABLED", "true")
	t.Setenv("OPS_AGENT_LANGCHAIN_URL", "http://langchain:8000")

	cfg := Load()
	if cfg.Port != 9010 {
		t.Fatalf("unexpected port override: %d", cfg.Port)
	}
	if cfg.Provider != "openai" {
		t.Fatalf("unexpected provider override: %s", cfg.Provider)
	}
	if !cfg.EnableLangChain {
		t.Fatalf("expected langchain enabled")
	}
	if cfg.LangChainURL != "http://langchain:8000" {
		t.Fatalf("unexpected langchain url: %s", cfg.LangChainURL)
	}
}
