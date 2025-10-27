package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Load loads configuration from environment variables
func Load() (*Config, error) {
	config := loadFromEnv()
	setDefaults(config)
	
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}
	
	return config, nil
}

// loadFromEnv loads configuration from environment variables
func loadFromEnv() *Config {
	return &Config{
		Service: ServiceConfig{
			Name:        getEnv("SERVICE_NAME", "backtesting"),
			Version:     getEnv("SERVICE_VERSION", "1.0.0"),
			Host:        getEnv("SERVICE_HOST", "0.0.0.0"),
			Port:        getEnvAsInt("SERVICE_PORT", 8084),
			Environment: getEnv("ENVIRONMENT", "development"),
		},
		MarketData: MarketDataConfig{
			BaseURL:    getEnv("MARKET_DATA_BASE_URL", "http://localhost:8083"),
			Timeout:    getEnvAsDuration("MARKET_DATA_TIMEOUT", 30*time.Second),
			RetryCount: getEnvAsInt("MARKET_DATA_RETRY_COUNT", 3),
			RetryDelay: getEnvAsDuration("MARKET_DATA_RETRY_DELAY", 1*time.Second),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnvAsInt("REDIS_PORT", 6379),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvAsInt("REDIS_DB", 0),
			PoolSize: getEnvAsInt("REDIS_POOL_SIZE", 10),
		},
		Storage: StorageConfig{
			Type:          getEnv("STORAGE_TYPE", "redis"),
			Path:          getEnv("STORAGE_PATH", "/var/lib/backtesting/results"),
			RetentionDays: getEnvAsInt("STORAGE_RETENTION_DAYS", 90),
		},
		Execution: ExecutionConfig{
			MaxConcurrentBacktests: getEnvAsInt("MAX_CONCURRENT_BACKTESTS", 5),
			DefaultSlippageModel:   getEnv("DEFAULT_SLIPPAGE_MODEL", "percentage"),
			DefaultSlippageValue:   getEnvAsFloat("DEFAULT_SLIPPAGE_VALUE", 0.001),
			DefaultCommissionRate:  getEnvAsFloat("DEFAULT_COMMISSION_RATE", 0.001),
			WorkerPoolSize:         getEnvAsInt("WORKER_POOL_SIZE", 10),
			EventBatchSize:         getEnvAsInt("EVENT_BATCH_SIZE", 1000),
			ProgressUpdateInterval: getEnvAsDuration("PROGRESS_UPDATE_INTERVAL", 5*time.Second),
		},
		Logging: LoggingConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
			Output: getEnv("LOG_OUTPUT", "stdout"),
		},
		Metrics: MetricsConfig{
			Enabled: getEnvAsBool("METRICS_ENABLED", true),
			Path:    getEnv("METRICS_PATH", "/metrics"),
			Port:    getEnvAsInt("METRICS_PORT", 9094),
		},
	}
}

// setDefaults sets default values for optional fields
func setDefaults(config *Config) {
	// Service defaults
	if config.Service.Port == 0 {
		config.Service.Port = 8084
	}
	if config.Service.Host == "" {
		config.Service.Host = "0.0.0.0"
	}
	
	// Execution defaults
	if config.Execution.MaxConcurrentBacktests == 0 {
		config.Execution.MaxConcurrentBacktests = 5
	}
	if config.Execution.WorkerPoolSize == 0 {
		config.Execution.WorkerPoolSize = 10
	}
	if config.Execution.EventBatchSize == 0 {
		config.Execution.EventBatchSize = 1000
	}
	
	// Storage defaults
	if config.Storage.RetentionDays == 0 {
		config.Storage.RetentionDays = 90
	}
}

// Helper functions

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt gets an environment variable as an integer with a default value
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

// getEnvAsFloat gets an environment variable as a float64 with a default value
func getEnvAsFloat(key string, defaultValue float64) float64 {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}
	
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return defaultValue
	}
	
	return value
}

// getEnvAsBool gets an environment variable as a boolean with a default value
func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}
	
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		// Try parsing as 1/0
		if valueStr == "1" {
			return true
		}
		if valueStr == "0" {
			return false
		}
		return defaultValue
	}
	
	return value
}

// getEnvAsDuration gets an environment variable as a duration with a default value
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

