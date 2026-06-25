package backtest

import (
	"context"
	"math"
	"testing"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// syntheticTrades builds an oscillating price series dense enough to warm the
// default bar-first indicators (1m bars, ~20-26 bar lookback).
func syntheticTrades(n int) []indicators.Trade {
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	trades := make([]indicators.Trade, n)
	for i := 0; i < n; i++ {
		price := 1000000.0 + 20000.0*math.Sin(float64(i)/15.0)
		trades[i] = indicators.Trade{
			Timestamp: base.Add(time.Duration(i) * 20 * time.Second),
			Price:     price,
			Amount:    0.01,
			Side:      "buy",
		}
	}
	return trades
}

func TestRunHistorical_MeanReversionPipeline(t *testing.T) {
	trades := syntheticTrades(600) // ~200 minutes of 1m bars

	result, err := RunHistorical(context.Background(), trades, EngineConfig{
		Book:           "btc_mxn",
		StrategyType:   "mean_reversion",
		InitialBalance: 100000,
	})
	if err != nil {
		t.Fatalf("RunHistorical error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.TicksProcessed != len(trades) {
		t.Errorf("TicksProcessed = %d, want %d", result.TicksProcessed, len(trades))
	}
	if result.Book != "btc_mxn" {
		t.Errorf("Book = %q, want btc_mxn", result.Book)
	}
	if result.StrategyName == "" {
		t.Error("expected a derived strategy name")
	}
}

func TestRunHistorical_UnsupportedType(t *testing.T) {
	_, err := RunHistorical(context.Background(), syntheticTrades(10), EngineConfig{
		Book:         "btc_mxn",
		StrategyType: "does_not_exist",
	})
	if err == nil {
		t.Fatal("expected error for unsupported strategy type")
	}
}

func TestRunHistorical_NoTrades(t *testing.T) {
	_, err := RunHistorical(context.Background(), nil, EngineConfig{
		Book:         "btc_mxn",
		StrategyType: "momentum",
	})
	if err == nil {
		t.Fatal("expected error for empty trade set")
	}
}

func TestReplayProvider_IncrementalBars(t *testing.T) {
	p := NewReplayProvider()
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	// Two trades in minute 0, one in minute 1.
	p.Observe(indicators.Trade{Timestamp: base, Price: 100, Amount: 1})
	p.Observe(indicators.Trade{Timestamp: base.Add(30 * time.Second), Price: 110, Amount: 1})
	p.Observe(indicators.Trade{Timestamp: base.Add(70 * time.Second), Price: 90, Amount: 1})

	bars, _ := p.GetRecentBars(context.Background(), "btc_mxn", "1m", 0)
	if len(bars) != 2 {
		t.Fatalf("expected 2 bars, got %d", len(bars))
	}
	if bars[0].Open != 100 || bars[0].High != 110 || bars[0].Close != 110 || bars[0].Volume != 2 {
		t.Errorf("bar0 unexpected: %+v", bars[0])
	}
	if bars[1].Open != 90 || bars[1].Close != 90 {
		t.Errorf("bar1 unexpected: %+v", bars[1])
	}
}
