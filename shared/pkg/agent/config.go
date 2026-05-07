package agent

import "os"

// RuntimeConfig contains toggles and defaults for agent runtime behavior.
type RuntimeConfig struct {
	Enabled      bool
	ReadOnlyMode bool
	KillSwitch   bool
	DefaultModel string
	Provider     string
}

// LoadRuntimeConfigFromEnv loads runtime toggles from environment variables.
func LoadRuntimeConfigFromEnv() RuntimeConfig {
	return RuntimeConfig{
		Enabled:      os.Getenv("AGENT_ENABLED") == "true",
		ReadOnlyMode: os.Getenv("AGENT_READ_ONLY") != "false",
		KillSwitch:   os.Getenv("AGENT_KILL_SWITCH") == "true",
		DefaultModel: getenvDefault("AGENT_DEFAULT_MODEL", "claude-sonnet-4-5"),
		Provider:     getenvDefault("AGENT_PROVIDER", "anthropic"),
	}
}

func getenvDefault(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}
