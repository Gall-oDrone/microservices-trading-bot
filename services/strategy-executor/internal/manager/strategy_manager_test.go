package manager

import (
	"context"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/strategy-executor/internal/logger"
	"bitso-trading-platform/strategy-executor/internal/metrics"
	"bitso-trading-platform/strategy-executor/internal/processor"
	"bitso-trading-platform/strategy-executor/internal/risk"
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

func TestNewManager(t *testing.T) {
	logger := logger.NewDefault()
	metrics := metrics.New("test")
	registry := strategies.NewRegistry()
	riskMgr := risk.NewManager(models.NewTradingConfig())

	manager := NewManager(logger, metrics, registry, riskMgr)

	if manager == nil {
		t.Fatal("NewManager() returned nil")
	}

	if manager.logger == nil {
		t.Error("Expected logger to be set")
	}

	if manager.metrics == nil {
		t.Error("Expected metrics to be set")
	}

	if manager.registry == nil {
		t.Error("Expected registry to be set")
	}

	if manager.riskMgr == nil {
		t.Error("Expected risk manager to be set")
	}
}

func TestManager_StartStop(t *testing.T) {
	logger := logger.NewDefault()
	metrics := metrics.New("test")
	registry := strategies.NewRegistry()
	riskMgr := risk.NewManager(models.NewTradingConfig())

	manager := NewManager(logger, metrics, registry, riskMgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start manager
	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Give it time to start
	time.Sleep(100 * time.Millisecond)

	// Stop manager
	err = manager.Stop()
	if err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
}

func TestManager_StartStrategy(t *testing.T) {
	logger := logger.NewDefault()
	metrics := metrics.New("test")
	registry := strategies.NewRegistry()
	riskMgr := risk.NewManager(models.NewTradingConfig())

	manager := NewManager(logger, metrics, registry, riskMgr)

	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)

	// Start strategy
	err := manager.StartStrategy("basic", config)
	if err != nil {
		t.Fatalf("StartStrategy() failed: %v", err)
	}

	// Check if strategy is running
	status, err := manager.GetStrategyStatus("basic")
	if err != nil {
		t.Fatalf("GetStrategyStatus() failed: %v", err)
	}

	if status.Name != "basic" {
		t.Errorf("Expected strategy name 'basic', got '%s'", status.Name)
	}

	if status.Status != StrategyStatusActive {
		t.Errorf("Expected status active, got %s", status.Status)
	}

	// Try to start same strategy again (should fail)
	err = manager.StartStrategy("basic", config)
	if err == nil {
		t.Error("Expected error when starting same strategy twice")
	}

	// Clean up
	manager.StopStrategy("basic")
}

func TestManager_StopStrategy(t *testing.T) {
	logger := logger.NewDefault()
	metrics := metrics.New("test")
	registry := strategies.NewRegistry()
	riskMgr := risk.NewManager(models.NewTradingConfig())

	manager := NewManager(logger, metrics, registry, riskMgr)

	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)

	// Start strategy
	err := manager.StartStrategy("basic", config)
	if err != nil {
		t.Fatalf("StartStrategy() failed: %v", err)
	}

	// Stop strategy
	err = manager.StopStrategy("basic")
	if err != nil {
		t.Fatalf("StopStrategy() failed: %v", err)
	}

	// Check if strategy is stopped
	_, err = manager.GetStrategyStatus("basic")
	if err == nil {
		t.Error("Expected error when getting status of stopped strategy")
	}

	// Try to stop non-existent strategy
	err = manager.StopStrategy("non-existent")
	if err == nil {
		t.Error("Expected error when stopping non-existent strategy")
	}
}

func TestManager_GetStrategyStatus(t *testing.T) {
	logger := logger.NewDefault()
	metrics := metrics.New("test")
	registry := strategies.NewRegistry()
	riskMgr := risk.NewManager(models.NewTradingConfig())

	manager := NewManager(logger, metrics, registry, riskMgr)

	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)

	// Start strategy
	err := manager.StartStrategy("basic", config)
	if err != nil {
		t.Fatalf("StartStrategy() failed: %v", err)
	}

	// Get status
	status, err := manager.GetStrategyStatus("basic")
	if err != nil {
		t.Fatalf("GetStrategyStatus() failed: %v", err)
	}

	if status.Name != "basic" {
		t.Errorf("Expected name 'basic', got '%s'", status.Name)
	}

	if status.Status != StrategyStatusActive {
		t.Errorf("Expected status active, got %s", status.Status)
	}

	if status.Book != "btc_mxn" {
		t.Errorf("Expected book 'btc_mxn', got '%s'", status.Book)
	}

	// Clean up
	manager.StopStrategy("basic")
}

func TestManager_UpdateStrategyConfig(t *testing.T) {
	logger := logger.NewDefault()
	metrics := metrics.New("test")
	registry := strategies.NewRegistry()
	riskMgr := risk.NewManager(models.NewTradingConfig())

	manager := NewManager(logger, metrics, registry, riskMgr)

	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)
	config.MaxTradeAmount = 0.1

	// Start strategy
	err := manager.StartStrategy("basic", config)
	if err != nil {
		t.Fatalf("StartStrategy() failed: %v", err)
	}

	// Update configuration
	newConfig := models.NewTradingConfig()
	newConfig.Book = bitso.NewBook(bitso.BTC, bitso.MXN)
	newConfig.MaxTradeAmount = 0.2

	err = manager.UpdateStrategyConfig("basic", newConfig)
	if err != nil {
		t.Fatalf("UpdateStrategyConfig() failed: %v", err)
	}

	// Try to update non-existent strategy
	err = manager.UpdateStrategyConfig("non-existent", newConfig)
	if err == nil {
		t.Error("Expected error when updating non-existent strategy")
	}

	// Clean up
	manager.StopStrategy("basic")
}

func TestManager_GetAllStatuses(t *testing.T) {
	logger := logger.NewDefault()
	metrics := metrics.New("test")
	registry := strategies.NewRegistry()
	riskMgr := risk.NewManager(models.NewTradingConfig())

	manager := NewManager(logger, metrics, registry, riskMgr)

	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)

	// Start multiple strategies
	manager.StartStrategy("basic", config)
	manager.StartStrategy("trend", config)

	// Get all statuses
	statuses := manager.GetAllStatuses()

	if len(statuses) != 2 {
		t.Errorf("Expected 2 statuses, got %d", len(statuses))
	}

	if _, exists := statuses["basic"]; !exists {
		t.Error("Expected 'basic' strategy in statuses")
	}

	if _, exists := statuses["trend"]; !exists {
		t.Error("Expected 'trend' strategy in statuses")
	}

	// Clean up
	manager.StopStrategy("basic")
	manager.StopStrategy("trend")
}

func TestManager_ProcessMarketData(t *testing.T) {
	logger := logger.NewDefault()
	metrics := metrics.New("test")
	registry := strategies.NewRegistry()
	riskMgr := risk.NewManager(models.NewTradingConfig())

	manager := NewManager(logger, metrics, registry, riskMgr)

	config := models.NewTradingConfig()
	config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)

	// Start strategy
	err := manager.StartStrategy("basic", config)
	if err != nil {
		t.Fatalf("StartStrategy() failed: %v", err)
	}

	// Create market data event
	event := &processor.ProcessedEvent{
		Type: processor.EventTypeTrade,
		Book: "btc_mxn",
		Metadata: map[string]interface{}{
			"price":  1000.0,
			"amount": 0.5,
			"bid":    999.0,
			"ask":    1001.0,
		},
		Timestamp: time.Now(),
	}

	// Process market data
	err = manager.ProcessMarketData(event)
	if err != nil {
		t.Fatalf("ProcessMarketData() failed: %v", err)
	}

	// Give it time to process
	time.Sleep(100 * time.Millisecond)

	// Check if execution count increased
	status, _ := manager.GetStrategyStatus("basic")
	if status.Executions == 0 {
		t.Error("Expected executions count to be > 0")
	}

	// Clean up
	manager.StopStrategy("basic")
}

func TestManager_ConvertEventToTicker(t *testing.T) {
	logger := logger.NewDefault()
	metrics := metrics.New("test")
	registry := strategies.NewRegistry()
	riskMgr := risk.NewManager(models.NewTradingConfig())

	manager := NewManager(logger, metrics, registry, riskMgr)

	// Test with valid event
	event := &processor.ProcessedEvent{
		Type: processor.EventTypeTrade,
		Book: "btc_mxn",
		Metadata: map[string]interface{}{
			"price":  1000.0,
			"amount": 0.5,
			"bid":    999.0,
			"ask":    1001.0,
			"last":   1000.5,
		},
		Timestamp: time.Now(),
	}

	ticker, err := manager.convertEventToTicker(event)
	if err != nil {
		t.Fatalf("convertEventToTicker() failed: %v", err)
	}

	if ticker == nil {
		t.Fatal("Expected ticker, got nil")
	}

	if ticker.Book.String() != "btc_mxn" {
		t.Errorf("Expected book 'btc_mxn', got '%s'", ticker.Book.String())
	}

	if string(ticker.Bid) == "" {
		t.Error("Expected bid to be set")
	}

	if string(ticker.Ask) == "" {
		t.Error("Expected ask to be set")
	}

	if string(ticker.Last) == "" {
		t.Error("Expected last to be set")
	}

	// Test with invalid book
	invalidEvent := &processor.ProcessedEvent{
		Type: processor.EventTypeTrade,
		Book: "invalid",
		Metadata: map[string]interface{}{
			"price": 1000.0,
		},
		Timestamp: time.Now(),
	}

	_, err = manager.convertEventToTicker(invalidEvent)
	if err == nil {
		t.Error("Expected error for invalid book")
	}
}

func TestStrategyStatus(t *testing.T) {
	status := &StrategyStatus{
		Name:          "test-strategy",
		Status:        StrategyStatusActive,
		Book:          "btc_mxn",
		StartTime:     time.Now(),
		LastExecution: time.Now(),
		Executions:    100,
		Signals:       50,
		Errors:        5,
		LastError:     "test error",
	}

	if status.Name != "test-strategy" {
		t.Errorf("Expected name 'test-strategy', got '%s'", status.Name)
	}

	if status.Status != StrategyStatusActive {
		t.Errorf("Expected status active, got %s", status.Status)
	}

	if status.Executions != 100 {
		t.Errorf("Expected 100 executions, got %d", status.Executions)
	}

	if status.Signals != 50 {
		t.Errorf("Expected 50 signals, got %d", status.Signals)
	}
}

func TestStrategyStatusType(t *testing.T) {
	tests := []struct {
		name   string
		status StrategyStatusType
		want   string
	}{
		{"active", StrategyStatusActive, "active"},
		{"inactive", StrategyStatusInactive, "inactive"},
		{"error", StrategyStatusError, "error"},
		{"stopping", StrategyStatusStopping, "stopping"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("Expected status '%s', got '%s'", tt.want, string(tt.status))
			}
		})
	}
}
