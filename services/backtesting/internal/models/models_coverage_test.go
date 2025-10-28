package models

import (
	"testing"
	"time"
)

// TestUncoveredMethods tests remaining uncovered methods to reach >80% coverage

func TestBacktestConfigBuilders(t *testing.T) {
	config := NewBacktestConfig("Test", "btc_mxn", time.Now(), time.Now().Add(time.Hour))
	
	// Test all builder methods
	config.WithStrategy("trend", map[string]interface{}{"ma": 20})
	if config.Strategy != "trend" {
		t.Error("WithStrategy failed")
	}
	
	config.WithSlippage("fixed", 5.0)
	if config.SlippageModel != "fixed" || config.SlippageValue != 5.0 {
		t.Error("WithSlippage failed")
	}
	
	config.WithCommission(0.002)
	if config.CommissionRate != 0.002 {
		t.Error("WithCommission failed")
	}
	
	// Test GetDuration and GetDays
	_ = config.GetDuration()
	_ = config.GetDays()
}

func TestBacktestProgressBoundaries(t *testing.T) {
	config := NewBacktestConfig("Test", "btc_mxn", time.Now(), time.Now().Add(time.Hour))
	backtest := NewBacktest(config)
	
	// Test negative progress (should be clamped to 0)
	backtest.UpdateProgress(-0.5)
	if backtest.Progress != 0 {
		t.Errorf("Expected progress 0, got %f", backtest.Progress)
	}
	
	// Test progress > 1.0 (should be clamped to 1.0)
	backtest.UpdateProgress(1.5)
	if backtest.Progress != 1.0 {
		t.Errorf("Expected progress 1.0, got %f", backtest.Progress)
	}
}

func TestStrategyValidation(t *testing.T) {
	// Test trend strategy params
	trendParams := map[string]interface{}{
		"short_ma": 10.0,
		"long_ma":  20.0,
	}
	if err := ValidateStrategy("trend", trendParams); err != nil {
		t.Errorf("ValidateStrategy(trend) error = %v", err)
	}
	
	// Test arbitrage strategy params
	arbParams := map[string]interface{}{
		"min_spread": 0.01,
	}
	if err := ValidateStrategy("arbitrage", arbParams); err != nil {
		t.Errorf("ValidateStrategy(arbitrage) error = %v", err)
	}
	
	// Test mean_reversion strategy params
	mrParams := map[string]interface{}{
		"period":  20.0,
		"std_dev": 2.0,
	}
	if err := ValidateStrategy("mean_reversion", mrParams); err != nil {
		t.Errorf("ValidateStrategy(mean_reversion) error = %v", err)
	}
	
	// Test nil params
	if err := ValidateStrategy("basic", nil); err == nil {
		t.Error("Expected error for nil params")
	}
	
	// Test empty strategy name
	if err := ValidateStrategy("", map[string]interface{}{}); err == nil {
		t.Error("Expected error for empty strategy name")
	}
}

func TestTimeRangeValidation(t *testing.T) {
	now := time.Now()
	past := now.AddDate(0, -1, 0)
	
	// Valid range
	if err := ValidateTimeRange(past, now); err != nil {
		t.Errorf("ValidateTimeRange() error = %v", err)
	}
	
	// Zero start date
	if err := ValidateTimeRange(time.Time{}, now); err == nil {
		t.Error("Expected error for zero start date")
	}
	
	// Zero end date
	if err := ValidateTimeRange(past, time.Time{}); err == nil {
		t.Error("Expected error for zero end date")
	}
	
	// Future start date
	future := now.Add(24 * time.Hour)
	if err := ValidateTimeRange(future, future.Add(time.Hour)); err == nil {
		t.Error("Expected error for future start date")
	}
	
	// Too long range (> 5 years)
	veryPast := now.AddDate(-6, 0, 0)
	if err := ValidateTimeRange(veryPast, now); err == nil {
		t.Error("Expected error for range > 5 years")
	}
}

func TestPositionIsShort(t *testing.T) {
	pos := NewPosition("btc_mxn")
	
	// Test short position (negative size)
	pos.Size = -0.01
	if !pos.IsShort() {
		t.Error("Expected position to be short")
	}
	if pos.IsLong() {
		t.Error("Did not expect position to be long")
	}
}

func TestEventCompare(t *testing.T) {
	now := time.Now()
	event1 := &MarketEvent{
		EventType: EventTypeTrade,
		Timestamp: now,
		Book:      "btc_mxn",
	}
	event2 := &MarketEvent{
		EventType: EventTypeTrade,
		Timestamp: now.Add(time.Hour),
		Book:      "btc_mxn",
	}
	event3 := &MarketEvent{
		EventType: EventTypeTrade,
		Timestamp: now,
		Book:      "btc_mxn",
	}
	
	if event1.Compare(event2) != -1 {
		t.Error("Expected event1 < event2")
	}
	if event2.Compare(event1) != 1 {
		t.Error("Expected event2 > event1")
	}
	if event1.Compare(event3) != 0 {
		t.Error("Expected event1 == event3")
	}
}

