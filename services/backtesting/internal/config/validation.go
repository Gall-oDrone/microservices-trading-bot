package config

import (
	"fmt"
	"strings"
)

// Validate validates the entire configuration
func (c *Config) Validate() error {
	if err := validateServiceConfig(&c.Service); err != nil {
		return fmt.Errorf("service config: %w", err)
	}

	if err := validateMarketDataConfig(&c.MarketData); err != nil {
		return fmt.Errorf("market data config: %w", err)
	}

	if err := validateRedisConfig(&c.Redis); err != nil {
		return fmt.Errorf("redis config: %w", err)
	}

	if err := validateStorageConfig(&c.Storage); err != nil {
		return fmt.Errorf("storage config: %w", err)
	}

	if err := validateExecutionConfig(&c.Execution); err != nil {
		return fmt.Errorf("execution config: %w", err)
	}

	if err := validateLoggingConfig(&c.Logging); err != nil {
		return fmt.Errorf("logging config: %w", err)
	}

	if err := validateMetricsConfig(&c.Metrics); err != nil {
		return fmt.Errorf("metrics config: %w", err)
	}

	return nil
}

// validateServiceConfig validates service configuration
func validateServiceConfig(cfg *ServiceConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("SERVICE_NAME is required")
	}

	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("SERVICE_PORT must be between 1 and 65535, got %d", cfg.Port)
	}

	if cfg.Host == "" {
		return fmt.Errorf("SERVICE_HOST is required")
	}

	// Validate environment
	validEnvironments := map[string]bool{
		"development": true,
		"staging":     true,
		"production":  true,
		"test":        true,
	}
	if !validEnvironments[cfg.Environment] {
		return fmt.Errorf("invalid ENVIRONMENT: %s (must be: development, staging, production, test)", cfg.Environment)
	}

	return nil
}

// validateMarketDataConfig validates market data configuration
func validateMarketDataConfig(cfg *MarketDataConfig) error {
	if cfg.BaseURL == "" {
		return fmt.Errorf("MARKET_DATA_BASE_URL is required")
	}

	// Validate URL format
	if !strings.HasPrefix(cfg.BaseURL, "http://") && !strings.HasPrefix(cfg.BaseURL, "https://") {
		return fmt.Errorf("MARKET_DATA_BASE_URL must start with http:// or https://, got: %s", cfg.BaseURL)
	}

	if cfg.Timeout <= 0 {
		return fmt.Errorf("MARKET_DATA_TIMEOUT must be positive")
	}

	if cfg.RetryCount < 0 {
		return fmt.Errorf("MARKET_DATA_RETRY_COUNT must be non-negative")
	}

	if cfg.RetryCount > 10 {
		return fmt.Errorf("MARKET_DATA_RETRY_COUNT too high: %d (maximum: 10)", cfg.RetryCount)
	}

	if cfg.RetryDelay < 0 {
		return fmt.Errorf("MARKET_DATA_RETRY_DELAY must be non-negative")
	}

	return nil
}

// validateRedisConfig validates Redis configuration
func validateRedisConfig(cfg *RedisConfig) error {
	if cfg.Host == "" {
		return fmt.Errorf("REDIS_HOST is required")
	}

	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("REDIS_PORT must be between 1 and 65535, got %d", cfg.Port)
	}

	if cfg.DB < 0 || cfg.DB > 15 {
		return fmt.Errorf("REDIS_DB must be between 0 and 15, got %d", cfg.DB)
	}

	if cfg.PoolSize <= 0 {
		return fmt.Errorf("REDIS_POOL_SIZE must be positive, got %d", cfg.PoolSize)
	}

	if cfg.PoolSize > 100 {
		return fmt.Errorf("REDIS_POOL_SIZE too high: %d (maximum: 100)", cfg.PoolSize)
	}

	return nil
}

// validateStorageConfig validates storage configuration
func validateStorageConfig(cfg *StorageConfig) error {
	// Validate storage type
	validTypes := map[string]bool{
		"redis": true,
		"file":  true,
		"s3":    true,
	}
	if !validTypes[cfg.Type] {
		return fmt.Errorf("invalid STORAGE_TYPE: %s (must be: redis, file, s3)", cfg.Type)
	}

	// If file storage, path is required
	if cfg.Type == "file" && cfg.Path == "" {
		return fmt.Errorf("STORAGE_PATH is required when STORAGE_TYPE is 'file'")
	}

	if cfg.RetentionDays < 0 {
		return fmt.Errorf("STORAGE_RETENTION_DAYS must be non-negative, got %d", cfg.RetentionDays)
	}

	if cfg.RetentionDays > 3650 {
		return fmt.Errorf("STORAGE_RETENTION_DAYS too high: %d (maximum: 3650/10 years)", cfg.RetentionDays)
	}

	return nil
}

// validateExecutionConfig validates execution configuration
func validateExecutionConfig(cfg *ExecutionConfig) error {
	if cfg.MaxConcurrentBacktests <= 0 {
		return fmt.Errorf("MAX_CONCURRENT_BACKTESTS must be positive, got %d", cfg.MaxConcurrentBacktests)
	}

	if cfg.MaxConcurrentBacktests > 50 {
		return fmt.Errorf("MAX_CONCURRENT_BACKTESTS too high: %d (maximum: 50)", cfg.MaxConcurrentBacktests)
	}

	// Validate slippage model
	validModels := map[string]bool{
		"none":       true,
		"fixed":      true,
		"percentage": true,
		"volume":     true,
	}
	if !validModels[cfg.DefaultSlippageModel] {
		return fmt.Errorf("invalid DEFAULT_SLIPPAGE_MODEL: %s (must be: none, fixed, percentage, volume)", cfg.DefaultSlippageModel)
	}

	if cfg.DefaultSlippageValue < 0 {
		return fmt.Errorf("DEFAULT_SLIPPAGE_VALUE must be non-negative, got %f", cfg.DefaultSlippageValue)
	}

	if cfg.DefaultSlippageValue > 0.1 {
		return fmt.Errorf("DEFAULT_SLIPPAGE_VALUE too high: %f (maximum: 0.1/10%%)", cfg.DefaultSlippageValue)
	}

	if cfg.DefaultCommissionRate < 0 {
		return fmt.Errorf("DEFAULT_COMMISSION_RATE must be non-negative, got %f", cfg.DefaultCommissionRate)
	}

	if cfg.DefaultCommissionRate > 0.1 {
		return fmt.Errorf("DEFAULT_COMMISSION_RATE too high: %f (maximum: 0.1/10%%)", cfg.DefaultCommissionRate)
	}

	if cfg.WorkerPoolSize <= 0 {
		return fmt.Errorf("WORKER_POOL_SIZE must be positive, got %d", cfg.WorkerPoolSize)
	}

	if cfg.EventBatchSize <= 0 {
		return fmt.Errorf("EVENT_BATCH_SIZE must be positive, got %d", cfg.EventBatchSize)
	}

	if cfg.ProgressUpdateInterval <= 0 {
		return fmt.Errorf("PROGRESS_UPDATE_INTERVAL must be positive")
	}

	return nil
}

// validateLoggingConfig validates logging configuration
func validateLoggingConfig(cfg *LoggingConfig) error {
	// Validate log level
	validLevels := map[string]bool{
		"trace": true,
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
		"fatal": true,
		"panic": true,
	}
	if !validLevels[strings.ToLower(cfg.Level)] {
		return fmt.Errorf("invalid LOG_LEVEL: %s (must be: trace, debug, info, warn, error, fatal, panic)", cfg.Level)
	}

	// Validate log format
	validFormats := map[string]bool{
		"json":    true,
		"console": true,
	}
	if !validFormats[cfg.Format] {
		return fmt.Errorf("invalid LOG_FORMAT: %s (must be: json, console)", cfg.Format)
	}

	// Validate log output
	if cfg.Output == "" {
		return fmt.Errorf("LOG_OUTPUT is required")
	}

	return nil
}

// validateMetricsConfig validates metrics configuration
func validateMetricsConfig(cfg *MetricsConfig) error {
	if !cfg.Enabled {
		return nil // Skip validation if metrics are disabled
	}

	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("METRICS_PORT must be between 1 and 65535, got %d", cfg.Port)
	}

	if cfg.Path == "" {
		return fmt.Errorf("METRICS_PATH is required when metrics are enabled")
	}

	if !strings.HasPrefix(cfg.Path, "/") {
		return fmt.Errorf("METRICS_PATH must start with '/', got: %s", cfg.Path)
	}

	return nil
}
