package database

import (
	"fmt"
	"time"
)

// Config holds the Redis configuration
type Config struct {
	Host     string
	Port     int
	Password string
	DB       int
	PoolSize int
	Timeout  time.Duration
}

// NewConfig creates a new Redis configuration with default values
func NewConfig() *Config {
	return &Config{
		Host:     "localhost",
		Port:     6379,
		Password: "",
		DB:       0,
		PoolSize: 10,
		Timeout:  5 * time.Second,
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("host cannot be empty")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("invalid port number: %d", c.Port)
	}
	if c.PoolSize <= 0 {
		return fmt.Errorf("pool size must be greater than 0")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be greater than 0")
	}
	return nil
}

// GetDSN returns the Redis connection string
func (c *Config) GetDSN() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
