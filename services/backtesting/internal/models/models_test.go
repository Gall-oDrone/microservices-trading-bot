package models

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// TestBacktestLifecycle tests the complete lifecycle of a backtest
func TestBacktestLifecycle(t *testing.T) {
	config := NewBacktestConfig("Test Backtest", "btc_mxn", 
		time.Now().AddDate(0, -1, 0), time.Now())
	config.WithInitialBalance(100000.0)
	
	backtest := NewBacktest(config)
	
	// Initial state
	if backtest.Status != BacktestStatusPending {
		t.Errorf("Expected status %s, got %s", BacktestStatusPending, backtest.Status)
	}
	if backtest.Progress != 0.0 {
		t.Errorf("Expected progress 0.0, got %f", backtest.Progress)
	}
	
	// Start backtest
	backtest.Start()
	if backtest.Status != BacktestStatusRunning {
		t.Errorf("Expected status %s, got %s", BacktestStatusRunning, backtest.Status)
	}
	if backtest.StartedAt == nil {
		t.Error("Expected StartedAt to be set")
	}
	
	// Update progress
	backtest.UpdateProgress(0.5)
	if backtest.Progress != 0.5 {
		t.Errorf("Expected progress 0.5, got %f", backtest.Progress)
	}
	
	// Complete backtest
	result := NewBacktestResult(backtest.ID, config.ID)
	result.SetSummary(&PerformanceSummary{
		TotalReturn: 5000,
		WinRate:     0.65,
	})
	
	backtest.Complete(result)
	if backtest.Status != BacktestStatusCompleted {
		t.Errorf("Expected status %s, got %s", BacktestStatusCompleted, backtest.Status)
	}
	if backtest.Progress != 1.0 {
		t.Errorf("Expected progress 1.0, got %f", backtest.Progress)
	}
	if backtest.CompletedAt == nil {
		t.Error("Expected CompletedAt to be set")
	}
	
	// Test IsActive and IsCompleted
	if backtest.IsActive() {
		t.Error("Expected backtest to not be active")
	}
	if !backtest.IsCompleted() {
		t.Error("Expected backtest to be completed")
	}
}

func TestBacktestStatusTransitions(t *testing.T) {
	config := NewBacktestConfig("Test", "btc_mxn", time.Now(), time.Now().Add(time.Hour))
	backtest := NewBacktest(config)
	
	// Test Cancel
	backtest.Start()
	backtest.Cancel()
	if backtest.Status != BacktestStatusCancelled {
		t.Errorf("Expected status %s, got %s", BacktestStatusCancelled, backtest.Status)
	}
	
	// Test Fail
	backtest2 := NewBacktest(config)
	backtest2.Start()
	err := testError("test error")
	backtest2.Fail(err)
	if backtest2.Status != BacktestStatusFailed {
		t.Errorf("Expected status %s, got %s", BacktestStatusFailed, backtest2.Status)
	}
	if backtest2.Error != "test error" {
		t.Errorf("Expected error 'test error', got '%s'", backtest2.Error)
	}
}

// TestBacktestConfigValidation tests configuration validation
func TestBacktestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  func() *BacktestConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: func() *BacktestConfig {
				return NewBacktestConfig("Test", "btc_mxn",
					time.Now().AddDate(0, -1, 0), time.Now())
			},
			wantErr: false,
		},
		{
			name: "empty name",
			config: func() *BacktestConfig {
				cfg := NewBacktestConfig("", "btc_mxn",
					time.Now().AddDate(0, -1, 0), time.Now())
				return cfg
			},
			wantErr: true,
		},
		{
			name: "empty book",
			config: func() *BacktestConfig {
				cfg := NewBacktestConfig("Test", "",
					time.Now().AddDate(0, -1, 0), time.Now())
				return cfg
			},
			wantErr: true,
		},
		{
			name: "end before start",
			config: func() *BacktestConfig {
				cfg := NewBacktestConfig("Test", "btc_mxn",
					time.Now(), time.Now().AddDate(0, -1, 0))
				return cfg
			},
			wantErr: true,
		},
		{
			name: "negative balance",
			config: func() *BacktestConfig {
				cfg := NewBacktestConfig("Test", "btc_mxn",
					time.Now().AddDate(0, -1, 0), time.Now())
				cfg.InitialBalance = -1000
				return cfg
			},
			wantErr: true,
		},
		{
			name: "invalid slippage model",
			config: func() *BacktestConfig {
				cfg := NewBacktestConfig("Test", "btc_mxn",
					time.Now().AddDate(0, -1, 0), time.Now())
				cfg.SlippageModel = "invalid"
				return cfg
			},
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.config()
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBacktestConfigClone(t *testing.T) {
	original := NewBacktestConfig("Test", "btc_mxn",
		time.Now().AddDate(0, -1, 0), time.Now())
	original.StrategyParams = map[string]interface{}{
		"param1": 10,
		"param2": "value",
	}
	
	clone := original.Clone()
	
	// Modify clone
	clone.Name = "Modified"
	clone.StrategyParams["param1"] = 20
	
	// Original should not be affected
	if original.Name == "Modified" {
		t.Error("Clone modified original name")
	}
	if original.StrategyParams["param1"] == 20 {
		t.Error("Clone modified original strategy params")
	}
}

// TestTradeCalculations tests trade P&L calculations
func TestTradeCalculations(t *testing.T) {
	now := time.Now()
	
	// Test long trade (buy)
	longTrade := NewTrade("buy", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
	longTrade.Close(510000.0, now.Add(time.Hour))
	
	expectedPL := (510000.0 - 500000.0) * 0.01 - 50.0 // (exit - entry) * amount - commission
	if longTrade.ProfitLoss != expectedPL {
		t.Errorf("Expected P&L %f, got %f", expectedPL, longTrade.ProfitLoss)
	}
	
	if !longTrade.IsWinning() {
		t.Error("Expected trade to be winning")
	}
	
	// Test short trade (sell)
	shortTrade := NewTrade("sell", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
	shortTrade.Close(490000.0, now.Add(time.Hour))
	
	expectedPL = (500000.0 - 490000.0) * 0.01 - 50.0 // (entry - exit) * amount - commission
	if shortTrade.ProfitLoss != expectedPL {
		t.Errorf("Expected P&L %f, got %f", expectedPL, shortTrade.ProfitLoss)
	}
	
	if !shortTrade.IsWinning() {
		t.Error("Expected trade to be winning")
	}
	
	// Test losing trade
	losingTrade := NewTrade("buy", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
	losingTrade.Close(490000.0, now.Add(time.Hour))
	
	if !losingTrade.IsLosing() {
		t.Error("Expected trade to be losing")
	}
}

func TestTradeValidation(t *testing.T) {
	now := time.Now()
	
	tests := []struct {
		name    string
		trade   *Trade
		wantErr bool
	}{
		{
			name: "valid trade",
			trade: func() *Trade {
				trade := NewTrade("buy", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
				trade.Close(510000.0, now.Add(time.Hour))
				return trade
			}(),
			wantErr: false,
		},
		{
			name: "invalid side",
			trade: func() *Trade {
				trade := NewTrade("invalid", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
				trade.Close(510000.0, now.Add(time.Hour))
				return trade
			}(),
			wantErr: true,
		},
		{
			name: "negative price",
			trade: func() *Trade {
				trade := NewTrade("buy", "btc_mxn", -500000.0, 0.01, 50.0, 5.0, now)
				trade.Close(510000.0, now.Add(time.Hour))
				return trade
			}(),
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.trade.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestPositionTracking tests position management
func TestPositionTracking(t *testing.T) {
	pos := NewPosition("btc_mxn")
	
	// Test empty position
	if !pos.IsEmpty() {
		t.Error("Expected position to be empty")
	}
	
	// Add to position (buy)
	pos.AddSize(0.01, 500000.0)
	if pos.Size != 0.01 {
		t.Errorf("Expected size 0.01, got %f", pos.Size)
	}
	if pos.AveragePrice != 500000.0 {
		t.Errorf("Expected avg price 500000.0, got %f", pos.AveragePrice)
	}
	if !pos.IsLong() {
		t.Error("Expected position to be long")
	}
	
	// Add more to position (average price calculation)
	pos.AddSize(0.01, 510000.0)
	expectedAvg := (500000.0 + 510000.0) / 2
	if pos.Size != 0.02 {
		t.Errorf("Expected size 0.02, got %f", pos.Size)
	}
	if pos.AveragePrice != expectedAvg {
		t.Errorf("Expected avg price %f, got %f", expectedAvg, pos.AveragePrice)
	}
	
	// Update current price and calculate unrealized P&L
	pos.UpdateCurrentPrice(520000.0)
	expectedPL := (520000.0 - pos.AveragePrice) * pos.Size
	if pos.UnrealizedPL != expectedPL {
		t.Errorf("Expected unrealized P&L %f, got %f", expectedPL, pos.UnrealizedPL)
	}
	
	// Reduce position
	realizedPL := pos.ReduceSize(0.01, 520000.0)
	if pos.Size != 0.01 {
		t.Errorf("Expected size 0.01 after reduce, got %f", pos.Size)
	}
	if realizedPL <= 0 {
		t.Errorf("Expected positive realized P&L, got %f", realizedPL)
	}
	
	// Close position completely
	pos.ReduceSize(0.01, 520000.0)
	if !pos.IsEmpty() {
		t.Error("Expected position to be empty after full close")
	}
}

// TestMarketEventCreation tests market event creation and type checking
func TestMarketEventCreation(t *testing.T) {
	// Create trade event
	now := time.Now()
	trade := &bitso.Trade{
		TID:       bitso.TID(12345),
		Book:      *bitso.NewBook(bitso.BTC, bitso.MXN),
		Amount:    "0.01",
		Price:     "500000.0",
		MakerSide: bitso.OrderSideBuy,
		CreatedAt: bitso.Time(now),
	}
	
	tradeEvent := NewTradeEvent(trade)
	if !tradeEvent.IsTradeEvent() {
		t.Error("Expected trade event")
	}
	if tradeEvent.IsTickerEvent() {
		t.Error("Did not expect ticker event")
	}
	
	retrievedTrade, err := tradeEvent.GetTrade()
	if err != nil {
		t.Errorf("GetTrade() error = %v", err)
	}
	if retrievedTrade.TID != trade.TID {
		t.Error("Trade ID mismatch")
	}
	
	// Test error on wrong type
	_, err = tradeEvent.GetTicker()
	if err == nil {
		t.Error("Expected error when getting ticker from trade event")
	}
	
	// Create ticker event
	ticker := &bitso.Ticker{
		Book:      *bitso.NewBook(bitso.BTC, bitso.MXN),
		Last:      "500000.0",
		Bid:       "499000.0",
		Ask:       "501000.0",
		CreatedAt: bitso.Time(time.Now()),
	}
	
	tickerEvent := NewTickerEvent(ticker)
	if !tickerEvent.IsTickerEvent() {
		t.Error("Expected ticker event")
	}
	
	price, err := tickerEvent.GetPrice()
	if err != nil {
		t.Errorf("GetPrice() error = %v", err)
	}
	if price != 500000.0 {
		t.Errorf("Expected price 500000.0, got %f", price)
	}
}

// TestValidationFunctions tests various validation functions
func TestValidationFunctions(t *testing.T) {
	// Test ValidateTimeRange
	now := time.Now()
	pastDate := now.AddDate(0, -1, 0)
	
	if err := ValidateTimeRange(pastDate, now); err != nil {
		t.Errorf("ValidateTimeRange() error = %v", err)
	}
	
	// Test invalid range (end before start)
	if err := ValidateTimeRange(now, pastDate); err == nil {
		t.Error("Expected error for invalid time range")
	}
	
	// Test ValidateBook
	validBooks := []string{"btc_mxn", "eth_mxn", "btc_usd"}
	for _, book := range validBooks {
		if err := ValidateBook(book); err != nil {
			t.Errorf("ValidateBook(%s) error = %v", book, err)
		}
	}
	
	// Test invalid book
	if err := ValidateBook("invalid"); err == nil {
		t.Error("Expected error for invalid book")
	}
	
	// Test ValidateBalance
	if err := ValidateBalance(100000.0); err != nil {
		t.Errorf("ValidateBalance() error = %v", err)
	}
	
	if err := ValidateBalance(-1000.0); err == nil {
		t.Error("Expected error for negative balance")
	}
	
	if err := ValidateBalance(50.0); err == nil {
		t.Error("Expected error for balance too small")
	}
	
	// Test ValidateStrategy
	params := map[string]interface{}{
		"rsi_period":     14.0,
		"rsi_oversold":   30.0,
		"rsi_overbought": 70.0,
	}
	if err := ValidateStrategy("basic", params); err != nil {
		t.Errorf("ValidateStrategy() error = %v", err)
	}
	
	// Test unknown strategy
	if err := ValidateStrategy("unknown", params); err == nil {
		t.Error("Expected error for unknown strategy")
	}
}

func TestBacktestResultMethods(t *testing.T) {
	result := NewBacktestResult("bt-123", "cfg-456")
	
	// Add trades
	now := time.Now()
	winningTrade := NewTrade("buy", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
	winningTrade.Close(510000.0, now.Add(time.Hour))
	
	losingTrade := NewTrade("buy", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
	losingTrade.Close(490000.0, now.Add(time.Hour))
	
	result.AddTrade(*winningTrade)
	result.AddTrade(*losingTrade)
	
	if result.GetTradeCount() != 2 {
		t.Errorf("Expected 2 trades, got %d", result.GetTradeCount())
	}
	
	winningTrades := result.GetWinningTrades()
	if len(winningTrades) != 1 {
		t.Errorf("Expected 1 winning trade, got %d", len(winningTrades))
	}
	
	losingTrades := result.GetLosingTrades()
	if len(losingTrades) != 1 {
		t.Errorf("Expected 1 losing trade, got %d", len(losingTrades))
	}
	
	// Add equity points
	result.AddEquityPoint(EquityPoint{
		Timestamp: now,
		Balance:   100000.0,
		Equity:    100000.0,
		Return:    0.0,
	})
	
	if len(result.EquityCurve) != 1 {
		t.Errorf("Expected 1 equity point, got %d", len(result.EquityCurve))
	}
	
	// Mark completed
	result.MarkCompleted()
	if result.Status != "completed" {
		t.Errorf("Expected status 'completed', got '%s'", result.Status)
	}
	if result.CompletedAt == nil {
		t.Error("Expected CompletedAt to be set")
	}
}

// Helper function for creating test errors
type testError string

func (e testError) Error() string {
	return string(e)
}

