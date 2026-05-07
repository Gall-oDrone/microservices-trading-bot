package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("AGENT_COORDINATOR_HOST", "")
	t.Setenv("AGENT_COORDINATOR_PORT", "")
	t.Setenv("OPS_AGENT_URL", "")
	t.Setenv("AGENT_COORDINATOR_READ_ONLY", "")
	t.Setenv("AGENT_COORDINATOR_MAX_FANOUT", "")

	cfg := Load()
	if cfg.Port != 8091 {
		t.Fatalf("unexpected default port: %d", cfg.Port)
	}
	if cfg.OpsAgentURL != "http://localhost:8090" {
		t.Fatalf("unexpected default ops-agent URL: %s", cfg.OpsAgentURL)
	}
	if !cfg.ReadOnly {
		t.Fatalf("expected read-only default true")
	}
	if cfg.MaxFanoutAgents != 3 {
		t.Fatalf("unexpected default max fanout: %d", cfg.MaxFanoutAgents)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("AGENT_COORDINATOR_PORT", "9100")
	t.Setenv("OPS_AGENT_URL", "http://ops-agent:8090")
	t.Setenv("AGENT_COORDINATOR_READ_ONLY", "false")
	t.Setenv("AGENT_COORDINATOR_MAX_FANOUT", "2")

	cfg := Load()
	if cfg.Port != 9100 {
		t.Fatalf("unexpected override port: %d", cfg.Port)
	}
	if cfg.OpsAgentURL != "http://ops-agent:8090" {
		t.Fatalf("unexpected override URL: %s", cfg.OpsAgentURL)
	}
	if cfg.ReadOnly {
		t.Fatalf("expected read-only false")
	}
	if cfg.MaxFanoutAgents != 2 {
		t.Fatalf("unexpected max fanout override: %d", cfg.MaxFanoutAgents)
	}
}
