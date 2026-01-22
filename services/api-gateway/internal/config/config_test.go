package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		wantErr bool
	}{
		{
			name:    "default configuration",
			envVars: map[string]string{},
			wantErr: false,
		},
		{
			name: "custom configuration",
			envVars: map[string]string{
				"SERVICE_NAME":            "test-gateway",
				"SERVICE_PORT":            "9090",
				"MARKET_DATA_URL":         "http://market-data:8083",
				"ORDER_MANAGEMENT_URL":    "http://order-mgmt:8081",
				"STRATEGY_EXECUTOR_URL":   "http://strategy:8082",
				"CLIENT_TIMEOUT":          "45s",
				"CLIENT_MAX_RETRIES":      "5",
				"RATE_LIMIT_ENABLED":      "true",
				"CIRCUIT_BREAKER_ENABLED": "true",
				"LOG_LEVEL":               "debug",
				"LOG_FORMAT":              "json",
			},
			wantErr: false,
		},
		{
			name: "invalid port",
			envVars: map[string]string{
				"SERVICE_PORT": "99999",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original environment
			originalEnv := make(map[string]string)
			for key := range tt.envVars {
				originalEnv[key] = os.Getenv(key)
			}

			// Set test environment
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			// Cleanup
			defer func() {
				for key, value := range originalEnv {
					if value == "" {
						os.Unsetenv(key)
					} else {
						os.Setenv(key, value)
					}
				}
			}()

			// Test
			config, err := Load()
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && config == nil {
				t.Error("Load() returned nil config without error")
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid configuration",
			config: &Config{
				Service: ServiceConfig{
					Name: "api-gateway",
					Port: 8080,
				},
				Backend: BackendConfig{
					MarketDataURL:       "http://localhost:8083",
					OrderManagementURL:  "http://localhost:8081",
					StrategyExecutorURL: "http://localhost:8082",
				},
				Client: ClientConfig{
					Timeout:    30 * time.Second,
					MaxRetries: 3,
					RetryDelay: 1 * time.Second,
				},
				RateLimit: RateLimitConfig{
					Enabled:           true,
					RequestsPerMinute: 60,
					Burst:             10,
				},
				CircuitBreaker: CircuitBreakerConfig{
					Enabled:   true,
					Threshold: 5,
					Timeout:   60 * time.Second,
				},
				Logging: LoggingConfig{
					Level:  "info",
					Format: "json",
				},
				Metrics: MetricsConfig{
					Enabled: true,
					Port:    9090,
				},
			},
			wantErr: false,
		},
		{
			name: "missing service name",
			config: &Config{
				Service: ServiceConfig{
					Name: "",
					Port: 8080,
				},
				Backend: BackendConfig{
					MarketDataURL:       "http://localhost:8083",
					OrderManagementURL:  "http://localhost:8081",
					StrategyExecutorURL: "http://localhost:8082",
				},
				Client: ClientConfig{
					Timeout: 30 * time.Second,
				},
				Logging: LoggingConfig{
					Level:  "info",
					Format: "json",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			config: &Config{
				Service: ServiceConfig{
					Name: "api-gateway",
					Port: -1,
				},
				Backend: BackendConfig{
					MarketDataURL:       "http://localhost:8083",
					OrderManagementURL:  "http://localhost:8081",
					StrategyExecutorURL: "http://localhost:8082",
				},
				Client: ClientConfig{
					Timeout: 30 * time.Second,
				},
				Logging: LoggingConfig{
					Level:  "info",
					Format: "json",
				},
			},
			wantErr: true,
		},
		{
			name: "missing backend URL",
			config: &Config{
				Service: ServiceConfig{
					Name: "api-gateway",
					Port: 8080,
				},
				Backend: BackendConfig{
					MarketDataURL:      "http://localhost:8083",
					OrderManagementURL: "",
				},
				Client: ClientConfig{
					Timeout: 30 * time.Second,
				},
				Logging: LoggingConfig{
					Level:  "info",
					Format: "json",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid log level",
			config: &Config{
				Service: ServiceConfig{
					Name: "api-gateway",
					Port: 8080,
				},
				Backend: BackendConfig{
					MarketDataURL:       "http://localhost:8083",
					OrderManagementURL:  "http://localhost:8081",
					StrategyExecutorURL: "http://localhost:8082",
				},
				Client: ClientConfig{
					Timeout: 30 * time.Second,
				},
				Logging: LoggingConfig{
					Level:  "invalid",
					Format: "json",
				},
			},
			wantErr: true,
		},
		{
			name: "TLS enabled without cert",
			config: &Config{
				Service: ServiceConfig{
					Name: "api-gateway",
					Port: 8080,
				},
				Backend: BackendConfig{
					MarketDataURL:       "http://localhost:8083",
					OrderManagementURL:  "http://localhost:8081",
					StrategyExecutorURL: "http://localhost:8082",
				},
				Client: ClientConfig{
					Timeout: 30 * time.Second,
				},
				Logging: LoggingConfig{
					Level:  "info",
					Format: "json",
				},
				TLS: TLSConfig{
					Enabled:  true,
					CertFile: "",
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

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		want         string
	}{
		{
			name:         "with env value",
			key:          "TEST_KEY",
			defaultValue: "default",
			envValue:     "custom",
			want:         "custom",
		},
		{
			name:         "without env value",
			key:          "TEST_KEY",
			defaultValue: "default",
			envValue:     "",
			want:         "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			got := getEnv(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnvAsInt(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue int
		envValue     string
		want         int
	}{
		{
			name:         "valid integer",
			key:          "TEST_INT",
			defaultValue: 10,
			envValue:     "20",
			want:         20,
		},
		{
			name:         "invalid integer",
			key:          "TEST_INT",
			defaultValue: 10,
			envValue:     "invalid",
			want:         10,
		},
		{
			name:         "empty value",
			key:          "TEST_INT",
			defaultValue: 10,
			envValue:     "",
			want:         10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
			} else {
				os.Unsetenv(tt.key)
			}
			defer os.Unsetenv(tt.key)

			got := getEnvAsInt(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvAsInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnvAsBool(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue bool
		envValue     string
		want         bool
	}{
		{
			name:         "true string",
			key:          "TEST_BOOL",
			defaultValue: false,
			envValue:     "true",
			want:         true,
		},
		{
			name:         "false string",
			key:          "TEST_BOOL",
			defaultValue: true,
			envValue:     "false",
			want:         false,
		},
		{
			name:         "1 value",
			key:          "TEST_BOOL",
			defaultValue: false,
			envValue:     "1",
			want:         true,
		},
		{
			name:         "0 value",
			key:          "TEST_BOOL",
			defaultValue: true,
			envValue:     "0",
			want:         false,
		},
		{
			name:         "invalid value",
			key:          "TEST_BOOL",
			defaultValue: true,
			envValue:     "invalid",
			want:         true,
		},
		{
			name:         "empty value",
			key:          "TEST_BOOL",
			defaultValue: true,
			envValue:     "",
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
			} else {
				os.Unsetenv(tt.key)
			}
			defer os.Unsetenv(tt.key)

			got := getEnvAsBool(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvAsBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnvAsDuration(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue time.Duration
		envValue     string
		want         time.Duration
	}{
		{
			name:         "valid duration",
			key:          "TEST_DURATION",
			defaultValue: 10 * time.Second,
			envValue:     "30s",
			want:         30 * time.Second,
		},
		{
			name:         "invalid duration",
			key:          "TEST_DURATION",
			defaultValue: 10 * time.Second,
			envValue:     "invalid",
			want:         10 * time.Second,
		},
		{
			name:         "empty value",
			key:          "TEST_DURATION",
			defaultValue: 10 * time.Second,
			envValue:     "",
			want:         10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
			} else {
				os.Unsetenv(tt.key)
			}
			defer os.Unsetenv(tt.key)

			got := getEnvAsDuration(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvAsDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}
