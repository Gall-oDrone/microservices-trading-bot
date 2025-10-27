package config

import (
	"fmt"
	"os"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

func TestLoad(t *testing.T) {
	// Save original environment
	originalEnv := make(map[string]string)
	envVars := []string{
		"SERVICE_NAME", "SERVICE_VERSION", "SERVICE_HOST", "SERVICE_PORT",
		"KAFKA_BROKERS", "KAFKA_CONSUMER_GROUP", "MARKET_DATA_BASE_URL",
		"DEFAULT_BOOK", "DEFAULT_STRATEGY",
	}

	for _, env := range envVars {
		originalEnv[env] = os.Getenv(env)
	}

	// Clean up after test
	defer func() {
		for _, env := range envVars {
			if val, exists := originalEnv[env]; exists {
				os.Setenv(env, val)
			} else {
				os.Unsetenv(env)
			}
		}
	}()

	tests := []struct {
		name     string
		envVars  map[string]string
		wantErr  bool
		validate func(*Config) error
	}{
		{
			name:    "default configuration",
			envVars: map[string]string{},
			wantErr: false,
			validate: func(c *Config) error {
				if c.Service.Name != "strategy-executor" {
					return fmt.Errorf("expected service name 'strategy-executor', got '%s'", c.Service.Name)
				}
				if c.Service.Port != 8080 {
					return fmt.Errorf("expected port 8080, got %d", c.Service.Port)
				}
				if len(c.Kafka.Brokers) != 1 || c.Kafka.Brokers[0] != "localhost:9092" {
					return fmt.Errorf("expected default Kafka broker, got %v", c.Kafka.Brokers)
				}
				return nil
			},
		},
		{
			name: "custom configuration",
			envVars: map[string]string{
				"SERVICE_NAME":         "test-strategy-executor",
				"SERVICE_PORT":         "9090",
				"KAFKA_BROKERS":        "kafka1:9092,kafka2:9092",
				"MARKET_DATA_BASE_URL": "http://market-data:8081",
				"DEFAULT_BOOK":         "eth_mxn",
			},
			wantErr: false,
			validate: func(c *Config) error {
				if c.Service.Name != "test-strategy-executor" {
					return fmt.Errorf("expected service name 'test-strategy-executor', got '%s'", c.Service.Name)
				}
				if c.Service.Port != 9090 {
					return fmt.Errorf("expected port 9090, got %d", c.Service.Port)
				}
				if len(c.Kafka.Brokers) != 2 {
					return fmt.Errorf("expected 2 Kafka brokers, got %d", len(c.Kafka.Brokers))
				}
				if c.MarketData.BaseURL != "http://market-data:8081" {
					return fmt.Errorf("expected market data URL 'http://market-data:8081', got '%s'", c.MarketData.BaseURL)
				}
				return nil
			},
		},
		{
			name: "invalid port",
			envVars: map[string]string{
				"SERVICE_PORT": "99999",
			},
			wantErr: true,
		},
		{
			name: "empty service name",
			envVars: map[string]string{
				"SERVICE_NAME": "",
			},
			wantErr: false, // This will be caught by validation, not by Load
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			for _, env := range envVars {
				os.Unsetenv(env)
			}

			// Set test environment variables
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			config, err := Load()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Load() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Load() unexpected error: %v", err)
				return
			}

			if config == nil {
				t.Errorf("Load() returned nil config")
				return
			}

			if tt.validate != nil {
				if err := tt.validate(config); err != nil {
					t.Errorf("Load() validation failed: %v", err)
				}
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid configuration",
			config: &Config{
				Service: ServiceConfig{
					Name: "test-service",
					Port: 8080,
				},
				Kafka: KafkaConfig{
					Brokers:       []string{"localhost:9092"},
					ConsumerGroup: "test-group",
				},
				MarketData: MarketDataConfig{
					BaseURL: "http://localhost:8081",
				},
				Strategy: StrategyConfig{
					DefaultBook: "btc_mxn",
				},
			},
			wantErr: false,
		},
		{
			name: "empty service name",
			config: &Config{
				Service: ServiceConfig{
					Name: "",
					Port: 8080,
				},
				Kafka: KafkaConfig{
					Brokers:       []string{"localhost:9092"},
					ConsumerGroup: "test-group",
				},
				MarketData: MarketDataConfig{
					BaseURL: "http://localhost:8081",
				},
				Strategy: StrategyConfig{
					DefaultBook: "btc_mxn",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			config: &Config{
				Service: ServiceConfig{
					Name: "test-service",
					Port: 0,
				},
				Kafka: KafkaConfig{
					Brokers:       []string{"localhost:9092"},
					ConsumerGroup: "test-group",
				},
				MarketData: MarketDataConfig{
					BaseURL: "http://localhost:8081",
				},
				Strategy: StrategyConfig{
					DefaultBook: "btc_mxn",
				},
			},
			wantErr: true,
		},
		{
			name: "no Kafka brokers",
			config: &Config{
				Service: ServiceConfig{
					Name: "test-service",
					Port: 8080,
				},
				Kafka: KafkaConfig{
					Brokers:       []string{},
					ConsumerGroup: "test-group",
				},
				MarketData: MarketDataConfig{
					BaseURL: "http://localhost:8081",
				},
				Strategy: StrategyConfig{
					DefaultBook: "btc_mxn",
				},
			},
			wantErr: true,
		},
		{
			name: "empty consumer group",
			config: &Config{
				Service: ServiceConfig{
					Name: "test-service",
					Port: 8080,
				},
				Kafka: KafkaConfig{
					Brokers:       []string{"localhost:9092"},
					ConsumerGroup: "",
				},
				MarketData: MarketDataConfig{
					BaseURL: "http://localhost:8081",
				},
				Strategy: StrategyConfig{
					DefaultBook: "btc_mxn",
				},
			},
			wantErr: true,
		},
		{
			name: "empty market data URL",
			config: &Config{
				Service: ServiceConfig{
					Name: "test-service",
					Port: 8080,
				},
				Kafka: KafkaConfig{
					Brokers:       []string{"localhost:9092"},
					ConsumerGroup: "test-group",
				},
				MarketData: MarketDataConfig{
					BaseURL: "",
				},
				Strategy: StrategyConfig{
					DefaultBook: "btc_mxn",
				},
			},
			wantErr: true,
		},
		{
			name: "empty default book",
			config: &Config{
				Service: ServiceConfig{
					Name: "test-service",
					Port: 8080,
				},
				Kafka: KafkaConfig{
					Brokers:       []string{"localhost:9092"},
					ConsumerGroup: "test-group",
				},
				MarketData: MarketDataConfig{
					BaseURL: "http://localhost:8081",
				},
				Strategy: StrategyConfig{
					DefaultBook: "",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_GetTradingConfig(t *testing.T) {
	config := &Config{
		Strategy: StrategyConfig{
			DefaultBook:     "btc_mxn",
			DefaultStrategy: "basic",
			Parameters: map[string]interface{}{
				"profit_target": 0.02,
				"stop_loss":     0.01,
			},
		},
		Risk: RiskConfig{
			MaxOpenPositions:  3,
			MaxTradeAmount:    0.1,
			MinTradeAmount:    0.001,
			MaxTradeValue:     10000.0,
			StopLossPercent:   2.0,
			TakeProfitPercent: 4.0,
			MaxTradingTime:    "24h",
		},
	}

	tradingConfig := config.GetTradingConfig()

	if tradingConfig == nil {
		t.Fatal("GetTradingConfig() returned nil")
	}

	if tradingConfig.Book == nil {
		t.Error("Expected book to be set")
	}

	if tradingConfig.MaxTradeAmount != 0.1 {
		t.Errorf("Expected MaxTradeAmount 0.1, got %f", tradingConfig.MaxTradeAmount)
	}

	if tradingConfig.MinTradeAmount != 0.001 {
		t.Errorf("Expected MinTradeAmount 0.001, got %f", tradingConfig.MinTradeAmount)
	}

	if tradingConfig.MaxOpenPositions != 3 {
		t.Errorf("Expected MaxOpenPositions 3, got %d", tradingConfig.MaxOpenPositions)
	}

	if tradingConfig.StrategyType != "basic" {
		t.Errorf("Expected StrategyType 'basic', got '%s'", tradingConfig.StrategyType)
	}

	if tradingConfig.MaxTradingTime != 24*time.Hour {
		t.Errorf("Expected MaxTradingTime 24h, got %v", tradingConfig.MaxTradingTime)
	}
}

func TestConfig_GetTradingConfig_InvalidBook(t *testing.T) {
	config := &Config{
		Strategy: StrategyConfig{
			DefaultBook:     "invalid_book",
			DefaultStrategy: "basic",
		},
		Risk: RiskConfig{
			MaxTradingTime: "24h",
		},
	}

	tradingConfig := config.GetTradingConfig()

	if tradingConfig == nil {
		t.Fatal("GetTradingConfig() returned nil")
	}

	// Should fallback to BTC/MXN
	if tradingConfig.Book == nil {
		t.Error("Expected book to be set (fallback to BTC/MXN)")
	}

	// Verify it's BTC/MXN
	expectedBook := bitso.NewBook(bitso.BTC, bitso.MXN)
	if tradingConfig.Book.String() != expectedBook.String() {
		t.Errorf("Expected fallback book BTC/MXN, got %s", tradingConfig.Book.String())
	}
}

func TestConfig_GetTradingConfig_InvalidDuration(t *testing.T) {
	config := &Config{
		Strategy: StrategyConfig{
			DefaultBook:     "btc_mxn",
			DefaultStrategy: "basic",
		},
		Risk: RiskConfig{
			MaxTradingTime: "invalid_duration",
		},
	}

	tradingConfig := config.GetTradingConfig()

	if tradingConfig == nil {
		t.Fatal("GetTradingConfig() returned nil")
	}

	// Should fallback to 24 hours
	if tradingConfig.MaxTradingTime != 24*time.Hour {
		t.Errorf("Expected fallback MaxTradingTime 24h, got %v", tradingConfig.MaxTradingTime)
	}
}

// Test helper functions
func TestGetEnvAsInt(t *testing.T) {
	tests := []struct {
		name         string
		envValue     string
		defaultValue int
		expected     int
	}{
		{"valid integer", "8080", 3000, 8080},
		{"invalid integer", "invalid", 3000, 3000},
		{"empty value", "", 3000, 3000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("TEST_INT", tt.envValue)
			defer os.Unsetenv("TEST_INT")

			result := getEnvAsInt("TEST_INT", tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnvAsInt() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestGetEnvAsFloat(t *testing.T) {
	tests := []struct {
		name         string
		envValue     string
		defaultValue float64
		expected     float64
	}{
		{"valid float", "1.5", 0.0, 1.5},
		{"invalid float", "invalid", 0.0, 0.0},
		{"empty value", "", 0.0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("TEST_FLOAT", tt.envValue)
			defer os.Unsetenv("TEST_FLOAT")

			result := getEnvAsFloat("TEST_FLOAT", tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnvAsFloat() = %f, want %f", result, tt.expected)
			}
		})
	}
}

func TestGetEnvAsBool(t *testing.T) {
	tests := []struct {
		name         string
		envValue     string
		defaultValue bool
		expected     bool
	}{
		{"true value", "true", false, true},
		{"false value", "false", true, false},
		{"invalid value", "invalid", true, true},
		{"empty value", "", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("TEST_BOOL", tt.envValue)
			defer os.Unsetenv("TEST_BOOL")

			result := getEnvAsBool("TEST_BOOL", tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnvAsBool() = %t, want %t", result, tt.expected)
			}
		})
	}
}

func TestGetEnvAsDuration(t *testing.T) {
	tests := []struct {
		name         string
		envValue     string
		defaultValue time.Duration
		expected     time.Duration
	}{
		{"valid duration", "5s", time.Second, 5 * time.Second},
		{"invalid duration", "invalid", time.Second, time.Second},
		{"empty value", "", time.Second, time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("TEST_DURATION", tt.envValue)
			defer os.Unsetenv("TEST_DURATION")

			result := getEnvAsDuration("TEST_DURATION", tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnvAsDuration() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetEnvAsSlice(t *testing.T) {
	tests := []struct {
		name         string
		envValue     string
		defaultValue []string
		expected     []string
	}{
		{"valid slice", "a,b,c", []string{"x"}, []string{"a", "b", "c"}},
		{"empty value", "", []string{"x"}, []string{"x"}},
		{"single value", "a", []string{"x"}, []string{"a"}},
		{"with spaces", " a , b , c ", []string{"x"}, []string{"a", "b", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("TEST_SLICE", tt.envValue)
			defer os.Unsetenv("TEST_SLICE")

			result := getEnvAsSlice("TEST_SLICE", tt.defaultValue)
			if len(result) != len(tt.expected) {
				t.Errorf("getEnvAsSlice() length = %d, want %d", len(result), len(tt.expected))
				return
			}

			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("getEnvAsSlice()[%d] = %s, want %s", i, v, tt.expected[i])
				}
			}
		})
	}
}
