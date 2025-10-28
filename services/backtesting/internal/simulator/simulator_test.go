package simulator

import (
	"context"
	"testing"
	"time"

	"bitso-trading-platform/backtesting/internal/models"
	sharedModels "bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/shared/pkg/bitso"
)

func TestNewSimulator(t *testing.T) {
	config := &SimulatorConfig{
		SlippageModel:  "percentage",
		SlippageValue:  0.001,
		CommissionRate: 0.001,
	}
	
	sim := NewSimulator(config, nil)
	if sim == nil {
		t.Fatal("NewSimulator returned nil")
	}
	
	if sim.config.SlippageModel != "percentage" {
		t.Errorf("Expected slippage model 'percentage', got '%s'", sim.config.SlippageModel)
	}
}

func TestSimulatorProcessEvent(t *testing.T) {
	config := &SimulatorConfig{
		SlippageModel:  "none",
		SlippageValue:  0,
		CommissionRate: 0.001,
	}
	
	sim := NewSimulator(config, nil)
	ctx := context.Background()
	sim.Initialize(ctx, config)
	
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
	
	event := models.NewTradeEvent(trade)
	
	// Process event
	err := sim.ProcessEvent(event)
	if err != nil {
		t.Fatalf("ProcessEvent() error = %v", err)
	}
	
	// Check price updated
	price, err := sim.GetCurrentPrice("btc_mxn")
	if err != nil {
		t.Fatalf("GetCurrentPrice() error = %v", err)
	}
	
	if price != 500000.0 {
		t.Errorf("Expected price 500000.0, got %f", price)
	}
}

func TestExecuteOrder(t *testing.T) {
	config := &SimulatorConfig{
		SlippageModel:  "percentage",
		SlippageValue:  0.001,
		CommissionRate: 0.001,
	}
	
	sim := NewSimulator(config, nil)
	
	// Set a current price
	sim.currentPrices["btc_mxn"] = 500000.0
	sim.lastUpdate = time.Now()
	
	// Create order
	order := &sharedModels.Order{
		ID:     "order-1",
		Symbol: "btc_mxn",
		Side:   "buy",
		Amount: 0.01,
		Price:  500000.0,
	}
	
	// Execute order
	execution, err := sim.ExecuteOrder(order)
	if err != nil {
		t.Fatalf("ExecuteOrder() error = %v", err)
	}
	
	if !execution.Success {
		t.Error("Expected successful execution")
	}
	
	// Price should be slightly worse due to slippage
	if execution.ExecutedPrice <= 500000.0 {
		t.Errorf("Expected execution price > 500000.0 (due to slippage), got %f", execution.ExecutedPrice)
	}
	
	// Commission should be calculated
	if execution.Commission <= 0 {
		t.Error("Expected positive commission")
	}
}

func TestSlippageModels(t *testing.T) {
	order := &sharedModels.Order{
		ID:     "order-1",
		Symbol: "btc_mxn",
		Side:   "buy",
		Amount: 0.01,
		Price:  500000.0,
	}
	marketPrice := 500000.0
	
	// Test NoSlippage
	noSlip := &NoSlippage{}
	if slippage := noSlip.Calculate(order, marketPrice); slippage != 0 {
		t.Errorf("NoSlippage should return 0, got %f", slippage)
	}
	
	// Test FixedSlippage
	fixedSlip := &FixedSlippage{Value: 100.0}
	if slippage := fixedSlip.Calculate(order, marketPrice); slippage != 100.0 {
		t.Errorf("FixedSlippage should return 100.0, got %f", slippage)
	}
	
	// Test PercentageSlippage
	percSlip := &PercentageSlippage{Percentage: 0.001}
	expectedSlippage := 500000.0 * 0.001
	if slippage := percSlip.Calculate(order, marketPrice); slippage != expectedSlippage {
		t.Errorf("PercentageSlippage should return %f, got %f", expectedSlippage, slippage)
	}
	
	// Test VolumeBasedSlippage
	volSlip := &VolumeBasedSlippage{BasePercentage: 0.001}
	slippage := volSlip.Calculate(order, marketPrice)
	if slippage <= 0 {
		t.Error("VolumeBasedSlippage should return positive slippage")
	}
}

func TestOrderBook(t *testing.T) {
	ob := NewOrderBook("btc_mxn")
	
	// Update with test data
	bids := []PriceLevel{
		{Price: 499000.0, Amount: 0.1},
		{Price: 498000.0, Amount: 0.2},
	}
	asks := []PriceLevel{
		{Price: 501000.0, Amount: 0.1},
		{Price: 502000.0, Amount: 0.2},
	}
	
	ob.Update(bids, asks, time.Now())
	
	// Test GetBestBid
	bidPrice, bidAmount, err := ob.GetBestBid()
	if err != nil {
		t.Fatalf("GetBestBid() error = %v", err)
	}
	if bidPrice != 499000.0 {
		t.Errorf("Expected best bid price 499000.0, got %f", bidPrice)
	}
	if bidAmount != 0.1 {
		t.Errorf("Expected best bid amount 0.1, got %f", bidAmount)
	}
	
	// Test GetBestAsk
	askPrice, askAmount, err := ob.GetBestAsk()
	if err != nil {
		t.Fatalf("GetBestAsk() error = %v", err)
	}
	if askPrice != 501000.0 {
		t.Errorf("Expected best ask price 501000.0, got %f", askPrice)
	}
	if askAmount != 0.1 {
		t.Errorf("Expected best ask amount 0.1, got %f", askAmount)
	}
	
	// Test GetMidPrice
	midPrice, err := ob.GetMidPrice()
	if err != nil {
		t.Fatalf("GetMidPrice() error = %v", err)
	}
	expected := (499000.0 + 501000.0) / 2
	if midPrice != expected {
		t.Errorf("Expected mid price %f, got %f", expected, midPrice)
	}
	
	// Test GetSpread
	spread := ob.GetSpread()
	expectedSpread := 501000.0 - 499000.0
	if spread != expectedSpread {
		t.Errorf("Expected spread %f, got %f", expectedSpread, spread)
	}
	
	// Test CanFillOrder
	if !ob.CanFillOrder("buy", 0.05) {
		t.Error("Should be able to fill order for 0.05")
	}
	if ob.CanFillOrder("buy", 1.0) {
		t.Error("Should not be able to fill order for 1.0 (insufficient liquidity)")
	}
}

