package strategies

import (
	"context"
	"testing"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

func TestNewEnhancedRegistry(t *testing.T) {
	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	registry := NewEnhancedRegistry(indicatorSvc)

	if registry == nil {
		t.Fatal("NewEnhancedRegistry() returned nil")
	}

	types := registry.GetAvailableTypes()
	if len(types) < 2 {
		t.Errorf("Expected at least 2 built-in strategy types, got %d", len(types))
	}

	foundMeanReversion := false
	foundMomentum := false
	for _, stratType := range types {
		if stratType == "mean_reversion" {
			foundMeanReversion = true
		}
		if stratType == "momentum" {
			foundMomentum = true
		}
	}

	if !foundMeanReversion {
		t.Error("Expected 'mean_reversion' in available types")
	}
	if !foundMomentum {
		t.Error("Expected 'momentum' in available types")
	}
}

func TestEnhancedRegistry_CreateAndRegister(t *testing.T) {
	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	registry := NewEnhancedRegistry(indicatorSvc)

	config := StrategyConfig{
		Name:    "test_mr_strategy",
		Type:    "mean_reversion",
		Version: "1.0.0",
		Enabled: true,
		Book:    "btc_mxn",
	}

	strategy, err := registry.CreateAndRegister(config)
	if err != nil {
		t.Fatalf("CreateAndRegister() failed: %v", err)
	}

	if strategy == nil {
		t.Fatal("Expected strategy, got nil")
	}

	if strategy.Name() != "test_mr_strategy" {
		t.Errorf("Expected strategy instance name 'test_mr_strategy', got '%s'", strategy.Name())
	}

	retrieved, err := registry.Get("test_mr_strategy")
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	if retrieved != strategy {
		t.Error("Expected to get same strategy instance")
	}
}

func TestEnhancedRegistry_CreateAndRegister_UnknownType(t *testing.T) {
	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	registry := NewEnhancedRegistry(indicatorSvc)

	config := StrategyConfig{
		Name: "test_unknown",
		Type: "unknown_strategy_type",
		Book: "btc_mxn",
	}

	_, err := registry.CreateAndRegister(config)
	if err == nil {
		t.Error("Expected error for unknown strategy type")
	}
}

func TestEnhancedRegistry_StartStop(t *testing.T) {
	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	registry := NewEnhancedRegistry(indicatorSvc)

	config := StrategyConfig{
		Name:    "test_lifecycle",
		Type:    "mean_reversion",
		Enabled: true,
		Book:    "btc_mxn",
	}

	_, err := registry.CreateAndRegister(config)
	if err != nil {
		t.Fatalf("CreateAndRegister() failed: %v", err)
	}

	ctx := context.Background()
	err = registry.Start(ctx, "test_lifecycle")
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	strategy, _ := registry.Get("test_lifecycle")
	if !strategy.IsRunning() {
		t.Error("Expected strategy to be running after Start()")
	}

	err = registry.Start(ctx, "test_lifecycle")
	if err == nil {
		t.Error("Expected error when starting already running strategy")
	}

	err = registry.Stop("test_lifecycle")
	if err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}

	if strategy.IsRunning() {
		t.Error("Expected strategy to be stopped after Stop()")
	}

	err = registry.Stop("test_lifecycle")
	if err == nil {
		t.Error("Expected error when stopping already stopped strategy")
	}
}

func TestEnhancedRegistry_Remove(t *testing.T) {
	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	registry := NewEnhancedRegistry(indicatorSvc)

	config := StrategyConfig{
		Name: "test_remove",
		Type: "mean_reversion",
		Book: "btc_mxn",
	}

	registry.CreateAndRegister(config)

	ctx := context.Background()
	registry.Start(ctx, "test_remove")

	err := registry.Remove("test_remove")
	if err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	_, err = registry.Get("test_remove")
	if err == nil {
		t.Error("Expected error when getting removed strategy")
	}

	err = registry.Remove("non_existent")
	if err == nil {
		t.Error("Expected error when removing non-existent strategy")
	}
}

func TestEnhancedRegistry_GetAllStrategyInfo(t *testing.T) {
	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	registry := NewEnhancedRegistry(indicatorSvc)

	configs := []StrategyConfig{
		{Name: "strategy_1", Type: "mean_reversion", Book: "btc_mxn", Enabled: true},
		{Name: "strategy_2", Type: "momentum", Book: "eth_mxn", Enabled: false},
	}

	for _, cfg := range configs {
		registry.CreateAndRegister(cfg)
	}

	ctx := context.Background()
	registry.Start(ctx, "strategy_1")

	infos := registry.GetAllStrategyInfo()

	if len(infos) != 2 {
		t.Errorf("Expected 2 strategy infos, got %d", len(infos))
	}

	var strategy1Info, strategy2Info *StrategyInfo
	for _, info := range infos {
		if info.Name == "strategy_1" && info.Book == "btc_mxn" {
			strategy1Info = info
		}
		if info.Name == "strategy_2" && info.Book == "eth_mxn" {
			strategy2Info = info
		}
	}

	if strategy1Info == nil {
		t.Error("Expected to find strategy_1 info")
	} else if !strategy1Info.Running {
		t.Error("Expected strategy_1 to be running")
	}

	if strategy2Info == nil {
		t.Error("Expected to find strategy_2 info")
	} else if strategy2Info.Running {
		t.Error("Expected strategy_2 to not be running")
	}
}

func TestEnhancedRegistry_ProcessTick(t *testing.T) {
	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	registry := NewEnhancedRegistry(indicatorSvc)

	config := StrategyConfig{
		Name:    "test_tick",
		Type:    "mean_reversion",
		Book:    "btc_mxn",
		Enabled: true,
		Parameters: map[string]interface{}{
			"min_signal_interval": float64(0),
		},
	}

	registry.CreateAndRegister(config)

	ctx := context.Background()
	registry.Start(ctx, "test_tick")

	store.SetBollinger(ctx, "btc_mxn", 20, &indicators.BollingerBands{
		Upper:  1210000,
		Middle: 1200000,
		Lower:  1190000,
		StdDev: 5000,
	})

	tick := &indicators.Trade{
		Timestamp: time.Now(),
		Price:     1185000,
		Amount:    0.01,
		Side:      "buy",
	}

	signals, err := registry.ProcessTick(tick, "btc_mxn")
	if err != nil {
		t.Fatalf("ProcessTick() error: %v", err)
	}

	if len(signals) != 1 {
		t.Errorf("Expected 1 signal, got %d", len(signals))
	}

	if len(signals) > 0 && signals[0].Side != "BUY" {
		t.Errorf("Expected BUY signal, got %s", signals[0].Side)
	}
}

func TestEnhancedRegistry_ProcessTick_WrongBook(t *testing.T) {
	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	registry := NewEnhancedRegistry(indicatorSvc)

	config := StrategyConfig{
		Name: "test_book_filter",
		Type: "mean_reversion",
		Book: "btc_mxn",
	}

	registry.CreateAndRegister(config)
	registry.Start(context.Background(), "test_book_filter")

	tick := &indicators.Trade{
		Timestamp: time.Now(),
		Price:     1185000,
		Amount:    0.01,
	}

	signals, _ := registry.ProcessTick(tick, "eth_mxn")

	if len(signals) != 0 {
		t.Errorf("Expected 0 signals for different book, got %d", len(signals))
	}
}

func TestEnhancedRegistry_StartAll_StopAll(t *testing.T) {
	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	registry := NewEnhancedRegistry(indicatorSvc)

	configs := []StrategyConfig{
		{Name: "strat_1", Type: "mean_reversion", Book: "btc_mxn"},
		{Name: "strat_2", Type: "momentum", Book: "eth_mxn"},
	}

	for _, cfg := range configs {
		registry.CreateAndRegister(cfg)
	}

	ctx := context.Background()
	err := registry.StartAll(ctx)
	if err != nil {
		t.Fatalf("StartAll() failed: %v", err)
	}

	active := registry.GetActiveStrategies()
	if len(active) != 2 {
		t.Errorf("Expected 2 active strategies after StartAll, got %d", len(active))
	}

	err = registry.StopAll()
	if err != nil {
		t.Fatalf("StopAll() failed: %v", err)
	}

	active = registry.GetActiveStrategies()
	if len(active) != 0 {
		t.Errorf("Expected 0 active strategies after StopAll, got %d", len(active))
	}
}

func TestEnhancedRegistry_GetStats(t *testing.T) {
	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	registry := NewEnhancedRegistry(indicatorSvc)

	configs := []StrategyConfig{
		{Name: "stat_1", Type: "mean_reversion", Book: "btc_mxn"},
		{Name: "stat_2", Type: "momentum", Book: "eth_mxn"},
	}

	for _, cfg := range configs {
		registry.CreateAndRegister(cfg)
	}

	registry.Start(context.Background(), "stat_1")

	stats := registry.GetStats()

	if stats.TotalStrategies != 2 {
		t.Errorf("Expected TotalStrategies 2, got %d", stats.TotalStrategies)
	}

	if stats.ActiveStrategies != 1 {
		t.Errorf("Expected ActiveStrategies 1, got %d", stats.ActiveStrategies)
	}

	if len(stats.AvailableTypes) < 2 {
		t.Errorf("Expected at least 2 available types, got %d", len(stats.AvailableTypes))
	}
}

func TestEnhancedRegistry_RegisterFactory(t *testing.T) {
	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	registry := NewEnhancedRegistry(indicatorSvc)

	customFactory := func() EnhancedStrategy {
		return NewMeanReversionStrategy()
	}

	err := registry.RegisterFactory("custom_type", customFactory)
	if err != nil {
		t.Fatalf("RegisterFactory() failed: %v", err)
	}

	types := registry.GetAvailableTypes()
	found := false
	for _, t := range types {
		if t == "custom_type" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'custom_type' in available types")
	}

	err = registry.RegisterFactory("custom_type", customFactory)
	if err == nil {
		t.Error("Expected error when registering duplicate factory")
	}
}
