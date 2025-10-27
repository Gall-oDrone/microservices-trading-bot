package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the API Gateway service
type Config struct {
	// Service configuration
	Service ServiceConfig

	// Backend services configuration
	Backend BackendConfig

	// HTTP client configuration
	Client ClientConfig

	// Rate limiting configuration
	RateLimit RateLimitConfig

	// Circuit breaker configuration
	CircuitBreaker CircuitBreakerConfig

	// Authentication configuration
	Auth AuthConfig

	// Logging configuration
	Logging LoggingConfig

	// Metrics configuration
	Metrics MetricsConfig

	// TLS configuration
	TLS TLSConfig
}

// ServiceConfig holds service-level configuration
type ServiceConfig struct {
	Name        string
	Version     string
	Host        string
	Port        int
	Environment string
}

// BackendConfig holds backend service URLs
type BackendConfig struct {
	MarketDataURL       string
	OrderManagementURL  string
	StrategyExecutorURL string
}

// ClientConfig holds HTTP client configuration
type ClientConfig struct {
	Timeout            time.Duration
	MaxRetries         int
	RetryDelay         time.Duration
	MaxIdleConns       int
	IdleConnTimeout    time.Duration
	MaxConnsPerHost    int
	DisableKeepAlives  bool
	DisableCompression bool
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	Enabled            bool
	RequestsPerMinute  int
	Burst              int
	CleanupInterval    time.Duration
	InactivityDuration time.Duration
}

// CircuitBreakerConfig holds circuit breaker configuration
type CircuitBreakerConfig struct {
	Enabled     bool
	Threshold   int
	Timeout     time.Duration
	MaxRequests uint32
	Interval    time.Duration
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Enabled      bool
	JWTSecret    string
	APIKeyHeader string
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string
	Format string
	Output string
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	Enabled bool
	Port    int
	Path    string
}

// TLSConfig holds TLS configuration
type TLSConfig struct {
	Enabled  bool
	CertFile string
	KeyFile  string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	config := &Config{
		Service: ServiceConfig{
			Name:        getEnv("SERVICE_NAME", "api-gateway"),
			Version:     getEnv("SERVICE_VERSION", "1.0.0"),
			Host:        getEnv("SERVICE_HOST", "0.0.0.0"),
			Port:        getEnvAsInt("SERVICE_PORT", 8080),
			Environment: getEnv("ENVIRONMENT", "development"),
		},
		Backend: BackendConfig{
			MarketDataURL:       getEnv("MARKET_DATA_URL", "http://localhost:8083"),
			OrderManagementURL:  getEnv("ORDER_MANAGEMENT_URL", "http://localhost:8081"),
			StrategyExecutorURL: getEnv("STRATEGY_EXECUTOR_URL", "http://localhost:8082"),
		},
		Client: ClientConfig{
			Timeout:            getEnvAsDuration("CLIENT_TIMEOUT", 30*time.Second),
			MaxRetries:         getEnvAsInt("CLIENT_MAX_RETRIES", 3),
			RetryDelay:         getEnvAsDuration("CLIENT_RETRY_DELAY", 1*time.Second),
			MaxIdleConns:       getEnvAsInt("CLIENT_MAX_IDLE_CONNS", 100),
			IdleConnTimeout:    getEnvAsDuration("CLIENT_IDLE_CONN_TIMEOUT", 90*time.Second),
			MaxConnsPerHost:    getEnvAsInt("CLIENT_MAX_CONNS_PER_HOST", 10),
			DisableKeepAlives:  getEnvAsBool("CLIENT_DISABLE_KEEP_ALIVES", false),
			DisableCompression: getEnvAsBool("CLIENT_DISABLE_COMPRESSION", false),
		},
		RateLimit: RateLimitConfig{
			Enabled:            getEnvAsBool("RATE_LIMIT_ENABLED", true),
			RequestsPerMinute:  getEnvAsInt("RATE_LIMIT_REQUESTS_PER_MINUTE", 60),
			Burst:              getEnvAsInt("RATE_LIMIT_BURST", 10),
			CleanupInterval:    getEnvAsDuration("RATE_LIMIT_CLEANUP_INTERVAL", 5*time.Minute),
			InactivityDuration: getEnvAsDuration("RATE_LIMIT_INACTIVITY_DURATION", 10*time.Minute),
		},
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:     getEnvAsBool("CIRCUIT_BREAKER_ENABLED", true),
			Threshold:   getEnvAsInt("CIRCUIT_BREAKER_THRESHOLD", 5),
			Timeout:     getEnvAsDuration("CIRCUIT_BREAKER_TIMEOUT", 60*time.Second),
			MaxRequests: uint32(getEnvAsInt("CIRCUIT_BREAKER_MAX_REQUESTS", 10)),
			Interval:    getEnvAsDuration("CIRCUIT_BREAKER_INTERVAL", 10*time.Second),
		},
		Auth: AuthConfig{
			Enabled:      getEnvAsBool("AUTH_ENABLED", false),
			JWTSecret:    getEnv("JWT_SECRET", ""),
			APIKeyHeader: getEnv("API_KEY_HEADER", "X-API-Key"),
		},
		Logging: LoggingConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
			Output: getEnv("LOG_OUTPUT", "stdout"),
		},
		Metrics: MetricsConfig{
			Enabled: getEnvAsBool("METRICS_ENABLED", true),
			Port:    getEnvAsInt("METRICS_PORT", 9090),
			Path:    getEnv("METRICS_PATH", "/metrics"),
		},
		TLS: TLSConfig{
			Enabled:  getEnvAsBool("TLS_ENABLED", false),
			CertFile: getEnv("TLS_CERT_FILE", ""),
			KeyFile:  getEnv("TLS_KEY_FILE", ""),
		},
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate service configuration
	if c.Service.Name == "" {
		return fmt.Errorf("SERVICE_NAME is required")
	}
	if c.Service.Port <= 0 || c.Service.Port > 65535 {
		return fmt.Errorf("SERVICE_PORT must be between 1 and 65535, got %d", c.Service.Port)
	}

	// Validate backend service URLs
	if c.Backend.MarketDataURL == "" {
		return fmt.Errorf("MARKET_DATA_URL is required")
	}
	if c.Backend.OrderManagementURL == "" {
		return fmt.Errorf("ORDER_MANAGEMENT_URL is required")
	}
	if c.Backend.StrategyExecutorURL == "" {
		return fmt.Errorf("STRATEGY_EXECUTOR_URL is required")
	}

	// Validate client configuration
	if c.Client.Timeout <= 0 {
		return fmt.Errorf("CLIENT_TIMEOUT must be positive")
	}
	if c.Client.MaxRetries < 0 {
		return fmt.Errorf("CLIENT_MAX_RETRIES must be non-negative")
	}
	if c.Client.RetryDelay < 0 {
		return fmt.Errorf("CLIENT_RETRY_DELAY must be non-negative")
	}

	// Validate rate limit configuration
	if c.RateLimit.Enabled {
		if c.RateLimit.RequestsPerMinute <= 0 {
			return fmt.Errorf("RATE_LIMIT_REQUESTS_PER_MINUTE must be positive")
		}
		if c.RateLimit.Burst <= 0 {
			return fmt.Errorf("RATE_LIMIT_BURST must be positive")
		}
	}

	// Validate circuit breaker configuration
	if c.CircuitBreaker.Enabled {
		if c.CircuitBreaker.Threshold <= 0 {
			return fmt.Errorf("CIRCUIT_BREAKER_THRESHOLD must be positive")
		}
		if c.CircuitBreaker.Timeout <= 0 {
			return fmt.Errorf("CIRCUIT_BREAKER_TIMEOUT must be positive")
		}
	}

	// Validate logging configuration
	validLogLevels := map[string]bool{
		"trace": true, "debug": true, "info": true,
		"warn": true, "error": true, "fatal": true, "panic": true,
	}
	if !validLogLevels[strings.ToLower(c.Logging.Level)] {
		return fmt.Errorf("LOG_LEVEL must be one of: trace, debug, info, warn, error, fatal, panic")
	}

	validLogFormats := map[string]bool{"json": true, "console": true}
	if !validLogFormats[c.Logging.Format] {
		return fmt.Errorf("LOG_FORMAT must be one of: json, console")
	}

	// Validate TLS configuration
	if c.TLS.Enabled {
		if c.TLS.CertFile == "" {
			return fmt.Errorf("TLS_CERT_FILE is required when TLS is enabled")
		}
		if c.TLS.KeyFile == "" {
			return fmt.Errorf("TLS_KEY_FILE is required when TLS is enabled")
		}
	}

	// Validate metrics configuration
	if c.Metrics.Enabled {
		if c.Metrics.Port <= 0 || c.Metrics.Port > 65535 {
			return fmt.Errorf("METRICS_PORT must be between 1 and 65535")
		}
	}

	return nil
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
