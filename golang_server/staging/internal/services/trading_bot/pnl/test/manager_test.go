package pnl_test

import (
	"strings"
	"testing"

	"bitso_trading_bot/internal/services/trading_bot/pnl"
	"bitso_trading_bot/pkg/bitso"
)

func TestManager(t *testing.T) {
	// Create a new PnL manager
	manager := pnl.NewManager()

	// Create a test book
	book := bitso.NewBook(bitso.BTC, bitso.USD)

	// Test case 1: Buy trade with profit
	buyTrade := pnl.TradePnL{
		Book:       book,
		Side:       bitso.OrderSideBuy,
		EntryPrice: 50000.0,
		ExitPrice:  51000.0,
		Amount:     0.1,
		Fees:       5.0,
	}

	manager.AddTrade(buyTrade)

	// Test case 2: Sell trade with loss
	sellTrade := pnl.TradePnL{
		Book:       book,
		Side:       bitso.OrderSideSell,
		EntryPrice: 51000.0,
		ExitPrice:  50000.0,
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

	// Expected PnL calculations:
	// Buy trade: (51000 - 50000) * 0.1 = 100 profit
	// Sell trade: (51000 - 50000) * 0.1 = 100 profit
	// Total fees: 5 + 5 = 10
	expectedNetPnL := 190.0 // 100 + 100 - 10
	if accPnL.NetPnL != expectedNetPnL {
		t.Errorf("Expected net PnL of %.2f, got %.2f", expectedNetPnL, accPnL.NetPnL)
	}

	// Verify win rate
	expectedWinRate := 100.0 // Both trades are profitable
	if accPnL.WinRate != expectedWinRate {
		t.Errorf("Expected win rate of %.2f%%, got %.2f%%", expectedWinRate, accPnL.WinRate)
	}

	// Test trade history
	history := manager.GetTradeHistory(book)
	if len(history) != 2 {
		t.Errorf("Expected 2 trades in history, got %d", len(history))
	}

	// Test string formatting with colors
	tradeString := manager.GetTradePnLString(buyTrade)
	if tradeString == "" {
		t.Error("Expected non-empty trade string")
	}
	// Verify that profit values are colored green
	if !strings.Contains(tradeString, "\033[32m") {
		t.Error("Expected PnL value to be colored green")
	}

	accString := manager.GetAccumulatedPnLString(book)
	if accString == "" {
		t.Error("Expected non-empty accumulated PnL string")
	}
	// Verify that net PnL is colored green
	if !strings.Contains(accString, "\033[32m") {
		t.Error("Expected net PnL to be colored green")
	}

	// Test case 3: Trade with loss
	lossTrade := pnl.TradePnL{
		Book:       book,
		Side:       bitso.OrderSideBuy,
		EntryPrice: 51000.0,
		ExitPrice:  50000.0,
		Amount:     0.1,
		Fees:       5.0,
	}

	manager.AddTrade(lossTrade)
	lossString := manager.GetTradePnLString(lossTrade)
	// Verify that loss values are colored red
	if !strings.Contains(lossString, "\033[31m") {
		t.Error("Expected PnL value to be colored red")
	}
}
