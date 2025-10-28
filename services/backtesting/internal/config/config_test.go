package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// Save original env vars
	originalVars := map[string]string{
		"SERVICE_NAME": os.Getenv("SERVICE_NAME"),
		"SERVICE_PORT": os.Getenv("SERVICE_PORT"),
	}

	// Restore after test
	defer func() {
		for key, value := range originalVars {
			if value != "" {
				os.Setenv(key, value)
			} else {
				os.Unsetenv(key)
			}
		}
	}()

	// Set test env vars
	os.Setenv("SERVICE_NAME", "test-service")
	os.Setenv("SERVICE_PORT", "9999")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if config.Service.Name != "test-service" {
		t.Errorf("Expected service name 'test-service', got '%s'", config.Service.Name)
	}

	if config.Service.Port != 9999 {
		t.Errorf("Expected port 9999, got %d", config.Service.Port)
	}
}

func TestLoadDefaults(t *testing.T) {
	// Clear env vars
	os.Clearenv()

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Check defaults
	if config.Service.Name != "backtesting" {
		t.Errorf("Expected default service name 'backtesting', got '%s'", config.Service.Name)
	}

	if config.Service.Port != 8084 {
		t.Errorf("Expected default port 8084, got %d", config.Service.Port)
	}

	if config.Execution.MaxConcurrentBacktests != 5 {
		t.Errorf("Expected default max concurrent 5, got %d", config.Execution.MaxConcurrentBacktests)
	}
}

func TestValidateServiceConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  ServiceConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: ServiceConfig{
				Name:        "test",
				Port:        8080,
				Host:        "0.0.0.0",
				Environment: "development",
			},
			wantErr: false,
		},
		{
			name: "empty name",
			config: ServiceConfig{
				Name:        "",
				Port:        8080,
				Host:        "0.0.0.0",
				Environment: "development",
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			config: ServiceConfig{
				Name:        "test",
				Port:        99999,
				Host:        "0.0.0.0",
				Environment: "development",
			},
			wantErr: true,
		},
		{
			name: "invalid environment",
			config: ServiceConfig{
				Name:        "test",
				Port:        8080,
				Host:        "0.0.0.0",
				Environment: "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateServiceConfig(&tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateServiceConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateMarketDataConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  MarketDataConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: MarketDataConfig{
				BaseURL:    "http://localhost:8083",
				Timeout:    30 * time.Second,
				RetryCount: 3,
				RetryDelay: 1 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "empty base URL",
			config: MarketDataConfig{
				BaseURL:    "",
				Timeout:    30 * time.Second,
				RetryCount: 3,
			},
			wantErr: true,
		},
		{
			name: "invalid URL format",
			config: MarketDataConfig{
				BaseURL:    "localhost:8083",
				Timeout:    30 * time.Second,
				RetryCount: 3,
			},
			wantErr: true,
		},
		{
			name: "too many retries",
			config: MarketDataConfig{
				BaseURL:    "http://localhost:8083",
				Timeout:    30 * time.Second,
				RetryCount: 20,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMarketDataConfig(&tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateMarketDataConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetEnvHelpers(t *testing.T) {
	os.Clearenv()

	// Test getEnv
	os.Setenv("TEST_STRING", "value")
	if got := getEnv("TEST_STRING", "default"); got != "value" {
		t.Errorf("getEnv() = %v, want %v", got, "value")
	}
	if got := getEnv("NONEXISTENT", "default"); got != "default" {
		t.Errorf("getEnv() = %v, want %v", got, "default")
	}

	// Test getEnvAsInt
	os.Setenv("TEST_INT", "123")
	if got := getEnvAsInt("TEST_INT", 0); got != 123 {
		t.Errorf("getEnvAsInt() = %v, want %v", got, 123)
	}
	if got := getEnvAsInt("NONEXISTENT", 456); got != 456 {
		t.Errorf("getEnvAsInt() = %v, want %v", got, 456)
	}

	// Test getEnvAsBool
	os.Setenv("TEST_BOOL", "true")
	if got := getEnvAsBool("TEST_BOOL", false); got != true {
		t.Errorf("getEnvAsBool() = %v, want %v", got, true)
	}
	os.Setenv("TEST_BOOL", "1")
	if got := getEnvAsBool("TEST_BOOL", false); got != true {
		t.Errorf("getEnvAsBool() = %v, want %v", got, true)
	}

	// Test getEnvAsDuration
	os.Setenv("TEST_DURATION", "10s")
	if got := getEnvAsDuration("TEST_DURATION", 0); got != 10*time.Second {
		t.Errorf("getEnvAsDuration() = %v, want %v", got, 10*time.Second)
	}
}

func TestValidateStorageConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  StorageConfig
		wantErr bool
	}{
		{
			name: "valid redis storage",
			config: StorageConfig{
				Type:          "redis",
				RetentionDays: 90,
			},
			wantErr: false,
		},
		{
			name: "valid file storage",
			config: StorageConfig{
				Type:          "file",
				Path:          "/var/lib/backtesting",
				RetentionDays: 90,
			},
			wantErr: false,
		},
		{
			name: "invalid storage type",
			config: StorageConfig{
				Type:          "invalid",
				RetentionDays: 90,
			},
			wantErr: true,
		},
		{
			name: "file storage without path",
			config: StorageConfig{
				Type:          "file",
				Path:          "",
				RetentionDays: 90,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStorageConfig(&tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateStorageConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
