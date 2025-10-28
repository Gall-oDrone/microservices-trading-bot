package strategy

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

func TestNewSignal(t *testing.T) {
	signal := NewSignal(SignalBuy, "btc_mxn", 500000.0, 0.01)

	if signal.Type != SignalBuy {
		t.Errorf("Expected type %s, got %s", SignalBuy, signal.Type)
	}
	if signal.Book != "btc_mxn" {
		t.Errorf("Expected book 'btc_mxn', got '%s'", signal.Book)
	}
	if signal.Price != 500000.0 {
		t.Errorf("Expected price 500000.0, got %f", signal.Price)
	}
	if signal.Amount != 0.01 {
		t.Errorf("Expected amount 0.01, got %f", signal.Amount)
	}
}

func TestSignalBuilders(t *testing.T) {
	signal := NewSignal(SignalBuy, "btc_mxn", 500000.0, 0.01).
		WithReason("Test reason").
		WithConfidence(0.8).
		WithMetadata("key", "value")

	if signal.Reason != "Test reason" {
		t.Errorf("Expected reason 'Test reason', got '%s'", signal.Reason)
	}
	if signal.Confidence != 0.8 {
		t.Errorf("Expected confidence 0.8, got %f", signal.Confidence)
	}
	if signal.Metadata["key"] != "value" {
		t.Error("Metadata not set correctly")
	}
}

func TestSignalValidation(t *testing.T) {
	tests := []struct {
		name    string
		signal  *Signal
		wantErr bool
	}{
		{
			name:    "valid buy signal",
			signal:  NewSignal(SignalBuy, "btc_mxn", 500000.0, 0.01),
			wantErr: false,
		},
		{
			name:    "valid hold signal",
			signal:  NewSignal(SignalHold, "btc_mxn", 0, 0),
			wantErr: false,
		},
		{
			name:    "empty book",
			signal:  NewSignal(SignalBuy, "", 500000.0, 0.01),
			wantErr: true,
		},
		{
			name:    "zero price on buy signal",
			signal:  NewSignal(SignalBuy, "btc_mxn", 0, 0.01),
			wantErr: true,
		},
		{
			name:    "zero amount on buy signal",
			signal:  NewSignal(SignalBuy, "btc_mxn", 500000.0, 0),
			wantErr: true,
		},
		{
			name:    "invalid confidence",
			signal:  NewSignal(SignalBuy, "btc_mxn", 500000.0, 0.01).WithConfidence(1.5),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.signal.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBasicStrategy(t *testing.T) {
	params := map[string]interface{}{
		"rsi_period":     14.0,
		"rsi_oversold":   30.0,
		"rsi_overbought": 70.0,
	}

	strategy, err := NewBasicStrategy(params)
	if err != nil {
		t.Fatalf("NewBasicStrategy() error = %v", err)
	}

	if strategy.GetName() != "basic" {
		t.Errorf("Expected name 'basic', got '%s'", strategy.GetName())
	}

	// Test with ticker event
	ticker := &bitso.Ticker{
		Book:      *bitso.NewBook(bitso.BTC, bitso.MXN),
		Last:      "500000.0",
		Bid:       "499000.0",
		Ask:       "501000.0",
		CreatedAt: bitso.Time(time.Now()),
	}

	signal, err := strategy.OnTicker(ticker)
	if err != nil {
		t.Fatalf("OnTicker() error = %v", err)
	}

	// First signal should be HOLD (not enough data)
	if signal.Type != SignalHold {
		t.Errorf("Expected first signal to be HOLD, got %s", signal.Type)
	}
}

func TestBasicStrategyInvalidParams(t *testing.T) {
	tests := []struct {
		name    string
		params  map[string]interface{}
		wantErr bool
	}{
		{
			name: "valid params",
			params: map[string]interface{}{
				"rsi_period":     14.0,
				"rsi_oversold":   30.0,
				"rsi_overbought": 70.0,
			},
			wantErr: false,
		},
		{
			name: "invalid rsi_period",
			params: map[string]interface{}{
				"rsi_period":     1.0,
				"rsi_oversold":   30.0,
				"rsi_overbought": 70.0,
			},
			wantErr: true,
		},
		{
			name: "oversold >= overbought",
			params: map[string]interface{}{
				"rsi_period":     14.0,
				"rsi_oversold":   70.0,
				"rsi_overbought": 30.0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewBasicStrategy(tt.params)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewBasicStrategy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStrategyFactory(t *testing.T) {
	factory := NewStrategyFactory()

	// Test available strategies
	strategies := factory.GetAvailableStrategies()
	if len(strategies) == 0 {
		t.Error("Expected at least one strategy to be registered")
	}

	// Test IsStrategyAvailable
	if !factory.IsStrategyAvailable("basic") {
		t.Error("Expected 'basic' strategy to be available")
	}
	if factory.IsStrategyAvailable("nonexistent") {
		t.Error("Expected 'nonexistent' strategy to not be available")
	}

	// Test Create
	params := map[string]interface{}{
		"rsi_period":     14.0,
		"rsi_oversold":   30.0,
		"rsi_overbought": 70.0,
	}
	strategy, err := factory.Create("basic", params)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if strategy == nil {
		t.Error("Expected strategy to be created")
	}

	// Test unknown strategy
	_, err = factory.Create("unknown", params)
	if err == nil {
		t.Error("Expected error for unknown strategy")
	}
}

func TestStrategyExecutor(t *testing.T) {
	params := map[string]interface{}{
		"rsi_period":     14.0,
		"rsi_oversold":   30.0,
		"rsi_overbought": 70.0,
	}

	strategy, _ := NewBasicStrategy(params)
	executor := NewStrategyExecutor(strategy, nil)

	if executor.GetStrategyName() != "basic" {
		t.Errorf("Expected strategy name 'basic', got '%s'", executor.GetStrategyName())
	}

	// Test Reset
	if err := executor.Reset(); err != nil {
		t.Errorf("Reset() error = %v", err)
	}
}
