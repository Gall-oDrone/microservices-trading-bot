package config

import (
	"time"
)

// Config holds all configuration for the Backtesting Service
type Config struct {
	Service    ServiceConfig
	MarketData MarketDataConfig
	Redis      RedisConfig
	Storage    StorageConfig
	Execution  ExecutionConfig
	Logging    LoggingConfig
	Metrics    MetricsConfig
}

// ServiceConfig holds service-level configuration
type ServiceConfig struct {
	Name        string
	Version     string
	Host        string
	Port        int
	Environment string
}

// MarketDataConfig holds market-data service configuration
type MarketDataConfig struct {
	BaseURL    string
	Timeout    time.Duration
	RetryCount int
	RetryDelay time.Duration
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
	PoolSize int
}

// StorageConfig holds storage configuration
type StorageConfig struct {
	Type          string // "redis", "file", "s3"
	Path          string // Path for file-based storage
	RetentionDays int    // How long to retain results
}

// ExecutionConfig holds backtest execution configuration
type ExecutionConfig struct {
	MaxConcurrentBacktests int
	DefaultSlippageModel   string
	DefaultSlippageValue   float64
	DefaultCommissionRate  float64
	WorkerPoolSize         int
	EventBatchSize         int
	ProgressUpdateInterval time.Duration
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string // trace, debug, info, warn, error, fatal
	Format string // json, console
	Output string // stdout, stderr, file path
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	Enabled bool
	Path    string
	Port    int
}

// GetAddress returns the service address
func (c *ServiceConfig) GetAddress() string {
	return c.Host + ":" + string(rune(c.Port))
}

// GetRedisAddress returns the Redis address
func (c *RedisConfig) GetAddress() string {
	return c.Host + ":" + string(rune(c.Port))
}
