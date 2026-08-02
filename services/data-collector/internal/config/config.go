package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the data-collector service.
type Config struct {
	ServiceName string
	HTTPPort    string

	BitsoWSURL string
	BitsoBook  string

	WSReconnectAttempts int
	WSReconnectInterval time.Duration
	WSReconnectMaxDelay time.Duration

	// S3 archive
	S3Bucket          string
	S3Prefix          string
	S3Region          string
	FlushInterval     time.Duration
	FlushMaxRows      int
	EnableS3          bool

	// Postgres hot store
	PostgresDSN           string
	HotRetentionDays      int
	EnablePostgres        bool
	PostgresWriteBatchSize int

	// Health / dead-man's switch
	HealthStaleAfter time.Duration
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() (*Config, error) {
	for _, path := range []string{".env", "../.env", "../../.env"} {
		if _, err := os.Stat(path); err == nil {
			_ = godotenv.Load(path)
			break
		}
	}

	cfg := &Config{
		ServiceName: getEnv("SERVICE_NAME", "data-collector"),
		HTTPPort:    getEnv("HTTP_PORT", "8085"),

		BitsoWSURL: getEnv("BITSO_WS_URL", "wss://ws.bitso.com"),
		BitsoBook:  getEnv("BITSO_BOOK", "btc_mxn"),

		WSReconnectAttempts: getEnvAsInt("WS_RECONNECT_ATTEMPTS", 10),
		WSReconnectInterval: getEnvAsDuration("WS_RECONNECT_INTERVAL", 5*time.Second),
		WSReconnectMaxDelay: getEnvAsDuration("WS_RECONNECT_MAX_DELAY", 30*time.Second),

		S3Bucket:      getEnv("S3_BUCKET", ""),
		S3Prefix:      getEnv("S3_PREFIX", "trades"),
		S3Region:      getEnv("AWS_REGION", "us-east-1"),
		FlushInterval: getEnvAsDuration("FLUSH_INTERVAL", 60*time.Second),
		FlushMaxRows:  getEnvAsInt("FLUSH_MAX_ROWS", 500),
		EnableS3:      getEnvAsBool("ENABLE_S3", true),

		PostgresDSN:            getEnv("POSTGRES_DSN", ""),
		HotRetentionDays:       getEnvAsInt("HOT_RETENTION_DAYS", 7),
		EnablePostgres:         getEnvAsBool("ENABLE_POSTGRES", true),
		PostgresWriteBatchSize: getEnvAsInt("POSTGRES_WRITE_BATCH_SIZE", 50),

		HealthStaleAfter: getEnvAsDuration("HEALTH_STALE_AFTER", 5*time.Minute),
	}

	return cfg, cfg.Validate()
}

// Validate checks required configuration.
func (c *Config) Validate() error {
	if c.ServiceName == "" {
		return fmt.Errorf("SERVICE_NAME is required")
	}
	if c.HTTPPort == "" {
		return fmt.Errorf("HTTP_PORT is required")
	}
	if c.BitsoWSURL == "" {
		return fmt.Errorf("BITSO_WS_URL is required")
	}
	if c.BitsoBook == "" {
		return fmt.Errorf("BITSO_BOOK is required")
	}
	if c.EnableS3 && c.S3Bucket == "" {
		return fmt.Errorf("S3_BUCKET is required when ENABLE_S3=true")
	}
	if c.EnablePostgres && c.PostgresDSN == "" {
		return fmt.Errorf("POSTGRES_DSN is required when ENABLE_POSTGRES=true")
	}
	if c.FlushMaxRows < 1 {
		return fmt.Errorf("FLUSH_MAX_ROWS must be >= 1")
	}
	if c.HotRetentionDays < 1 {
		return fmt.Errorf("HOT_RETENTION_DAYS must be >= 1")
	}
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}
	return strings.ToLower(valueStr) == "true" || valueStr == "1"
}

func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}
	duration, err := time.ParseDuration(valueStr)
	if err != nil {
		return defaultValue
	}
	return duration
}
