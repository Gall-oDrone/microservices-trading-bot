package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ServiceName string
	Host        string
	Port        int

	Provider string
	Model    string

	ReadOnly        bool
	EnableLangChain bool
	LangChainURL    string

	MaxTokens int
	MaxCost   float64

	PrometheusBaseURL string
}

func Load() *Config {
	return &Config{
		ServiceName:       getenv("OPS_AGENT_SERVICE_NAME", "ops-agent"),
		Host:              getenv("OPS_AGENT_HOST", "0.0.0.0"),
		Port:              getenvInt("OPS_AGENT_PORT", 8090),
		Provider:          getenv("OPS_AGENT_PROVIDER", "anthropic"),
		Model:             getenv("OPS_AGENT_MODEL", "claude-sonnet-4-5"),
		ReadOnly:          getenv("OPS_AGENT_READ_ONLY", "true") != "false",
		EnableLangChain:   getenv("OPS_AGENT_LANGCHAIN_ENABLED", "false") == "true",
		LangChainURL:      getenv("OPS_AGENT_LANGCHAIN_URL", ""),
		MaxTokens:         getenvInt("OPS_AGENT_MAX_TOKENS", 12000),
		MaxCost:           getenvFloat("OPS_AGENT_MAX_COST_USD", 1.0),
		PrometheusBaseURL: getenv("OPS_AGENT_PROMETHEUS_URL", "http://localhost:9090"),
	}
}

func DefaultHTTPTimeout() time.Duration {
	return 10 * time.Second
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getenvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

func getenvFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}
