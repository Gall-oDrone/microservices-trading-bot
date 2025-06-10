package table_test

import (
	"testing"

	"bitso_trading_bot/pkg/bitso"
	"bitso_trading_bot/table"
)

func TestPnLManager(t *testing.T) {
	// Create a new PnL manager
	manager := table.NewPnLManager()

	// Create a test book
	book := bitso.NewBook(bitso.BTC, bitso.USD)

	// Test case 1: Buy trade with profit
	buyTrade := table.TradePnL{
		Book:       book,
		Side:       bitso.OrderSideBuy,
		EntryPrice: 50000.0,
		ExitPrice:  51000.0,
		Amount:     0.1,
		Fees:       5.0,
	}

	manager.AddTrade(buyTrade)

	// Test case 2: Sell trade with loss
	sellTrade := table.TradePnL{
		Book:       book,
		Side:       bitso.OrderSideSell,
		EntryPrice: 51000.0,
		ExitPrice:  50500.0,
		Amount:     0.1,
		Fees:       5.0,
	}

	manager.AddTrade(sellTrade)

	// Get accumulated PnL
	accPnL := manager.GetAccumulatedPnL(book)
	if accPnL == nil {
		t.Fatal("Expected accumulated PnL to exist")
	}

	// Verify accumulated statistics
	if accPnL.TotalTrades != 2 {
		t.Errorf("Expected 2 total trades, got %d", accPnL.TotalTrades)
	}

	if accPnL.WinningTrades != 1 {
		t.Errorf("Expected 1 winning trade, got %d", accPnL.WinningTrades)
	}

	if accPnL.LosingTrades != 1 {
		t.Errorf("Expected 1 losing trade, got %d", accPnL.LosingTrades)
	}

	// Expected PnL calculations:
	// Buy trade: (51000 - 50000) * 0.1 = 100 profit
	// Sell trade: (51000 - 50500) * 0.1 = 50 profit
	// Total fees: 5 + 5 = 10
	expectedNetPnL := 140.0 // 100 + 50 - 10
	if accPnL.NetPnL != expectedNetPnL {
		t.Errorf("Expected net PnL of %.2f, got %.2f", expectedNetPnL, accPnL.NetPnL)
	}

	// Verify win rate
	expectedWinRate := 50.0 // 1 winning trade out of 2 total trades
	if accPnL.WinRate != expectedWinRate {
		t.Errorf("Expected win rate of %.2f%%, got %.2f%%", expectedWinRate, accPnL.WinRate)
	}

	// Test trade history
	history := manager.GetTradeHistory(book)
	if len(history) != 2 {
		t.Errorf("Expected 2 trades in history, got %d", len(history))
	}

	// Test string formatting
	tradeString := manager.GetTradePnLString(buyTrade)
	if tradeString == "" {
		t.Error("Expected non-empty trade string")
	}

	accString := manager.GetAccumulatedPnLString(book)
	if accString == "" {
		t.Error("Expected non-empty accumulated PnL string")
	}
}
