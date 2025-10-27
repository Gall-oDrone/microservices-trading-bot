package strategies

import (
	"testing"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
)

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()

	if registry == nil {
		t.Fatal("NewRegistry() returned nil")
	}

	// Check if built-in strategies are registered
	available := registry.GetAvailableStrategies()
	if len(available) < 3 {
		t.Errorf("Expected at least 3 built-in strategies, got %d", len(available))
	}

	// Check for specific strategies
	expectedStrategies := []string{"basic", "trend", "arbitrage"}
	for _, name := range expectedStrategies {
		found := false
		for _, available := range registry.GetAvailableStrategies() {
			if available == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected strategy '%s' to be registered", name)
		}
	}
}

func TestRegistry_RegisterFactory(t *testing.T) {
	registry := NewRegistry()

	// Create a custom factory
	customFactory := func(config *models.TradingConfig) (Strategy, error) {
		return NewBasicStrategy(config.Book), nil
	}

	// Register factory
	err := registry.RegisterFactory("custom", customFactory)
	if err != nil {
		t.Errorf("RegisterFactory() failed: %v", err)
	}

	// Try to register same factory again
	err = registry.RegisterFactory("custom", customFactory)
	if err == nil {
		t.Error("Expected error when registering duplicate factory")
	}

	// Check if factory is available
	available := registry.GetAvailableStrategies()
	found := false
	for _, name := range available {
		if name == "custom" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'custom' strategy to be available")
	}
}

func TestRegistry_UnregisterFactory(t *testing.T) {
	registry := NewRegistry()

	// Register a custom factory
	customFactory := func(config *models.TradingConfig) (Strategy, error) {
		return NewBasicStrategy(config.Book), nil
	}
	registry.RegisterFactory("custom", customFactory)

	// Unregister factory
	err := registry.UnregisterFactory("custom")
	if err != nil {
		t.Errorf("UnregisterFactory() failed: %v", err)
	}

	// Try to unregister non-existent factory
	err = registry.UnregisterFactory("non-existent")
	if err == nil {
		t.Error("Expected error when unregistering non-existent factory")
	}
}

func TestRegistry_CreateStrategy(t *testing.T) {
	registry := NewRegistry()

	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)

	// Create basic strategy
	strategy, err := registry.CreateStrategy("basic", config)
	if err != nil {
		t.Fatalf("CreateStrategy() failed: %v", err)
	}

	if strategy == nil {
		t.Fatal("Expected strategy, got nil")
	}

	if strategy.GetName() != "basic" {
		t.Errorf("Expected strategy name 'basic', got '%s'", strategy.GetName())
	}

	// Try to create non-existent strategy
	_, err = registry.CreateStrategy("non-existent", config)
	if err == nil {
		t.Error("Expected error when creating non-existent strategy")
	}

	// Clean up
	strategy.Stop()
}

func TestRegistry_GetStrategy(t *testing.T) {
	registry := NewRegistry()

	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)

	// Create strategy
	created, err := registry.CreateStrategy("basic", config)
	if err != nil {
		t.Fatalf("CreateStrategy() failed: %v", err)
	}

	// Get strategy
	retrieved, err := registry.GetStrategy("basic")
	if err != nil {
		t.Fatalf("GetStrategy() failed: %v", err)
	}

	if retrieved != created {
		t.Error("Expected to get same strategy instance")
	}

	// Try to get non-existent strategy
	_, err = registry.GetStrategy("non-existent")
	if err == nil {
		t.Error("Expected error when getting non-existent strategy")
	}

	// Clean up
	registry.RemoveStrategy("basic")
}

func TestRegistry_RemoveStrategy(t *testing.T) {
	registry := NewRegistry()

	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)

	// Create strategy
	_, err := registry.CreateStrategy("basic", config)
	if err != nil {
		t.Fatalf("CreateStrategy() failed: %v", err)
	}

	// Remove strategy
	err = registry.RemoveStrategy("basic")
	if err != nil {
		t.Fatalf("RemoveStrategy() failed: %v", err)
	}

	// Check if strategy is removed
	_, err = registry.GetStrategy("basic")
	if err == nil {
		t.Error("Expected error when getting removed strategy")
	}

	// Try to remove non-existent strategy
	err = registry.RemoveStrategy("non-existent")
	if err == nil {
		t.Error("Expected error when removing non-existent strategy")
	}
}

func TestRegistry_GetAllStrategies(t *testing.T) {
	registry := NewRegistry()

	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)

	// Create multiple strategies
	registry.CreateStrategy("basic", config)
	registry.CreateStrategy("trend", config)

	// Get all strategies
	all := registry.GetAllStrategies()

	if len(all) != 2 {
		t.Errorf("Expected 2 strategies, got %d", len(all))
	}

	if _, exists := all["basic"]; !exists {
		t.Error("Expected 'basic' strategy in all strategies")
	}

	if _, exists := all["trend"]; !exists {
		t.Error("Expected 'trend' strategy in all strategies")
	}

	// Clean up
	registry.StopAll()
}

func TestRegistry_GetAvailableStrategies(t *testing.T) {
	registry := NewRegistry()

	available := registry.GetAvailableStrategies()

	if len(available) < 3 {
		t.Errorf("Expected at least 3 available strategies, got %d", len(available))
	}

	// Should include built-in strategies
	expectedStrategies := []string{"basic", "trend", "arbitrage"}
	for _, expected := range expectedStrategies {
		found := false
		for _, name := range available {
			if name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected '%s' in available strategies", expected)
		}
	}
}

func TestRegistry_GetActiveStrategies(t *testing.T) {
	registry := NewRegistry()

	// Initially, no active strategies
	active := registry.GetActiveStrategies()
	if len(active) != 0 {
		t.Errorf("Expected 0 active strategies, got %d", len(active))
	}

	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)

	// Create strategies
	registry.CreateStrategy("basic", config)
	registry.CreateStrategy("trend", config)

	// Get active strategies
	active = registry.GetActiveStrategies()
	if len(active) != 2 {
		t.Errorf("Expected 2 active strategies, got %d", len(active))
	}

	// Should include created strategies
	expectedActive := []string{"basic", "trend"}
	for _, expected := range expectedActive {
		found := false
		for _, name := range active {
			if name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected '%s' in active strategies", expected)
		}
	}

	// Clean up
	registry.StopAll()
}

func TestRegistry_StopAll(t *testing.T) {
	registry := NewRegistry()

	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)

	// Create multiple strategies
	registry.CreateStrategy("basic", config)
	registry.CreateStrategy("trend", config)
	registry.CreateStrategy("arbitrage", config)

	// Stop all
	err := registry.StopAll()
	if err != nil {
		t.Errorf("StopAll() failed: %v", err)
	}

	// Check if all strategies are stopped
	active := registry.GetActiveStrategies()
	if len(active) != 0 {
		t.Errorf("Expected 0 active strategies after StopAll, got %d", len(active))
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	registry := NewRegistry()

	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)

	// Test concurrent creation and retrieval
	done := make(chan bool)

	// Goroutine 1: Create strategies
	go func() {
		for i := 0; i < 10; i++ {
			registry.CreateStrategy("basic", config)
			registry.RemoveStrategy("basic")
		}
		done <- true
	}()

	// Goroutine 2: Get strategies
	go func() {
		for i := 0; i < 10; i++ {
			registry.GetAllStrategies()
			registry.GetActiveStrategies()
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// No assertions needed - just checking for race conditions
	// Run with: go test -race
}

func TestStrategyFactory(t *testing.T) {
	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)

	tests := []struct {
		name     string
		factory  StrategyFactory
		config   *models.TradingConfig
		wantErr  bool
		wantName string
	}{
		{
			name:     "basic strategy factory",
			factory:  NewBasicStrategyFactory(),
			config:   config,
			wantErr:  false,
			wantName: "basic",
		},
		{
			name:     "trend strategy factory",
			factory:  NewTrendStrategyFactory(),
			config:   config,
			wantErr:  false,
			wantName: "trend_following",
		},
		{
			name:     "arbitrage strategy factory",
			factory:  NewArbitrageStrategyFactory(),
			config:   config,
			wantErr:  false,
			wantName: "arbitrage",
		},
		{
			name:     "nil config",
			factory:  NewBasicStrategyFactory(),
			config:   nil,
			wantErr:  true,
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy, err := tt.factory(tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if strategy == nil {
				t.Error("Expected strategy, got nil")
				return
			}

			if strategy.GetName() != tt.wantName {
				t.Errorf("Expected strategy name '%s', got '%s'", tt.wantName, strategy.GetName())
			}

			// Clean up
			strategy.Stop()
		})
	}
}
