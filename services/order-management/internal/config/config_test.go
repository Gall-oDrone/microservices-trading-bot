package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// Test loading with default values
	config, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if config == nil {
		t.Fatal("Load() returned nil config")
	}

	// Verify default values
	if config.Service.Name != "order-management" {
		t.Errorf("Expected service name 'order-management', got '%s'", config.Service.Name)
	}

	if config.Service.Port != 8080 {
		t.Errorf("Expected service port 8080, got %d", config.Service.Port)
	}

	if config.Service.Environment != "development" {
		t.Errorf("Expected environment 'development', got '%s'", config.Service.Environment)
	}
}

func TestLoadWithEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("SERVICE_NAME", "test-service")
	os.Setenv("SERVICE_PORT", "9090")
	os.Setenv("ENVIRONMENT", "testing")
	defer func() {
		os.Unsetenv("SERVICE_NAME")
		os.Unsetenv("SERVICE_PORT")
		os.Unsetenv("ENVIRONMENT")
	}()

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if config.Service.Name != "test-service" {
		t.Errorf("Expected service name 'test-service', got '%s'", config.Service.Name)
	}

	if config.Service.Port != 9090 {
		t.Errorf("Expected service port 9090, got %d", config.Service.Port)
	}

	if config.Service.Environment != "testing" {
		t.Errorf("Expected environment 'testing', got '%s'", config.Service.Environment)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Service: ServiceConfig{
					Name: "order-management",
					Port: 8080,
				},
				Kafka: KafkaConfig{
					Brokers:       []string{"localhost:9092"},
					ConsumerGroup: "test-group",
					TopicSignals:  "signals",
					TopicOrders:   "orders",
					TopicEvents:   "events",
				},
				Redis: RedisConfig{
					Host: "localhost",
					Port: 6379,
				},
				TradingEngine: TradingEngineConfig{
					BaseURL: "http://localhost:8082",
				},
				Risk: RiskConfig{
					MaxOpenOrders:   10,
					MaxOrderValue:   100000.0,
					MinOrderSize:    0.001,
					MaxPositionSize: 1.0,
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
					TopicSignals:  "signals",
					TopicOrders:   "orders",
					TopicEvents:   "events",
				},
				Redis: RedisConfig{
					Host: "localhost",
					Port: 6379,
				},
				TradingEngine: TradingEngineConfig{
					BaseURL: "http://localhost:8082",
				},
				Risk: RiskConfig{
					MaxOpenOrders:   10,
					MaxOrderValue:   100000.0,
					MinOrderSize:    0.001,
					MaxPositionSize: 1.0,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			config: &Config{
				Service: ServiceConfig{
					Name: "order-management",
					Port: 0,
				},
				Kafka: KafkaConfig{
					Brokers:       []string{"localhost:9092"},
					ConsumerGroup: "test-group",
					TopicSignals:  "signals",
					TopicOrders:   "orders",
					TopicEvents:   "events",
				},
				Redis: RedisConfig{
					Host: "localhost",
					Port: 6379,
				},
				TradingEngine: TradingEngineConfig{
					BaseURL: "http://localhost:8082",
				},
				Risk: RiskConfig{
					MaxOpenOrders:   10,
					MaxOrderValue:   100000.0,
					MinOrderSize:    0.001,
					MaxPositionSize: 1.0,
				},
			},
			wantErr: true,
		},
		{
			name: "no kafka brokers",
			config: &Config{
				Service: ServiceConfig{
					Name: "order-management",
					Port: 8080,
				},
				Kafka: KafkaConfig{
					Brokers:       []string{},
					ConsumerGroup: "test-group",
					TopicSignals:  "signals",
					TopicOrders:   "orders",
					TopicEvents:   "events",
				},
				Storage: StorageConfig{Type: "memory"},
				Redis: RedisConfig{
					Host: "localhost",
					Port: 6379,
				},
				TradingEngine: TradingEngineConfig{
					BaseURL: "http://localhost:8082",
				},
				Risk: RiskConfig{
					MaxOpenOrders:   10,
					MaxOrderValue:   100000.0,
					MinOrderSize:    0.001,
					MaxPositionSize: 1.0,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid STORAGE_TYPE",
			config: &Config{
				Service: ServiceConfig{
					Name: "order-management",
					Port: 8080,
				},
				Kafka: KafkaConfig{
					Brokers:       []string{"localhost:9092"},
					ConsumerGroup: "test-group",
					TopicSignals:  "signals",
					TopicOrders:   "orders",
					TopicEvents:   "events",
				},
				Storage: StorageConfig{Type: "postgres"},
				Redis:   RedisConfig{Host: "localhost", Port: 6379},
				TradingEngine: TradingEngineConfig{BaseURL: "http://localhost:8082"},
				Risk: RiskConfig{
					MaxOpenOrders: 10, MaxOrderValue: 100000.0, MinOrderSize: 0.001, MaxPositionSize: 1.0,
				},
			},
			wantErr: true,
		},
		{
			name: "redis storage requires Redis host",
			config: &Config{
				Service: ServiceConfig{
					Name: "order-management",
					Port: 8080,
				},
				Kafka: KafkaConfig{
					Brokers:       []string{"localhost:9092"},
					ConsumerGroup: "test-group",
					TopicSignals:  "signals",
					TopicOrders:   "orders",
					TopicEvents:   "events",
				},
				Storage: StorageConfig{Type: "redis"},
				Redis:   RedisConfig{Host: "", Port: 6379},
				TradingEngine: TradingEngineConfig{BaseURL: "http://localhost:8082"},
				Risk: RiskConfig{
					MaxOpenOrders: 10, MaxOrderValue: 100000.0, MinOrderSize: 0.001, MaxPositionSize: 1.0,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetEnvAsInt(t *testing.T) {
	os.Setenv("TEST_INT", "123")
	defer os.Unsetenv("TEST_INT")

	result := getEnvAsInt("TEST_INT", 456)
	if result != 123 {
		t.Errorf("Expected 123, got %d", result)
	}

	result = getEnvAsInt("MISSING_INT", 456)
	if result != 456 {
		t.Errorf("Expected default 456, got %d", result)
	}
}

func TestGetEnvAsFloat(t *testing.T) {
	os.Setenv("TEST_FLOAT", "123.45")
	defer os.Unsetenv("TEST_FLOAT")

	result := getEnvAsFloat("TEST_FLOAT", 678.90)
	if result != 123.45 {
		t.Errorf("Expected 123.45, got %f", result)
	}

	result = getEnvAsFloat("MISSING_FLOAT", 678.90)
	if result != 678.90 {
		t.Errorf("Expected default 678.90, got %f", result)
	}
}

func TestGetEnvAsBool(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
	}{
		{"true", true},
		{"false", false},
		{"1", true},
		{"0", false},
		{"TRUE", true},
		{"FALSE", false},
	}

	for _, tt := range tests {
		os.Setenv("TEST_BOOL", tt.value)
		result := getEnvAsBool("TEST_BOOL", false)
		if result != tt.expected {
			t.Errorf("For value '%s', expected %v, got %v", tt.value, tt.expected, result)
		}
		os.Unsetenv("TEST_BOOL")
	}

	result := getEnvAsBool("MISSING_BOOL", true)
	if result != true {
		t.Errorf("Expected default true, got %v", result)
	}
}

func TestGetEnvAsDuration(t *testing.T) {
	os.Setenv("TEST_DURATION", "5s")
	defer os.Unsetenv("TEST_DURATION")

	result := getEnvAsDuration("TEST_DURATION", 10*time.Second)
	if result != 5*time.Second {
		t.Errorf("Expected 5s, got %v", result)
	}

	result = getEnvAsDuration("MISSING_DURATION", 10*time.Second)
	if result != 10*time.Second {
		t.Errorf("Expected default 10s, got %v", result)
	}
}

func TestGetEnvAsSlice(t *testing.T) {
	os.Setenv("TEST_SLICE", "item1,item2,item3")
	defer os.Unsetenv("TEST_SLICE")

	result := getEnvAsSlice("TEST_SLICE", []string{"default"})
	if len(result) != 3 {
		t.Errorf("Expected 3 items, got %d", len(result))
	}
	if result[0] != "item1" || result[1] != "item2" || result[2] != "item3" {
		t.Errorf("Expected [item1, item2, item3], got %v", result)
	}

	result = getEnvAsSlice("MISSING_SLICE", []string{"default"})
	if len(result) != 1 || result[0] != "default" {
		t.Errorf("Expected default slice, got %v", result)
	}
}

func TestTrimString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  test  ", "test"},
		{"\ttest\t", "test"},
		{"\ntest\n", "test"},
		{"test", "test"},
		{"  ", ""},
		{"", ""},
	}

	for _, tt := range tests {
		result := trimString(tt.input)
		if result != tt.expected {
			t.Errorf("For input '%s', expected '%s', got '%s'", tt.input, tt.expected, result)
		}
	}
}
