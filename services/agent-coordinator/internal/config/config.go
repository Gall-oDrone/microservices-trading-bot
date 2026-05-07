package config

import (
	"os"
	"strconv"
)

type Config struct {
	Host string
	Port int

	OpsAgentURL       string
	KafkaAgentURL     string
	ExecutionAgentURL string

	ReadOnly        bool
	MaxFanoutAgents int
}

func Load() *Config {
	return &Config{
		Host:              getenv("AGENT_COORDINATOR_HOST", "0.0.0.0"),
		Port:              getenvInt("AGENT_COORDINATOR_PORT", 8091),
		OpsAgentURL:       getenv("OPS_AGENT_URL", "http://localhost:8090"),
		KafkaAgentURL:     getenv("KAFKA_AGENT_URL", ""),
		ExecutionAgentURL: getenv("EXECUTION_AGENT_URL", ""),
		ReadOnly:          getenv("AGENT_COORDINATOR_READ_ONLY", "true") != "false",
		MaxFanoutAgents:   getenvInt("AGENT_COORDINATOR_MAX_FANOUT", 3),
	}
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
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
