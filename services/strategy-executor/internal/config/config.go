package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
)

// Config holds the configuration for the strategy executor service
type Config struct {
	// Service configuration
	Service ServiceConfig `json:"service"`

	// Kafka configuration
	Kafka KafkaConfig `json:"kafka"`

	// Market data service configuration
	MarketData MarketDataConfig `json:"market_data"`

	// Strategy configuration
	Strategy StrategyConfig `json:"strategy"`

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
	ConsumerGroup string `json:"consumer_group"`
	Topics        struct {
		MarketDataTrades    string `json:"market_data_trades"`
		MarketDataTickers   string `json:"market_data_tickers"`
		MarketDataOrderBook string `json:"market_data_orderbook"`
	} `json:"topics"`

	// Producer configuration
	ProducerTopics struct {
		Signals string `json:"signals"`
		Events  string `json:"events"`
	} `json:"producer_topics"`

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

// MarketDataConfig holds market data service configuration
type MarketDataConfig struct {
	BaseURL    string        `json:"base_url"`
	Timeout    time.Duration `json:"timeout"`
	RetryCount int           `json:"retry_count"`
	RetryDelay time.Duration `json:"retry_delay"`
}

// StrategyConfig holds strategy-specific configuration
type StrategyConfig struct {
	DefaultBook     string                 `json:"default_book"`
	DefaultStrategy string                 `json:"default_strategy"`
	Parameters      map[string]interface{} `json:"parameters"`
}

// RiskConfig holds risk management configuration
type RiskConfig struct {
	MaxOpenPositions  int     `json:"max_open_positions"`
	MaxTradeAmount    float64 `json:"max_trade_amount"`
	MinTradeAmount    float64 `json:"min_trade_amount"`
	MaxTradeValue     float64 `json:"max_trade_value"`
	StopLossPercent   float64 `json:"stop_loss_percent"`
	TakeProfitPercent float64 `json:"take_profit_percent"`
	MaxTradingTime    string  `json:"max_trading_time"`
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
			Name:        getEnv("SERVICE_NAME", "strategy-executor"),
			Version:     getEnv("SERVICE_VERSION", "1.0.0"),
			Host:        getEnv("SERVICE_HOST", "0.0.0.0"),
			Port:        getEnvAsInt("SERVICE_PORT", 8080),
			Environment: getEnv("ENVIRONMENT", "development"),
		},
		Kafka: KafkaConfig{
			Brokers:       getEnvAsSlice("KAFKA_BROKERS", []string{"localhost:9092"}),
			ConsumerGroup: getEnv("KAFKA_CONSUMER_GROUP", "strategy-executor-group"),
			Topics: struct {
				MarketDataTrades    string `json:"market_data_trades"`
				MarketDataTickers   string `json:"market_data_tickers"`
				MarketDataOrderBook string `json:"market_data_orderbook"`
			}{
				MarketDataTrades:    getEnv("KAFKA_TOPIC_MARKET_DATA_TRADES", "market-data.trades"),
				MarketDataTickers:   getEnv("KAFKA_TOPIC_MARKET_DATA_TICKERS", "market-data.tickers"),
				MarketDataOrderBook: getEnv("KAFKA_TOPIC_MARKET_DATA_ORDERBOOK", "market-data.orderbook"),
			},
			ProducerTopics: struct {
				Signals string `json:"signals"`
				Events  string `json:"events"`
			}{
				Signals: getEnv("KAFKA_TOPIC_SIGNALS", "strategy-executor.signals"),
				Events:  getEnv("KAFKA_TOPIC_EVENTS", "strategy-executor.events"),
			},
			AutoOffsetReset:  getEnv("KAFKA_AUTO_OFFSET_RESET", "latest"),
			CommitInterval:   getEnvAsDuration("KAFKA_COMMIT_INTERVAL", 1*time.Second),
			MaxWait:          getEnvAsDuration("KAFKA_MAX_WAIT", 500*time.Millisecond),
			BatchSize:        getEnvAsInt("KAFKA_BATCH_SIZE", 100),
			BatchTimeout:     getEnvAsDuration("KAFKA_BATCH_TIMEOUT", 1*time.Second),
			CompressionCodec: getEnv("KAFKA_COMPRESSION_CODEC", "snappy"),
			RequiredAcks:     getEnvAsInt("KAFKA_REQUIRED_ACKS", -1),
		},
		MarketData: MarketDataConfig{
			BaseURL:    getEnv("MARKET_DATA_BASE_URL", "http://localhost:8081"),
			Timeout:    getEnvAsDuration("MARKET_DATA_TIMEOUT", 30*time.Second),
			RetryCount: getEnvAsInt("MARKET_DATA_RETRY_COUNT", 3),
			RetryDelay: getEnvAsDuration("MARKET_DATA_RETRY_DELAY", 1*time.Second),
		},
		Strategy: StrategyConfig{
			DefaultBook:     getEnv("DEFAULT_BOOK", "btc_mxn"),
			DefaultStrategy: getEnv("DEFAULT_STRATEGY", "basic"),
			Parameters:      make(map[string]interface{}),
		},
		Risk: RiskConfig{
			MaxOpenPositions:  getEnvAsInt("MAX_OPEN_POSITIONS", 3),
			MaxTradeAmount:    getEnvAsFloat("MAX_TRADE_AMOUNT", 0.1),
			MinTradeAmount:    getEnvAsFloat("MIN_TRADE_AMOUNT", 0.001),
			MaxTradeValue:     getEnvAsFloat("MAX_TRADE_VALUE", 10000.0),
			StopLossPercent:   getEnvAsFloat("STOP_LOSS_PERCENT", 2.0),
			TakeProfitPercent: getEnvAsFloat("TAKE_PROFIT_PERCENT", 4.0),
			MaxTradingTime:    getEnv("MAX_TRADING_TIME", "24h"),
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

	if c.MarketData.BaseURL == "" {
		return fmt.Errorf("market data base URL is required")
	}

	if c.Strategy.DefaultBook == "" {
		return fmt.Errorf("default book is required")
	}

	return nil
}

// GetTradingConfig creates a TradingConfig from the current configuration
func (c *Config) GetTradingConfig() *models.TradingConfig {
	// Parse max trading time
	maxTradingTime, _ := time.ParseDuration(c.Risk.MaxTradingTime)
	if maxTradingTime == 0 {
		maxTradingTime = 24 * time.Hour
	}

	// Create book from string - simplified implementation
	// In a real implementation, you would parse the book string
	book := bitso.NewBook(bitso.BTC, bitso.MXN) // Default fallback

	return &models.TradingConfig{
		Book:              book,
		MaxTradeAmount:    c.Risk.MaxTradeAmount,
		MinTradeAmount:    c.Risk.MinTradeAmount,
		MaxTradeValue:     c.Risk.MaxTradeValue,
		MaxTradingTime:    maxTradingTime,
		StartTime:         time.Now(),
		EndTime:           time.Now().Add(maxTradingTime),
		MaxOpenPositions:  c.Risk.MaxOpenPositions,
		StopLossPercent:   c.Risk.StopLossPercent,
		TakeProfitPercent: c.Risk.TakeProfitPercent,
		StrategyType:      c.Strategy.DefaultStrategy,
		Parameters:        c.Strategy.Parameters,
	}
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
		// Simple comma-separated parsing
		var result []string
		for _, item := range splitString(value, ",") {
			if trimmed := trimString(item); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return defaultValue
}

// Simple string utilities (avoiding strings package dependency)
func splitString(s, sep string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
		}
	}
	result = append(result, s[start:])
	return result
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
