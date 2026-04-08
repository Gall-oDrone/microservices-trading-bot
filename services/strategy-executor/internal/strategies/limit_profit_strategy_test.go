package strategies

import (
	"context"
	"testing"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

func TestLimitProfitStrategy_EntrySignal(t *testing.T) {
	s := NewLimitProfitStrategy()
	cfg := StrategyConfig{
		Name:    "lp_entry",
		Type:    "limit_profit",
		Book:    "btc_mxn",
		Enabled: true,
		Parameters: map[string]interface{}{
			"entry_offset":        float64(100),
			"min_profit":          float64(500),
			"min_signal_interval": float64(0),
			"position_size":       float64(0.001),
			"reference":           "last_trade",
		},
	}
	store := indicators.NewInMemoryIndicatorStore()
	prov := indicators.NewMockDataProvider()
	svc := indicators.NewService(nil, store, prov, nil)
	ctx := context.Background()
	if err := s.Initialize(cfg, svc); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}

	sig, err := s.OnTick(&indicators.Trade{Timestamp: time.Now(), Price: 1_000_000, Amount: 0.01, Side: "buy"})
	if err != nil {
		t.Fatal(err)
	}
	if sig == nil || sig.Side != "BUY" {
		t.Fatalf("expected BUY, got %+v", sig)
	}
	want := 1_000_100.0
	if sig.Price != want {
		t.Errorf("buy price: want %v got %v", want, sig.Price)
	}
	if !s.GetState().HasPosition {
		t.Error("expected position after entry")
	}
}

func TestLimitProfitStrategy_ExitSignal(t *testing.T) {
	s := NewLimitProfitStrategy()
	cfg := StrategyConfig{
		Name:    "lp_exit",
		Type:    "limit_profit",
		Book:    "btc_mxn",
		Enabled: true,
		Parameters: map[string]interface{}{
			"entry_offset":        float64(100),
			"min_profit":          float64(200),
			"min_signal_interval": float64(0),
			"position_size":       float64(0.001),
		},
	}
	store := indicators.NewInMemoryIndicatorStore()
	prov := indicators.NewMockDataProvider()
	svc := indicators.NewService(nil, store, prov, nil)
	ctx := context.Background()
	if err := s.Initialize(cfg, svc); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}

	_, _ = s.OnTick(&indicators.Trade{Price: 1_000_000})
	// entry 1_000_100, need price >= 1_000_300 for min_profit 200
	sig, err := s.OnTick(&indicators.Trade{Price: 1_000_400})
	if err != nil {
		t.Fatal(err)
	}
	if sig == nil || sig.Side != "SELL" {
		t.Fatalf("expected SELL, got %+v", sig)
	}
	if s.GetState().HasPosition {
		t.Error("position should clear after exit")
	}
}
