package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config represents the application configuration
type Config struct {
	// Bitso API configuration
	BitsoAPIBaseURL     string // REST API base URL (default: stage for safety)
	BitsoAPIKey         string
	BitsoAPISecret      string
	StageBitsoAPIKey    string
	StageBitsoAPISecret string

	// Redis configuration
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// Kafka configuration (for microservices)
	KafkaBrokers          string
	KafkaGroupID          string
	KafkaTopicSignals     string
	KafkaTopicOrdersPlaced string // topic to publish placed orders (for order-management sync)

	// Service configuration
	ServiceName string
	ServicePort string

	// Optional: dry-run mode (no live broker API calls; log orders only). Env: DRY_RUN=true
	DryRun bool

	// Broker selects the exchange API (bitso|etoro). Env: BROKER
	Broker Broker

	// eToro API keys — ETORO_PUBLIC_KEY (x-api-key), ETORO_PRIVATE_KEY (x-user-key)
	EtoroPublicKey  string
	EtoroPrivateKey string
	EtoroEnv        string // demo|real — ETORO_ENV
}

// LoadConfig loads the configuration from environment variables
func LoadConfig() (*Config, error) {
	// Try to load .env file from multiple possible locations
	envPaths := []string{
		".env",          // Current directory
		"../.env",       // Parent directory
		"../../.env",    // Grandparent directory
		"../../../.env", // Great-grandparent directory
	}

	for _, envPath := range envPaths {
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			break
		}
	}

	defaultBitsoBaseURL := "https://stage.bitso.com/api"
	if v := os.Getenv("BITSO_API_BASE_URL"); v != "" {
		defaultBitsoBaseURL = v
	}
	config := &Config{
		// Bitso API configuration
		BitsoAPIBaseURL:     defaultBitsoBaseURL,
		BitsoAPIKey:         os.Getenv("BITSO_API_KEY"),
		BitsoAPISecret:      os.Getenv("BITSO_API_SECRET"),
		StageBitsoAPIKey:    os.Getenv("STAGE_BITSO_API_KEY"),
		StageBitsoAPISecret: os.Getenv("STAGE_BITSO_API_SECRET"),

		// Redis configuration
		RedisHost:     os.Getenv("REDIS_HOST"),
		RedisPort:     os.Getenv("REDIS_PORT"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       0, // Default to DB 0

		// Kafka configuration
		KafkaBrokers:          os.Getenv("KAFKA_BROKERS"),
		KafkaGroupID:          os.Getenv("KAFKA_GROUP_ID"),
		KafkaTopicSignals:     os.Getenv("KAFKA_TOPIC_SIGNALS"),
		KafkaTopicOrdersPlaced: os.Getenv("KAFKA_TOPIC_ORDERS_PLACED"),

		// Service configuration
		ServiceName: os.Getenv("SERVICE_NAME"),
		ServicePort: os.Getenv("SERVICE_PORT"),

		// Dry-run: if set, trading-engine logs orders but does not call broker APIs
		DryRun: os.Getenv("DRY_RUN") == "true" || os.Getenv("DRY_RUN") == "1",

		EtoroPublicKey:  os.Getenv("ETORO_PUBLIC_KEY"),
		EtoroPrivateKey: os.Getenv("ETORO_PRIVATE_KEY"),
		EtoroEnv:        os.Getenv("ETORO_ENV"),
	}
	broker, err := ParseBroker(os.Getenv("BROKER"))
	if err != nil {
		return nil, err
	}
	config.Broker = broker

	// Set defaults
	if config.RedisHost == "" {
		config.RedisHost = "localhost"
	}
	if config.RedisPort == "" {
		config.RedisPort = "6379"
	}
	if config.KafkaBrokers == "" {
		config.KafkaBrokers = "localhost:9092"
	}
	if config.KafkaTopicSignals == "" {
		config.KafkaTopicSignals = "trading.signals"
	}
	if config.KafkaTopicOrdersPlaced == "" {
		config.KafkaTopicOrdersPlaced = "trading.orders.placed"
	}
	if config.ServicePort == "" {
		config.ServicePort = "8080"
	}

	return config, nil
}

// Validate validates the required configuration fields
func (c *Config) Validate() error {
	if c.BitsoAPIKey == "" || c.BitsoAPISecret == "" {
		return fmt.Errorf("missing required Bitso API configuration")
	}
	if c.RedisHost == "" || c.RedisPort == "" {
		return fmt.Errorf("missing required Redis configuration")
	}
	return nil
}

// ValidateStage validates stage-specific Bitso API configuration
func (c *Config) ValidateStage() error {
	if c.StageBitsoAPIKey == "" || c.StageBitsoAPISecret == "" {
		return fmt.Errorf("missing required Stage Bitso API configuration")
	}
	return nil
}

// ValidateEtoro validates eToro API-key credentials when BROKER=etoro.
func (c *Config) ValidateEtoro() error {
	if c.EtoroPublicKey == "" || c.EtoroPrivateKey == "" {
		return fmt.Errorf("missing required eToro API configuration (ETORO_PUBLIC_KEY, ETORO_PRIVATE_KEY)")
	}
	if c.EtoroEnv != "" && c.EtoroEnv != "demo" && c.EtoroEnv != "real" {
		return fmt.Errorf("invalid ETORO_ENV %q: use demo or real", c.EtoroEnv)
	}
	return nil
}
