package strategies

import (
	"context"
	"testing"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

func TestNewMeanReversionStrategy(t *testing.T) {
	strategy := NewMeanReversionStrategy()

	if strategy == nil {
		t.Fatal("NewMeanReversionStrategy() returned nil")
	}

	if strategy.Name() != "mean_reversion" {
		t.Errorf("Expected name 'mean_reversion', got '%s'", strategy.Name())
	}

	if strategy.Version() != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", strategy.Version())
	}
}

func TestMeanReversionStrategy_Initialize(t *testing.T) {
	strategy := NewMeanReversionStrategy()

	config := StrategyConfig{
		Name:    "test_mean_reversion",
		Type:    "mean_reversion",
		Version: "1.0.0",
		Enabled: true,
		Book:    "btc_mxn",
		Parameters: map[string]interface{}{
			"lookback_period":     float64(30),
			"entry_threshold":     float64(2.5),
			"exit_threshold":      float64(0.3),
			"min_signal_interval": float64(120),
			"position_size":       float64(0.005),
		},
		Sizing: SizingConfig{
			Method:           "fixed",
			MaxPositionSize:  0.01,
			MaxPositionValue: 20000,
		},
	}

	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	err := strategy.Initialize(config, indicatorSvc)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	mrConfig := strategy.GetMeanReversionConfig()

	if mrConfig.LookbackPeriod != 30 {
		t.Errorf("Expected LookbackPeriod 30, got %d", mrConfig.LookbackPeriod)
	}

	if mrConfig.EntryThreshold != 2.5 {
		t.Errorf("Expected EntryThreshold 2.5, got %f", mrConfig.EntryThreshold)
	}

	if mrConfig.ExitThreshold != 0.3 {
		t.Errorf("Expected ExitThreshold 0.3, got %f", mrConfig.ExitThreshold)
	}

	if mrConfig.PositionSize != 0.005 {
		t.Errorf("Expected PositionSize 0.005 (from parameters, under max cap), got %f", mrConfig.PositionSize)
	}
}

func TestResolvePositionSize_ExplicitParamWinsUnderCap(t *testing.T) {
	got := ResolvePositionSize(map[string]interface{}{"position_size": 0.005}, 0.01, 0.001)
	if got != 0.005 {
		t.Errorf("expected 0.005, got %f", got)
	}
}

func TestResolvePositionSize_CappedByMax(t *testing.T) {
	got := ResolvePositionSize(map[string]interface{}{"position_size": 0.02}, 0.01, 0.001)
	if got != 0.01 {
		t.Errorf("expected cap 0.01, got %f", got)
	}
}

func TestResolvePositionSize_DefaultFromMaxWhenParamMissing(t *testing.T) {
	got := ResolvePositionSize(nil, 0.01, 0.001)
	if got != 0.01 {
		t.Errorf("expected 0.01 from max, got %f", got)
	}
}

func TestMeanReversionStrategy_OnTick_BuySignal(t *testing.T) {
	strategy := NewMeanReversionStrategy()

	config := StrategyConfig{
		Name:    "test_mean_reversion",
		Type:    "mean_reversion",
		Book:    "btc_mxn",
		Enabled: true,
		Parameters: map[string]interface{}{
			"entry_threshold":     float64(2.0),
			"exit_threshold":      float64(0.5),
			"min_signal_interval": float64(0),
		},
		Sizing: SizingConfig{
			MaxPositionSize: 0.001,
		},
	}

	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	ctx := context.Background()
	store.SetBollinger(ctx, "btc_mxn", 20, &indicators.BollingerBands{
		Upper:  1210000,
		Middle: 1200000,
		Lower:  1190000,
		StdDev: 5000,
	})

	strategy.Initialize(config, indicatorSvc)
	strategy.Start(ctx)

	tick := &indicators.Trade{
		Timestamp: time.Now(),
		Price:     1185000,
		Amount:    0.01,
		Side:      "buy",
	}

	signal, err := strategy.OnTick(tick)
	if err != nil {
		t.Fatalf("OnTick() error: %v", err)
	}

	if signal == nil {
		t.Fatal("Expected BUY signal when price below lower band, got nil")
	}

	if signal.Side != "BUY" {
		t.Errorf("Expected signal side 'BUY', got '%s'", signal.Side)
	}

	if signal.Strategy != "test_mean_reversion" {
		t.Errorf("Expected signal strategy id 'test_mean_reversion', got '%s'", signal.Strategy)
	}

	if signal.Book != "btc_mxn" {
		t.Errorf("Expected book 'btc_mxn', got '%s'", signal.Book)
	}

	if signal.Confidence <= 0 || signal.Confidence > 1 {
		t.Errorf("Expected confidence between 0 and 1, got %f", signal.Confidence)
	}
}

func TestMeanReversionStrategy_OnTick_SellSignal(t *testing.T) {
	strategy := NewMeanReversionStrategy()

	config := StrategyConfig{
		Name:    "test_mean_reversion",
		Type:    "mean_reversion",
		Book:    "btc_mxn",
		Enabled: true,
		Parameters: map[string]interface{}{
			"entry_threshold":     float64(2.0),
			"exit_threshold":      float64(0.5),
			"min_signal_interval": float64(0),
		},
		Sizing: SizingConfig{
			MaxPositionSize: 0.001,
		},
	}

	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	ctx := context.Background()
	store.SetBollinger(ctx, "btc_mxn", 20, &indicators.BollingerBands{
		Upper:  1210000,
		Middle: 1200000,
		Lower:  1190000,
		StdDev: 5000,
	})

	strategy.Initialize(config, indicatorSvc)
	strategy.Start(ctx)

	tick := &indicators.Trade{
		Timestamp: time.Now(),
		Price:     1215000,
		Amount:    0.01,
		Side:      "buy",
	}

	signal, err := strategy.OnTick(tick)
	if err != nil {
		t.Fatalf("OnTick() error: %v", err)
	}

	if signal == nil {
		t.Fatal("Expected SELL signal when price above upper band, got nil")
	}

	if signal.Side != "SELL" {
		t.Errorf("Expected signal side 'SELL', got '%s'", signal.Side)
	}
}

func TestMeanReversionStrategy_OnTick_NoSignal(t *testing.T) {
	strategy := NewMeanReversionStrategy()

	config := StrategyConfig{
		Name:    "test_mean_reversion",
		Type:    "mean_reversion",
		Book:    "btc_mxn",
		Enabled: true,
		Parameters: map[string]interface{}{
			"entry_threshold":     float64(2.0),
			"exit_threshold":      float64(0.5),
			"min_signal_interval": float64(0),
		},
	}

	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	ctx := context.Background()
	store.SetBollinger(ctx, "btc_mxn", 20, &indicators.BollingerBands{
		Upper:  1210000,
		Middle: 1200000,
		Lower:  1190000,
		StdDev: 5000,
	})

	strategy.Initialize(config, indicatorSvc)
	strategy.Start(ctx)

	tick := &indicators.Trade{
		Timestamp: time.Now(),
		Price:     1200000,
		Amount:    0.01,
		Side:      "buy",
	}

	signal, err := strategy.OnTick(tick)
	if err != nil {
		t.Fatalf("OnTick() error: %v", err)
	}

	if signal != nil {
		t.Errorf("Expected no signal when price within bands, got %+v", signal)
	}
}

func TestMeanReversionStrategy_ExitSignal(t *testing.T) {
	strategy := NewMeanReversionStrategy()

	config := StrategyConfig{
		Name:    "test_mean_reversion",
		Type:    "mean_reversion",
		Book:    "btc_mxn",
		Enabled: true,
		Parameters: map[string]interface{}{
			"entry_threshold":     float64(2.0),
			"exit_threshold":      float64(0.5),
			"min_signal_interval": float64(0),
		},
		Sizing: SizingConfig{
			MaxPositionSize: 0.001,
		},
	}

	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	ctx := context.Background()
	store.SetBollinger(ctx, "btc_mxn", 20, &indicators.BollingerBands{
		Upper:  1210000,
		Middle: 1200000,
		Lower:  1190000,
		StdDev: 5000,
	})

	strategy.Initialize(config, indicatorSvc)
	strategy.Start(ctx)

	strategy.SetPosition("BUY", 0.001, 1185000)

	tick := &indicators.Trade{
		Timestamp: time.Now(),
		Price:     1200500,
		Amount:    0.01,
		Side:      "buy",
	}

	signal, err := strategy.OnTick(tick)
	if err != nil {
		t.Fatalf("OnTick() error: %v", err)
	}

	if signal == nil {
		t.Fatal("Expected exit signal when price returns to mean, got nil")
	}

	if signal.Side != "SELL" {
		t.Errorf("Expected exit signal side 'SELL', got '%s'", signal.Side)
	}

	state := strategy.GetState()
	if !state.HasPosition {
		t.Error("Expected position to remain until SELL fill is confirmed")
	}
	if !state.PendingSell {
		t.Error("Expected PendingSell after exit signal")
	}
}

func TestMeanReversionStrategy_NotRunning(t *testing.T) {
	strategy := NewMeanReversionStrategy()

	config := StrategyConfig{
		Name: "test_mean_reversion",
		Type: "mean_reversion",
		Book: "btc_mxn",
	}

	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	strategy.Initialize(config, indicatorSvc)

	tick := &indicators.Trade{
		Timestamp: time.Now(),
		Price:     1185000,
		Amount:    0.01,
	}

	signal, err := strategy.OnTick(tick)
	if err != nil {
		t.Fatalf("OnTick() error: %v", err)
	}

	if signal != nil {
		t.Error("Expected no signal when strategy is not running")
	}
}

func TestMeanReversionStrategy_Reset(t *testing.T) {
	strategy := NewMeanReversionStrategy()

	config := StrategyConfig{
		Name: "test_mean_reversion",
		Type: "mean_reversion",
		Book: "btc_mxn",
	}

	store := indicators.NewInMemoryIndicatorStore()
	provider := indicators.NewMockDataProvider()
	indicatorSvc := indicators.NewService(nil, store, provider, nil)

	strategy.Initialize(config, indicatorSvc)
	strategy.Start(context.Background())

	strategy.SetPosition("BUY", 0.001, 1185000)
	strategy.RecordSignal()
	strategy.RecordTrade(true)

	state := strategy.GetState()
	if state.SignalCount != 1 || state.TradeCount != 1 {
		t.Error("Expected signal and trade to be recorded")
	}

	strategy.Reset()

	state = strategy.GetState()
	if state.SignalCount != 0 || state.TradeCount != 0 || state.HasPosition {
		t.Error("Expected state to be reset")
	}
}

func TestMeanReversionStrategy_CalculateConfidence(t *testing.T) {
	strategy := NewMeanReversionStrategy()

	bb := &indicators.BollingerBands{
		Upper:  1210000,
		Middle: 1200000,
		Lower:  1190000,
		StdDev: 5000,
	}

	prices := []float64{1175000, 1180000, 1185000, 1188000}
	for _, price := range prices {
		confidence := strategy.calculateConfidence(price, bb)
		if confidence < 0.5 || confidence > 1.0 {
			t.Errorf("Confidence for price %f should be between 0.5 and 1.0, got %f", price, confidence)
		}
	}
}

func TestDefaultMeanReversionConfig(t *testing.T) {
	config := DefaultMeanReversionConfig()

	if config.LookbackPeriod != 20 {
		t.Errorf("Expected LookbackPeriod 20, got %d", config.LookbackPeriod)
	}

	if config.EntryThreshold != 2.0 {
		t.Errorf("Expected EntryThreshold 2.0, got %f", config.EntryThreshold)
	}

	if config.ExitThreshold != 0.5 {
		t.Errorf("Expected ExitThreshold 0.5, got %f", config.ExitThreshold)
	}

	if config.MinSignalInterval != 60 {
		t.Errorf("Expected MinSignalInterval 60, got %d", config.MinSignalInterval)
	}
}
