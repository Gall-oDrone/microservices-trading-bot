package config

import (
	"os"
	"testing"
	"time"
)

// TestLoadConfig tests configuration loading
func TestLoadConfig(t *testing.T) {
	// Save original environment variables
	originalEnv := make(map[string]string)
	envVars := []string{
		"SERVICE_NAME", "SERVICE_PORT", "BITSO_WS_URL", "BITSO_BOOKS", "BITSO_CHANNELS",
		"KAFKA_BROKERS", "KAFKA_TOPIC_TRADES", "KAFKA_TOPIC_ORDERBOOK", "KAFKA_TOPIC_TICKER",
		"REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "REDIS_DB",
		"CACHE_TICKER_TTL", "CACHE_ORDERBOOK_TTL", "CACHE_TRADES_SIZE",
		"PUBLISH_ORDERBOOK_INTERVAL", "PUBLISH_TICKER_INTERVAL",
		"WS_RECONNECT_ATTEMPTS", "WS_RECONNECT_INTERVAL", "WS_RECONNECT_MAX_DELAY",
		"ENABLE_WEBSOCKET", "ENABLE_KAFKA", "ENABLE_CACHE", "ENABLE_HTTP_API",
	}

	for _, envVar := range envVars {
		originalEnv[envVar] = os.Getenv(envVar)
	}

	// Clean up after test
	defer func() {
		for _, envVar := range envVars {
			if originalEnv[envVar] == "" {
				os.Unsetenv(envVar)
			} else {
				os.Setenv(envVar, originalEnv[envVar])
			}
		}
	}()

	// Test with default values
	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load config with defaults: %v", err)
	}

	// Test default values
	if config.ServiceName != "market-data" {
		t.Errorf("Expected ServiceName 'market-data', got '%s'", config.ServiceName)
	}
	if config.ServicePort != "8083" {
		t.Errorf("Expected ServicePort '8083', got '%s'", config.ServicePort)
	}
	if config.BitsoWSURL != "wss://ws.bitso.com" {
		t.Errorf("Expected BitsoWSURL 'wss://ws.bitso.com', got '%s'", config.BitsoWSURL)
	}
	if len(config.BitsoBooks) != 1 || config.BitsoBooks[0] != "btc_mxn" {
		t.Errorf("Expected BitsoBooks ['btc_mxn'], got %v", config.BitsoBooks)
	}
	if len(config.BitsoChannels) != 1 || config.BitsoChannels[0] != "trades" {
		t.Errorf("Expected BitsoChannels ['trades'], got %v", config.BitsoChannels)
	}
	if config.KafkaBrokers != "localhost:9092" {
		t.Errorf("Expected KafkaBrokers 'localhost:9092', got '%s'", config.KafkaBrokers)
	}
	if config.RedisHost != "localhost" {
		t.Errorf("Expected RedisHost 'localhost', got '%s'", config.RedisHost)
	}
	if config.RedisPort != "6379" {
		t.Errorf("Expected RedisPort '6379', got '%s'", config.RedisPort)
	}
	if config.RedisDB != 0 {
		t.Errorf("Expected RedisDB 0, got %d", config.RedisDB)
	}
	if config.CacheTickerTTL != 5*time.Second {
		t.Errorf("Expected CacheTickerTTL 5s, got %v", config.CacheTickerTTL)
	}
	if config.CacheOrderBookTTL != 2*time.Second {
		t.Errorf("Expected CacheOrderBookTTL 2s, got %v", config.CacheOrderBookTTL)
	}
	if config.CacheTradesSize != 1000 {
		t.Errorf("Expected CacheTradesSize 1000, got %d", config.CacheTradesSize)
	}
	if config.WSReconnectAttempts != 10 {
		t.Errorf("Expected WSReconnectAttempts 10, got %d", config.WSReconnectAttempts)
	}
	if config.WSReconnectInterval != 5*time.Second {
		t.Errorf("Expected WSReconnectInterval 5s, got %v", config.WSReconnectInterval)
	}
	if config.WSReconnectMaxDelay != 30*time.Second {
		t.Errorf("Expected WSReconnectMaxDelay 30s, got %v", config.WSReconnectMaxDelay)
	}
	if !config.EnableWebSocket {
		t.Error("Expected EnableWebSocket true")
	}
	if !config.EnableKafka {
		t.Error("Expected EnableKafka true")
	}
	if !config.EnableCache {
		t.Error("Expected EnableCache true")
	}
	if !config.EnableHTTPAPI {
		t.Error("Expected EnableHTTPAPI true")
	}
}

// TestLoadConfigWithCustomValues tests configuration loading with custom values
func TestLoadConfigWithCustomValues(t *testing.T) {
	// Save original environment variables
	originalEnv := make(map[string]string)
	envVars := []string{
		"SERVICE_NAME", "SERVICE_PORT", "BITSO_WS_URL", "BITSO_BOOKS", "BITSO_CHANNELS",
		"KAFKA_BROKERS", "KAFKA_TOPIC_TRADES", "KAFKA_TOPIC_ORDERBOOK", "KAFKA_TOPIC_TICKER",
		"REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "REDIS_DB",
		"CACHE_TICKER_TTL", "CACHE_ORDERBOOK_TTL", "CACHE_TRADES_SIZE",
		"PUBLISH_ORDERBOOK_INTERVAL", "PUBLISH_TICKER_INTERVAL",
		"WS_RECONNECT_ATTEMPTS", "WS_RECONNECT_INTERVAL", "WS_RECONNECT_MAX_DELAY",
		"ENABLE_WEBSOCKET", "ENABLE_KAFKA", "ENABLE_CACHE", "ENABLE_HTTP_API",
	}

	for _, envVar := range envVars {
		originalEnv[envVar] = os.Getenv(envVar)
	}

	// Clean up after test
	defer func() {
		for _, envVar := range envVars {
			if originalEnv[envVar] == "" {
				os.Unsetenv(envVar)
			} else {
				os.Setenv(envVar, originalEnv[envVar])
			}
		}
	}()

	// Set custom values
	os.Setenv("SERVICE_NAME", "custom-market-data")
	os.Setenv("SERVICE_PORT", "9090")
	os.Setenv("BITSO_WS_URL", "wss://custom.bitso.com")
	os.Setenv("BITSO_BOOKS", "btc_mxn,eth_mxn,xrp_mxn")
	os.Setenv("BITSO_CHANNELS", "trades,orders,diff-orders")
	os.Setenv("KAFKA_BROKERS", "kafka1:9092,kafka2:9092")
	os.Setenv("KAFKA_TOPIC_TRADES", "custom.trades")
	os.Setenv("KAFKA_TOPIC_ORDERBOOK", "custom.orderbook")
	os.Setenv("KAFKA_TOPIC_TICKER", "custom.ticker")
	os.Setenv("REDIS_HOST", "redis.example.com")
	os.Setenv("REDIS_PORT", "6380")
	os.Setenv("REDIS_PASSWORD", "secretpassword")
	os.Setenv("REDIS_DB", "5")
	os.Setenv("CACHE_TICKER_TTL", "10s")
	os.Setenv("CACHE_ORDERBOOK_TTL", "5s")
	os.Setenv("CACHE_TRADES_SIZE", "2000")
	os.Setenv("PUBLISH_ORDERBOOK_INTERVAL", "2s")
	os.Setenv("PUBLISH_TICKER_INTERVAL", "3s")
	os.Setenv("WS_RECONNECT_ATTEMPTS", "5")
	os.Setenv("WS_RECONNECT_INTERVAL", "2s")
	os.Setenv("WS_RECONNECT_MAX_DELAY", "20s")
	os.Setenv("ENABLE_WEBSOCKET", "false")
	os.Setenv("ENABLE_KAFKA", "false")
	os.Setenv("ENABLE_CACHE", "false")
	os.Setenv("ENABLE_HTTP_API", "false")

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load config with custom values: %v", err)
	}

	// Test custom values
	if config.ServiceName != "custom-market-data" {
		t.Errorf("Expected ServiceName 'custom-market-data', got '%s'", config.ServiceName)
	}
	if config.ServicePort != "9090" {
		t.Errorf("Expected ServicePort '9090', got '%s'", config.ServicePort)
	}
	if config.BitsoWSURL != "wss://custom.bitso.com" {
		t.Errorf("Expected BitsoWSURL 'wss://custom.bitso.com', got '%s'", config.BitsoWSURL)
	}
	expectedBooks := []string{"btc_mxn", "eth_mxn", "xrp_mxn"}
	if len(config.BitsoBooks) != len(expectedBooks) {
		t.Errorf("Expected %d books, got %d", len(expectedBooks), len(config.BitsoBooks))
	}
	for i, book := range expectedBooks {
		if config.BitsoBooks[i] != book {
			t.Errorf("Expected book[%d] '%s', got '%s'", i, book, config.BitsoBooks[i])
		}
	}
	expectedChannels := []string{"trades", "orders", "diff-orders"}
	if len(config.BitsoChannels) != len(expectedChannels) {
		t.Errorf("Expected %d channels, got %d", len(expectedChannels), len(config.BitsoChannels))
	}
	for i, channel := range expectedChannels {
		if config.BitsoChannels[i] != channel {
			t.Errorf("Expected channel[%d] '%s', got '%s'", i, channel, config.BitsoChannels[i])
		}
	}
	if config.KafkaBrokers != "kafka1:9092,kafka2:9092" {
		t.Errorf("Expected KafkaBrokers 'kafka1:9092,kafka2:9092', got '%s'", config.KafkaBrokers)
	}
	if config.KafkaTopicTrades != "custom.trades" {
		t.Errorf("Expected KafkaTopicTrades 'custom.trades', got '%s'", config.KafkaTopicTrades)
	}
	if config.RedisHost != "redis.example.com" {
		t.Errorf("Expected RedisHost 'redis.example.com', got '%s'", config.RedisHost)
	}
	if config.RedisPort != "6380" {
		t.Errorf("Expected RedisPort '6380', got '%s'", config.RedisPort)
	}
	if config.RedisPassword != "secretpassword" {
		t.Errorf("Expected RedisPassword 'secretpassword', got '%s'", config.RedisPassword)
	}
	if config.RedisDB != 5 {
		t.Errorf("Expected RedisDB 5, got %d", config.RedisDB)
	}
	if config.CacheTickerTTL != 10*time.Second {
		t.Errorf("Expected CacheTickerTTL 10s, got %v", config.CacheTickerTTL)
	}
	if config.CacheOrderBookTTL != 5*time.Second {
		t.Errorf("Expected CacheOrderBookTTL 5s, got %v", config.CacheOrderBookTTL)
	}
	if config.CacheTradesSize != 2000 {
		t.Errorf("Expected CacheTradesSize 2000, got %d", config.CacheTradesSize)
	}
	if config.WSReconnectAttempts != 5 {
		t.Errorf("Expected WSReconnectAttempts 5, got %d", config.WSReconnectAttempts)
	}
	if config.WSReconnectInterval != 2*time.Second {
		t.Errorf("Expected WSReconnectInterval 2s, got %v", config.WSReconnectInterval)
	}
	if config.WSReconnectMaxDelay != 20*time.Second {
		t.Errorf("Expected WSReconnectMaxDelay 20s, got %v", config.WSReconnectMaxDelay)
	}
	if config.EnableWebSocket {
		t.Error("Expected EnableWebSocket false")
	}
	if config.EnableKafka {
		t.Error("Expected EnableKafka false")
	}
	if config.EnableCache {
		t.Error("Expected EnableCache false")
	}
	if config.EnableHTTPAPI {
		t.Error("Expected EnableHTTPAPI false")
	}
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	// Save original environment variables
	originalEnv := make(map[string]string)
	envVars := []string{
		"SERVICE_NAME", "SERVICE_PORT", "BITSO_WS_URL", "BITSO_BOOKS", "KAFKA_BROKERS",
	}

	for _, envVar := range envVars {
		originalEnv[envVar] = os.Getenv(envVar)
	}

	// Clean up after test
	defer func() {
		for _, envVar := range envVars {
			if originalEnv[envVar] == "" {
				os.Unsetenv(envVar)
			} else {
				os.Setenv(envVar, originalEnv[envVar])
			}
		}
	}()

	// Test missing SERVICE_NAME
	os.Unsetenv("SERVICE_NAME")
	os.Setenv("SERVICE_PORT", "8080")
	os.Setenv("BITSO_WS_URL", "wss://ws.bitso.com")
	os.Setenv("BITSO_BOOKS", "btc_mxn")
	os.Setenv("KAFKA_BROKERS", "localhost:9092")

	_, err := LoadConfig()
	if err == nil {
		t.Error("Expected error for missing SERVICE_NAME")
	}

	// Test missing SERVICE_PORT
	os.Setenv("SERVICE_NAME", "test")
	os.Unsetenv("SERVICE_PORT")

	_, err = LoadConfig()
	if err == nil {
		t.Error("Expected error for missing SERVICE_PORT")
	}

	// Test missing BITSO_WS_URL
	os.Setenv("SERVICE_PORT", "8080")
	os.Unsetenv("BITSO_WS_URL")

	_, err = LoadConfig()
	if err == nil {
		t.Error("Expected error for missing BITSO_WS_URL")
	}

	// Test missing BITSO_BOOKS
	os.Setenv("BITSO_WS_URL", "wss://ws.bitso.com")
	os.Unsetenv("BITSO_BOOKS")

	_, err = LoadConfig()
	if err == nil {
		t.Error("Expected error for missing BITSO_BOOKS")
	}

	// Test empty BITSO_BOOKS
	os.Setenv("BITSO_BOOKS", "")

	_, err = LoadConfig()
	if err == nil {
		t.Error("Expected error for empty BITSO_BOOKS")
	}

	// Test missing KAFKA_BROKERS when Kafka is enabled
	os.Setenv("BITSO_BOOKS", "btc_mxn")
	os.Setenv("ENABLE_KAFKA", "true")
	os.Unsetenv("KAFKA_BROKERS")

	_, err = LoadConfig()
	if err == nil {
		t.Error("Expected error for missing KAFKA_BROKERS when Kafka is enabled")
	}
}

// TestHelperFunctions tests helper functions
func TestHelperFunctions(t *testing.T) {
	// Test getEnv
	if getEnv("NONEXISTENT_VAR", "default") != "default" {
		t.Error("getEnv should return default for non-existent variable")
	}

	os.Setenv("TEST_VAR", "test_value")
	if getEnv("TEST_VAR", "default") != "test_value" {
		t.Error("getEnv should return environment variable value")
	}
	os.Unsetenv("TEST_VAR")

	// Test getEnvAsInt
	if getEnvAsInt("NONEXISTENT_INT", 42) != 42 {
		t.Error("getEnvAsInt should return default for non-existent variable")
	}

	os.Setenv("TEST_INT", "123")
	if getEnvAsInt("TEST_INT", 42) != 123 {
		t.Error("getEnvAsInt should return parsed integer value")
	}
	os.Unsetenv("TEST_INT")

	// Test getEnvAsBool
	if getEnvAsBool("NONEXISTENT_BOOL", true) != true {
		t.Error("getEnvAsBool should return default for non-existent variable")
	}

	os.Setenv("TEST_BOOL", "true")
	if getEnvAsBool("TEST_BOOL", false) != true {
		t.Error("getEnvAsBool should return true for 'true'")
	}

	os.Setenv("TEST_BOOL", "1")
	if getEnvAsBool("TEST_BOOL", false) != true {
		t.Error("getEnvAsBool should return true for '1'")
	}

	os.Setenv("TEST_BOOL", "false")
	if getEnvAsBool("TEST_BOOL", true) != false {
		t.Error("getEnvAsBool should return false for 'false'")
	}

	os.Setenv("TEST_BOOL", "0")
	if getEnvAsBool("TEST_BOOL", true) != false {
		t.Error("getEnvAsBool should return false for '0'")
	}
	os.Unsetenv("TEST_BOOL")

	// Test getEnvAsDuration
	if getEnvAsDuration("NONEXISTENT_DURATION", 5*time.Second) != 5*time.Second {
		t.Error("getEnvAsDuration should return default for non-existent variable")
	}

	os.Setenv("TEST_DURATION", "10s")
	if getEnvAsDuration("TEST_DURATION", 5*time.Second) != 10*time.Second {
		t.Error("getEnvAsDuration should return parsed duration value")
	}

	os.Setenv("TEST_DURATION", "invalid")
	if getEnvAsDuration("TEST_DURATION", 5*time.Second) != 5*time.Second {
		t.Error("getEnvAsDuration should return default for invalid duration")
	}
	os.Unsetenv("TEST_DURATION")
}

// TestConfigCopy tests that config validation doesn't modify the original
func TestConfigCopy(t *testing.T) {
	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Validate config (this should not modify the original)
	err = config.Validate()
	if err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	// Check that original values are still intact
	if config.ServiceName != "market-data" {
		t.Errorf("ServiceName was modified during validation")
	}
	if config.ServicePort != "8083" {
		t.Errorf("ServicePort was modified during validation")
	}
}
