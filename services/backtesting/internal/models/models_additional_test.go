package models

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// TestBacktestDuration tests duration calculation
func TestBacktestDuration(t *testing.T) {
	config := NewBacktestConfig("Test", "btc_mxn", time.Now(), time.Now().Add(time.Hour))
	backtest := NewBacktest(config)

	// Not started
	if backtest.Duration() != 0 {
		t.Errorf("Expected duration 0 for non-started backtest, got %v", backtest.Duration())
	}

	// Started
	backtest.Start()
	time.Sleep(10 * time.Millisecond)
	duration := backtest.Duration()
	if duration < 10*time.Millisecond {
		t.Errorf("Expected duration >= 10ms, got %v", duration)
	}
}

// TestBacktestJSON tests JSON serialization
func TestBacktestJSON(t *testing.T) {
	config := NewBacktestConfig("Test", "btc_mxn", time.Now(), time.Now().Add(time.Hour))
	original := NewBacktest(config)
	original.Start()

	// To JSON
	data, err := original.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	// From JSON
	restored, err := BacktestFromJSON(data)
	if err != nil {
		t.Fatalf("BacktestFromJSON() error = %v", err)
	}

	if restored.ID != original.ID {
		t.Errorf("ID mismatch: expected %s, got %s", original.ID, restored.ID)
	}
	if restored.Status != original.Status {
		t.Errorf("Status mismatch: expected %s, got %s", original.Status, restored.Status)
	}
}

// TestBacktestConfigMethods tests config builder methods
func TestBacktestConfigMethods(t *testing.T) {
	config := NewBacktestConfig("Test", "btc_mxn", time.Now(), time.Now().Add(24*time.Hour))

	// Test GetDuration
	duration := config.GetDuration()
	expectedDuration := 24 * time.Hour
	// Allow for small differences due to time precision
	if duration < expectedDuration-time.Second || duration > expectedDuration+time.Second {
		t.Errorf("Expected duration ~24h, got %v", duration)
	}

	// Test GetDays
	days := config.GetDays()
	if days != 1 {
		t.Errorf("Expected 1 day, got %d", days)
	}

	// Test WithStrategy
	params := map[string]interface{}{"param1": 10}
	config.WithStrategy("trend", params)
	if config.Strategy != "trend" {
		t.Errorf("Expected strategy 'trend', got '%s'", config.Strategy)
	}

	// Test WithSlippage
	config.WithSlippage("fixed", 10.0)
	if config.SlippageModel != "fixed" {
		t.Errorf("Expected slippage model 'fixed', got '%s'", config.SlippageModel)
	}
	if config.SlippageValue != 10.0 {
		t.Errorf("Expected slippage value 10.0, got %f", config.SlippageValue)
	}

	// Test WithCommission
	config.WithCommission(0.002)
	if config.CommissionRate != 0.002 {
		t.Errorf("Expected commission rate 0.002, got %f", config.CommissionRate)
	}
}

// TestResultJSON tests result JSON serialization
func TestResultJSON(t *testing.T) {
	result := NewBacktestResult("bt-123", "cfg-456")
	result.SetSummary(&PerformanceSummary{
		TotalReturn: 5000,
		WinRate:     0.65,
	})

	// To JSON
	data, err := result.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	// From JSON
	restored, err := BacktestResultFromJSON(data)
	if err != nil {
		t.Fatalf("BacktestResultFromJSON() error = %v", err)
	}

	if restored.BacktestID != result.BacktestID {
		t.Errorf("BacktestID mismatch")
	}
	if restored.Summary.TotalReturn != 5000 {
		t.Errorf("Summary mismatch")
	}
}

// TestResultMarkMethods tests result marking methods
func TestResultMarkMethods(t *testing.T) {
	result := NewBacktestResult("bt-123", "cfg-456")

	// Test MarkFailed
	result.MarkFailed(testError("test error"))
	if result.Status != "failed" {
		t.Errorf("Expected status 'failed', got '%s'", result.Status)
	}
	if result.Error != "test error" {
		t.Errorf("Expected error 'test error', got '%s'", result.Error)
	}
	if result.CompletedAt == nil {
		t.Error("Expected CompletedAt to be set")
	}

	// Test MarkCompleted
	result2 := NewBacktestResult("bt-456", "cfg-789")
	result2.MarkCompleted()
	if result2.Status != "completed" {
		t.Errorf("Expected status 'completed', got '%s'", result2.Status)
	}
	if result2.Progress != 1.0 {
		t.Errorf("Expected progress 1.0, got %f", result2.Progress)
	}
}

// TestTradeMethods tests additional trade methods
func TestTradeMethods(t *testing.T) {
	now := time.Now()
	trade := NewTrade("buy", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
	trade.Close(510000.0, now.Add(time.Hour))

	// Test GetHoldingDuration
	duration := trade.GetHoldingDuration()
	if duration < time.Hour {
		t.Errorf("Expected holding duration >= 1h, got %v", duration)
	}

	// Test GetCostBasis
	costBasis := trade.GetCostBasis()
	expected := 500000.0 * 0.01
	if costBasis != expected {
		t.Errorf("Expected cost basis %f, got %f", expected, costBasis)
	}

	// Test GetProceeds
	proceeds := trade.GetProceeds()
	expectedProceeds := 510000.0 * 0.01
	if proceeds != expectedProceeds {
		t.Errorf("Expected proceeds %f, got %f", expectedProceeds, proceeds)
	}

	// Test GetGrossProfit
	grossProfit := trade.GetGrossProfit()
	if grossProfit <= 0 {
		t.Errorf("Expected positive gross profit, got %f", grossProfit)
	}

	// Test String method
	str := trade.String()
	if str == "" {
		t.Error("Expected non-empty string representation")
	}

	// Test IsBreakEven
	breakEvenTrade := NewTrade("buy", "btc_mxn", 500000.0, 0.01, 0.0, 0.0, now)
	breakEvenTrade.Close(500000.0, now.Add(time.Hour))
	if !breakEvenTrade.IsBreakEven() {
		t.Error("Expected trade to be break-even")
	}
}

// TestPositionMethods tests additional position methods
func TestPositionMethods(t *testing.T) {
	pos := NewPosition("btc_mxn")

	// Test GetValue
	pos.AddSize(0.01, 500000.0)
	pos.UpdateCurrentPrice(510000.0)
	value := pos.GetValue()
	expected := 510000.0 * 0.01
	if value != expected {
		t.Errorf("Expected value %f, got %f", expected, value)
	}

	// Test GetProfitLossPercent
	plPercent := pos.GetProfitLossPercent()
	if plPercent <= 0 {
		t.Errorf("Expected positive P&L percent, got %f", plPercent)
	}

	// Test Clone
	clone := pos.Clone()
	if clone.Book != pos.Book {
		t.Error("Clone failed: book mismatch")
	}
	if clone.Size != pos.Size {
		t.Error("Clone failed: size mismatch")
	}

	// Test Validate
	if err := pos.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}

	// Test invalid position
	invalidPos := NewPosition("")
	if err := invalidPos.Validate(); err == nil {
		t.Error("Expected error for empty book")
	}

	// Test String method
	str := pos.String()
	if str == "" {
		t.Error("Expected non-empty string representation")
	}
}

// TestEventMethods tests additional event methods
func TestEventMethods(t *testing.T) {
	now := time.Now()
	trade := &bitso.Trade{
		TID:       bitso.TID(12345),
		Book:      *bitso.NewBook(bitso.BTC, bitso.MXN),
		Amount:    "0.01",
		Price:     "500000.0",
		MakerSide: bitso.OrderSideBuy,
		CreatedAt: bitso.Time(now),
	}

	event := NewTradeEvent(trade)

	// Test GetAmount
	amount, err := event.GetAmount()
	if err != nil {
		t.Errorf("GetAmount() error = %v", err)
	}
	if amount != 0.01 {
		t.Errorf("Expected amount 0.01, got %f", amount)
	}

	// Test GetSide
	side, err := event.GetSide()
	if err != nil {
		t.Errorf("GetSide() error = %v", err)
	}
	if side != "buy" {
		t.Errorf("Expected side 'buy', got '%s'", side)
	}

	// Test Validate
	if err := event.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}

	// Test String method
	str := event.String()
	if str == "" {
		t.Error("Expected non-empty string representation")
	}

	// Test Compare
	event2 := NewTradeEvent(trade)
	event2.Timestamp = now.Add(time.Hour)
	if event.Compare(event2) != -1 {
		t.Error("Expected event to be before event2")
	}
}

// TestOrderBookEvent tests order book events
func TestOrderBookEvent(t *testing.T) {
	event := NewOrderBookEvent("btc_mxn", time.Now(), map[string]interface{}{"data": "test"})

	if !event.IsOrderBookEvent() {
		t.Error("Expected order book event")
	}

	data, err := event.GetOrderBook()
	if err != nil {
		t.Errorf("GetOrderBook() error = %v", err)
	}
	if data == nil {
		t.Error("Expected order book data")
	}
}

// TestValidationDataSource tests data source validation
func TestValidationDataSource(t *testing.T) {
	validSources := []string{"market-data", "file", "csv"}
	for _, source := range validSources {
		if err := ValidateDataSource(source); err != nil {
			t.Errorf("ValidateDataSource(%s) error = %v", source, err)
		}
	}

	if err := ValidateDataSource("invalid"); err == nil {
		t.Error("Expected error for invalid data source")
	}
}

// TestValidationGranularity tests granularity validation
func TestValidationGranularity(t *testing.T) {
	validGranularities := []string{"tick", "1m", "5m", "15m", "1h", "1d"}
	for _, gran := range validGranularities {
		if err := ValidateGranularity(gran); err != nil {
			t.Errorf("ValidateGranularity(%s) error = %v", gran, err)
		}
	}

	if err := ValidateGranularity("invalid"); err == nil {
		t.Error("Expected error for invalid granularity")
	}
}

// TestValidationSlippageModel tests slippage model validation
func TestValidationSlippageModel(t *testing.T) {
	validModels := []string{"none", "fixed", "percentage", "volume"}
	for _, model := range validModels {
		if err := ValidateSlippageModel(model); err != nil {
			t.Errorf("ValidateSlippageModel(%s) error = %v", model, err)
		}
	}

	if err := ValidateSlippageModel("invalid"); err == nil {
		t.Error("Expected error for invalid slippage model")
	}
}

// TestValidationCommissionRate tests commission rate validation
func TestValidationCommissionRate(t *testing.T) {
	// Valid rates
	validRates := []float64{0.0, 0.001, 0.01, 0.05}
	for _, rate := range validRates {
		if err := ValidateCommissionRate(rate); err != nil {
			t.Errorf("ValidateCommissionRate(%f) error = %v", rate, err)
		}
	}

	// Invalid: negative
	if err := ValidateCommissionRate(-0.01); err == nil {
		t.Error("Expected error for negative commission rate")
	}

	// Invalid: too high
	if err := ValidateCommissionRate(0.15); err == nil {
		t.Error("Expected error for commission rate > 10%")
	}
}

// TestPositionMutations tests position edge cases
func TestPositionMutations(t *testing.T) {
	pos := NewPosition("btc_mxn")

	// Test AddSize with zero amount
	initialSize := pos.Size
	pos.AddSize(0, 500000.0)
	if pos.Size != initialSize {
		t.Error("AddSize with 0 should not change size")
	}

	// Test ReduceSize with zero amount
	pos.AddSize(0.01, 500000.0)
	realizedPL := pos.ReduceSize(0, 500000.0)
	if realizedPL != 0 {
		t.Error("ReduceSize with 0 should return 0 realized P&L")
	}

	// Test ReduceSize more than available
	pos.ReduceSize(1.0, 500000.0) // Much more than available
	if !pos.IsEmpty() {
		t.Error("ReduceSize with amount > size should close position")
	}

	// Test negative position values
	negPos := &Position{
		Book:         "btc_mxn",
		AveragePrice: -100,
		CurrentPrice: -50,
		CostBasis:    -1000,
	}
	if err := negPos.Validate(); err == nil {
		t.Error("Expected error for negative values")
	}
}

// TestEventEdgeCases tests event edge cases
func TestEventEdgeCases(t *testing.T) {
	// Test invalid event validation
	invalidEvent := &MarketEvent{
		EventType: EventTypeTrade,
		Book:      "",
		Timestamp: time.Now(),
	}
	if err := invalidEvent.Validate(); err == nil {
		t.Error("Expected error for empty book")
	}

	// Test event with zero timestamp
	invalidEvent2 := &MarketEvent{
		EventType: EventTypeTrade,
		Book:      "btc_mxn",
		Timestamp: time.Time{},
	}
	if err := invalidEvent2.Validate(); err == nil {
		t.Error("Expected error for zero timestamp")
	}

	// Test event without data
	invalidEvent3 := &MarketEvent{
		EventType: EventTypeTrade,
		Book:      "btc_mxn",
		Timestamp: time.Now(),
		Data:      nil,
	}
	if err := invalidEvent3.Validate(); err == nil {
		t.Error("Expected error for nil data")
	}

	// Test GetPrice from ticker
	ticker := &bitso.Ticker{
		Book:      *bitso.NewBook(bitso.BTC, bitso.MXN),
		Last:      "500000.0",
		CreatedAt: bitso.Time(time.Now()),
	}
	tickerEvent := NewTickerEvent(ticker)
	price, err := tickerEvent.GetPrice()
	if err != nil {
		t.Errorf("GetPrice() error = %v", err)
	}
	if price != 500000.0 {
		t.Errorf("Expected price 500000.0, got %f", price)
	}

	// Test GetAmount on non-trade event
	_, err = tickerEvent.GetAmount()
	if err == nil {
		t.Error("Expected error when getting amount from ticker event")
	}

	// Test GetSide on non-trade event
	_, err = tickerEvent.GetSide()
	if err == nil {
		t.Error("Expected error when getting side from ticker event")
	}

	// Test GetPrice on order book event
	obEvent := NewOrderBookEvent("btc_mxn", time.Now(), map[string]interface{}{})
	_, err = obEvent.GetPrice()
	if err == nil {
		t.Error("Expected error when getting price from order book event")
	}
}

// TestTradeEdgeCases tests trade edge cases
func TestTradeEdgeCases(t *testing.T) {
	now := time.Now()

	// Test trade with zero amounts
	trade := NewTrade("buy", "btc_mxn", 0, 0, 0, 0, now)
	if err := trade.Validate(); err == nil {
		t.Error("Expected error for zero price")
	}

	// Test short trade calculation
	shortTrade := NewTrade("sell", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
	shortTrade.Close(490000.0, now.Add(time.Hour))

	if !shortTrade.IsWinning() {
		t.Error("Expected short trade to be winning (sold high, bought back low)")
	}

	// Test PL calculation with zero entry price
	zeroTrade := NewTrade("buy", "btc_mxn", 0, 0.01, 0, 0, now)
	zeroTrade.ExitPrice = 500000.0
	zeroTrade.CalculatePL()
	if zeroTrade.ProfitLossPercent != 0 {
		t.Errorf("Expected PL percent to be 0 with zero entry price, got %f", zeroTrade.ProfitLossPercent)
	}
}
