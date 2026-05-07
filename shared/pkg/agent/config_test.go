package agent

import "testing"

func TestLoadRuntimeConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv("AGENT_ENABLED", "")
	t.Setenv("AGENT_READ_ONLY", "")
	t.Setenv("AGENT_KILL_SWITCH", "")
	t.Setenv("AGENT_DEFAULT_MODEL", "")
	t.Setenv("AGENT_PROVIDER", "")

	cfg := LoadRuntimeConfigFromEnv()
	if cfg.Enabled {
		t.Fatal("expected AGENT_ENABLED default false")
	}
	if !cfg.ReadOnlyMode {
		t.Fatal("expected AGENT_READ_ONLY default true")
	}
	if cfg.KillSwitch {
		t.Fatal("expected AGENT_KILL_SWITCH default false")
	}
	if cfg.DefaultModel != "claude-sonnet-4-5" {
		t.Fatalf("unexpected default model: %s", cfg.DefaultModel)
	}
	if cfg.Provider != "anthropic" {
		t.Fatalf("unexpected default provider: %s", cfg.Provider)
	}
}

func TestLoadRuntimeConfigFromEnv_Overrides(t *testing.T) {
	t.Setenv("AGENT_ENABLED", "true")
	t.Setenv("AGENT_READ_ONLY", "false")
	t.Setenv("AGENT_KILL_SWITCH", "true")
	t.Setenv("AGENT_DEFAULT_MODEL", "gpt-4.1")
	t.Setenv("AGENT_PROVIDER", "openai")

	cfg := LoadRuntimeConfigFromEnv()
	if !cfg.Enabled {
		t.Fatal("expected enabled true")
	}
	if cfg.ReadOnlyMode {
		t.Fatal("expected read only false")
	}
	if !cfg.KillSwitch {
		t.Fatal("expected kill switch true")
	}
	if cfg.DefaultModel != "gpt-4.1" {
		t.Fatalf("unexpected model: %s", cfg.DefaultModel)
	}
	if cfg.Provider != "openai" {
		t.Fatalf("unexpected provider: %s", cfg.Provider)
	}
}
