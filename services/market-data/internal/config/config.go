package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the market-data service
type Config struct {
	// Service configuration
	ServiceName string
	ServicePort string

	// Bitso WebSocket configuration
	BitsoWSURL    string
	BitsoBooks    []string // Trading pairs to monitor
	BitsoChannels []string // Channels to subscribe (trades, diff-orders, orders)

	// Kafka configuration
	KafkaBrokers        string
	KafkaTopicTrades    string
	KafkaTopicOrderBook string
	KafkaTopicTicker    string

	// Redis configuration
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// Cache TTLs
	CacheTickerTTL    time.Duration
	CacheOrderBookTTL time.Duration
	CacheTradesSize   int

	// Publishing intervals
	PublishOrderBookInterval time.Duration
	PublishTickerInterval    time.Duration

	// Reconnection configuration
	WSReconnectAttempts int
	WSReconnectInterval time.Duration
	WSReconnectMaxDelay time.Duration

	// Feature flags
	EnableWebSocket bool
	EnableKafka     bool
	EnableCache     bool
	EnableHTTPAPI   bool
}

// LoadConfig loads configuration from environment variables
func LoadConfig() (*Config, error) {
	// Try to load .env file from multiple locations
	envPaths := []string{
		".env",
		"../.env",
		"../../.env",
		"../../../.env",
	}

	for _, path := range envPaths {
		if _, err := os.Stat(path); err == nil {
			_ = godotenv.Load(path)
			break
		}
	}

	config := &Config{
		// Service
		ServiceName: getEnv("SERVICE_NAME", "market-data"),
		ServicePort: getEnv("SERVICE_PORT", "8083"),

		// Bitso WebSocket
		BitsoWSURL:    getEnv("BITSO_WS_URL", "wss://ws.bitso.com"),
		BitsoBooks:    strings.Split(getEnv("BITSO_BOOKS", "btc_mxn"), ","),
		BitsoChannels: strings.Split(getEnv("BITSO_CHANNELS", "trades"), ","),

		// Kafka
		KafkaBrokers:        getEnv("KAFKA_BROKERS", "localhost:9092"),
		KafkaTopicTrades:    getEnv("KAFKA_TOPIC_TRADES", "market-data.trades"),
		KafkaTopicOrderBook: getEnv("KAFKA_TOPIC_ORDERBOOK", "market-data.orderbook"),
		KafkaTopicTicker:    getEnv("KAFKA_TOPIC_TICKER", "market-data.ticker"),

		// Redis
		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvAsInt("REDIS_DB", 0),

		// Cache configuration
		CacheTickerTTL:    getEnvAsDuration("CACHE_TICKER_TTL", 5*time.Second),
		CacheOrderBookTTL: getEnvAsDuration("CACHE_ORDERBOOK_TTL", 2*time.Second),
		CacheTradesSize:   getEnvAsInt("CACHE_TRADES_SIZE", 1000),

		// Publishing intervals
		PublishOrderBookInterval: getEnvAsDuration("PUBLISH_ORDERBOOK_INTERVAL", 1*time.Second),
		PublishTickerInterval:    getEnvAsDuration("PUBLISH_TICKER_INTERVAL", 1*time.Second),

		// Reconnection
		WSReconnectAttempts: getEnvAsInt("WS_RECONNECT_ATTEMPTS", 10),
		WSReconnectInterval: getEnvAsDuration("WS_RECONNECT_INTERVAL", 5*time.Second),
		WSReconnectMaxDelay: getEnvAsDuration("WS_RECONNECT_MAX_DELAY", 30*time.Second),

		// Feature flags
		EnableWebSocket: getEnvAsBool("ENABLE_WEBSOCKET", true),
		EnableKafka:     getEnvAsBool("ENABLE_KAFKA", true),
		EnableCache:     getEnvAsBool("ENABLE_CACHE", true),
		EnableHTTPAPI:   getEnvAsBool("ENABLE_HTTP_API", true),
	}

	return config, config.Validate()
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.ServiceName == "" {
		return fmt.Errorf("SERVICE_NAME is required")
	}

	if c.ServicePort == "" {
		return fmt.Errorf("SERVICE_PORT is required")
	}

	if c.BitsoWSURL == "" {
		return fmt.Errorf("BITSO_WS_URL is required")
	}

	if len(c.BitsoBooks) == 0 {
		return fmt.Errorf("at least one BITSO_BOOK is required")
	}

	if c.EnableKafka && c.KafkaBrokers == "" {
		return fmt.Errorf("KAFKA_BROKERS is required when Kafka is enabled")
	}

	return nil
}

// Helper functions

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

	var value int
	if _, err := fmt.Sscanf(valueStr, "%d", &value); err != nil {
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
