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
	}

	// Validate required configuration
	if config.BitsoAPIKey == "" || config.BitsoAPISecret == "" || config.StageBitsoAPIKey == "" || config.StageBitsoAPISecret == "" {
		return nil, fmt.Errorf("missing required Bitso API configuration")
	}

	if config.RedisHost == "" {
		config.RedisHost = "localhost"
	}
	if config.RedisPort == "" {
		config.RedisPort = "6379"
	}

	return config, nil
}
