package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the configuration for the order management service
type Config struct {
	// Service configuration
	Service ServiceConfig `json:"service"`

	// Kafka configuration
	Kafka KafkaConfig `json:"kafka"`

	// Storage: "memory" (default) or "redis". When "redis", Redis config is used for order/position persistence.
	Storage StorageConfig `json:"storage"`

	// Redis configuration (required when Storage.Type == "redis")
	Redis RedisConfig `json:"redis"`

	// Trading Engine configuration
	TradingEngine TradingEngineConfig `json:"trading_engine"`

	// Bitso API (optional: for sync job to poll order status)
	Bitso BitsoConfig `json:"bitso"`

	// Risk management configuration
	Risk RiskConfig `json:"risk"`

	// Logging configuration
	Logging LoggingConfig `json:"logging"`

	// Metrics configuration
	Metrics MetricsConfig `json:"metrics"`
}

// ServiceConfig holds service-specific configuration
type ServiceConfig struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Environment string `json:"environment"`
}

// KafkaConfig holds Kafka configuration
type KafkaConfig struct {
	Brokers []string `json:"brokers"`

	// Consumer configuration
	ConsumerGroup     string `json:"consumer_group"`
	TopicSignals      string `json:"topic_signals"`      // Input: strategy-executor.signals
	TopicOrdersPlaced string `json:"topic_orders_placed"` // Input: trading-engine publishes placed orders (trading.orders.placed)

	// Producer configuration
	TopicOrders string `json:"topic_orders"` // Output: order-management.orders
	TopicEvents string `json:"topic_events"`  // Output: order-management.events

	// Consumer settings
	AutoOffsetReset string        `json:"auto_offset_reset"`
	CommitInterval  time.Duration `json:"commit_interval"`
	MaxWait         time.Duration `json:"max_wait"`

	// Producer settings
	BatchSize        int           `json:"batch_size"`
	BatchTimeout     time.Duration `json:"batch_timeout"`
	CompressionCodec string        `json:"compression_codec"`
	RequiredAcks     int           `json:"required_acks"`
}

// StorageConfig holds storage backend selection (memory or redis).
type StorageConfig struct {
	Type string `json:"type"` // "memory" (default) or "redis"
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Password string `json:"password"`
	DB       int    `json:"db"`
	PoolSize int    `json:"pool_size"`
}

// TradingEngineConfig holds trading engine service configuration
type TradingEngineConfig struct {
	BaseURL    string        `json:"base_url"`
	Timeout    time.Duration `json:"timeout"`
	RetryCount int           `json:"retry_count"`
	RetryDelay time.Duration `json:"retry_delay"`
}

// BitsoConfig holds Bitso API config for the sync job (optional)
type BitsoConfig struct {
	APIBaseURL string `json:"api_base_url"`
	APIKey     string `json:"api_key"`
	APISecret  string `json:"api_secret"`
}

// RiskConfig holds risk management configuration
type RiskConfig struct {
	MaxOpenOrders        int     `json:"max_open_orders"`
	MaxOrderValue        float64 `json:"max_order_value"`
	MinOrderSize         float64 `json:"min_order_size"`
	MaxPositionSize      float64 `json:"max_position_size"`
	EnableDuplicateCheck bool    `json:"enable_duplicate_check"`
	MaxOrdersPerMinute   int     `json:"max_orders_per_minute"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string `json:"level"`
	Format string `json:"format"`
	Output string `json:"output"`
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
	Port    int    `json:"port"`
}

// Load loads configuration from environment variables with defaults
func Load() (*Config, error) {
	config := &Config{
		Service: ServiceConfig{
			Name:        getEnv("SERVICE_NAME", "order-management"),
			Version:     getEnv("SERVICE_VERSION", "1.0.0"),
			Host:        getEnv("SERVICE_HOST", "0.0.0.0"),
			Port:        getEnvAsInt("SERVICE_PORT", 8080),
			Environment: getEnv("ENVIRONMENT", "development"),
		},
		Kafka: KafkaConfig{
			Brokers:          getEnvAsSlice("KAFKA_BROKERS", []string{"localhost:9092"}),
			ConsumerGroup:    getEnv("KAFKA_CONSUMER_GROUP", "order-management-group"),
			TopicSignals:      getEnv("KAFKA_TOPIC_SIGNALS", "strategy-executor.signals"),
			TopicOrdersPlaced: getEnv("KAFKA_TOPIC_ORDERS_PLACED", "trading.orders.placed"),
			TopicOrders:       getEnv("KAFKA_TOPIC_ORDERS", "order-management.orders"),
			TopicEvents:       getEnv("KAFKA_TOPIC_EVENTS", "order-management.events"),
			AutoOffsetReset:  getEnv("KAFKA_AUTO_OFFSET_RESET", "latest"),
			CommitInterval:   getEnvAsDuration("KAFKA_COMMIT_INTERVAL", 1*time.Second),
			MaxWait:          getEnvAsDuration("KAFKA_MAX_WAIT", 500*time.Millisecond),
			BatchSize:        getEnvAsInt("KAFKA_BATCH_SIZE", 100),
			BatchTimeout:     getEnvAsDuration("KAFKA_BATCH_TIMEOUT", 1*time.Second),
			CompressionCodec: getEnv("KAFKA_COMPRESSION_CODEC", "snappy"),
			RequiredAcks:     getEnvAsInt("KAFKA_REQUIRED_ACKS", -1),
		},
		Storage: StorageConfig{
			Type: getEnv("STORAGE_TYPE", "memory"),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnvAsInt("REDIS_PORT", 6379),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvAsInt("REDIS_DB", 0),
			PoolSize: getEnvAsInt("REDIS_POOL_SIZE", 10),
		},
		TradingEngine: TradingEngineConfig{
			BaseURL:    getEnv("TRADING_ENGINE_BASE_URL", "http://localhost:8082"),
			Timeout:    getEnvAsDuration("TRADING_ENGINE_TIMEOUT", 30*time.Second),
			RetryCount: getEnvAsInt("TRADING_ENGINE_RETRY_COUNT", 3),
			RetryDelay: getEnvAsDuration("TRADING_ENGINE_RETRY_DELAY", 1*time.Second),
		},
		Bitso: BitsoConfig{
			APIBaseURL: getEnv("BITSO_API_BASE_URL", "https://stage.bitso.com/api"),
			APIKey:     bitsoAPIKeyFromEnv(),
			APISecret:  bitsoAPISecretFromEnv(),
		},
		Risk: RiskConfig{
			MaxOpenOrders:        getEnvAsInt("MAX_OPEN_ORDERS", 10),
			MaxOrderValue:        getEnvAsFloat("MAX_ORDER_VALUE", 100000.0),
			MinOrderSize:         getEnvAsFloat("MIN_ORDER_SIZE", 0.001),
			MaxPositionSize:      getEnvAsFloat("MAX_POSITION_SIZE", 1.0),
			EnableDuplicateCheck: getEnvAsBool("ENABLE_DUPLICATE_CHECK", true),
			MaxOrdersPerMinute:   getEnvAsInt("MAX_ORDERS_PER_MINUTE", 60),
		},
		Logging: LoggingConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
			Output: getEnv("LOG_OUTPUT", "stdout"),
		},
		Metrics: MetricsConfig{
			Enabled: getEnvAsBool("METRICS_ENABLED", true),
			Path:    getEnv("METRICS_PATH", "/metrics"),
			Port:    getEnvAsInt("METRICS_PORT", 9090),
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
	if c.Service.Name == "" {
		return fmt.Errorf("service name is required")
	}

	if c.Service.Port <= 0 || c.Service.Port > 65535 {
		return fmt.Errorf("invalid service port: %d", c.Service.Port)
	}

	if len(c.Kafka.Brokers) == 0 {
		return fmt.Errorf("at least one Kafka broker is required")
	}

	if c.Kafka.ConsumerGroup == "" {
		return fmt.Errorf("Kafka consumer group is required")
	}

	if c.Kafka.TopicSignals == "" {
		return fmt.Errorf("Kafka signals topic is required")
	}

	if c.Kafka.TopicOrders == "" {
		return fmt.Errorf("Kafka orders topic is required")
	}

	if c.Kafka.TopicEvents == "" {
		return fmt.Errorf("Kafka events topic is required")
	}

	// Storage type: memory (default) or redis
	if c.Storage.Type == "" {
		c.Storage.Type = "memory"
	}
	if c.Storage.Type != "memory" && c.Storage.Type != "redis" {
		return fmt.Errorf("invalid STORAGE_TYPE: %q (must be memory or redis)", c.Storage.Type)
	}
	// Redis config required only when using Redis storage
	if c.Storage.Type == "redis" {
		if c.Redis.Host == "" {
			return fmt.Errorf("Redis host is required when STORAGE_TYPE=redis")
		}
		if c.Redis.Port <= 0 || c.Redis.Port > 65535 {
			return fmt.Errorf("invalid Redis port: %d", c.Redis.Port)
		}
	}

	if c.TradingEngine.BaseURL == "" {
		return fmt.Errorf("trading engine base URL is required")
	}

	if c.Risk.MaxOpenOrders <= 0 {
		return fmt.Errorf("max open orders must be positive")
	}

	if c.Risk.MinOrderSize <= 0 {
		return fmt.Errorf("min order size must be positive")
	}

	if c.Risk.MaxOrderValue <= 0 {
		return fmt.Errorf("max order value must be positive")
	}

	return nil
}

// Helper functions for environment variable parsing

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvAsFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getEnvAsSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		var result []string
		current := ""
		for _, ch := range value {
			if ch == ',' {
				if trimmed := trimString(current); trimmed != "" {
					result = append(result, trimmed)
				}
				current = ""
			} else {
				current += string(ch)
			}
		}
		if trimmed := trimString(current); trimmed != "" {
			result = append(result, trimmed)
		}
		if len(result) > 0 {
			return result
		}
	}
	return defaultValue
}

// bitsoAPIKeyFromEnv prefers BITSO_API_KEY (Kubernetes trading-secrets), else STAGE_BITSO_API_KEY (legacy/local).
func bitsoAPIKeyFromEnv() string {
	if v := getEnv("BITSO_API_KEY", ""); v != "" {
		return v
	}
	return getEnv("STAGE_BITSO_API_KEY", "")
}

// bitsoAPISecretFromEnv prefers BITSO_API_SECRET, else STAGE_BITSO_APISECRET (legacy/local).
func bitsoAPISecretFromEnv() string {
	if v := getEnv("BITSO_API_SECRET", ""); v != "" {
		return v
	}
	return getEnv("STAGE_BITSO_APISECRET", "")
}

func trimString(s string) string {
	start := 0
	end := len(s)

	// Trim leading whitespace
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}

	// Trim trailing whitespace
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}
