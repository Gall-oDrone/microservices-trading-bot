package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config represents the application configuration
type Config struct {
	// Bitso API configuration
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
	KafkaBrokers string
	KafkaGroupID string

	// Service configuration
	ServiceName string
	ServicePort string
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

	config := &Config{
		// Bitso API configuration
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
		KafkaBrokers: os.Getenv("KAFKA_BROKERS"),
		KafkaGroupID: os.Getenv("KAFKA_GROUP_ID"),

		// Service configuration
		ServiceName: os.Getenv("SERVICE_NAME"),
		ServicePort: os.Getenv("SERVICE_PORT"),
	}

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
