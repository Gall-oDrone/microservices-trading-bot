package pnl_test

import (
	"strings"
	"testing"

	"bitso_trading_bot/internal/services/trading_bot/pnl"
	"bitso_trading_bot/internal/utils"
	"bitso_trading_bot/pkg/bitso"
)

func TestPnLColorFormatting(t *testing.T) {
	// Create a new PnL manager
	manager := pnl.NewManager()

	// Create test books
	btcBook := bitso.NewBook(bitso.BTC, bitso.USD)
	ethBook := bitso.NewBook(bitso.ETH, bitso.USD)

	// Test cases for different scenarios
	testCases := []struct {
		name        string
		trade       pnl.TradePnL
		expectedPnL float64
	}{
		{
			name: "BTC Profitable Buy",
			trade: pnl.TradePnL{
				Book:       btcBook,
				Side:       bitso.OrderSideBuy,
				EntryPrice: 50000.0,
				ExitPrice:  51000.0,
				Amount:     0.1,
				Fees:       5.0,
			},
			expectedPnL: 100.0,
		},
		{
			name: "BTC Lossy Sell",
			trade: pnl.TradePnL{
				Book:       btcBook,
				Side:       bitso.OrderSideSell,
				EntryPrice: 51000.0,
				ExitPrice:  50000.0,
				Amount:     0.1,
				Fees:       5.0,
			},
			expectedPnL: -100.0,
		},
		{
			name: "ETH Profitable Sell",
			trade: pnl.TradePnL{
				Book:       ethBook,
				Side:       bitso.OrderSideSell,
				EntryPrice: 3000.0,
				ExitPrice:  2900.0,
				Amount:     1.0,
				Fees:       3.0,
			},
			expectedPnL: 100.0,
		},
		{
			name: "ETH Lossy Buy",
			trade: pnl.TradePnL{
				Book:       ethBook,
				Side:       bitso.OrderSideBuy,
				EntryPrice: 3000.0,
				ExitPrice:  2900.0,
				Amount:     1.0,
				Fees:       3.0,
			},
			expectedPnL: -100.0,
		},
	}

	// Run test cases
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Add trade to manager
			manager.AddTrade(tc.trade)

			// Get formatted string
			tradeString := manager.GetTradePnLString(tc.trade)

			// Verify color formatting
			if tc.expectedPnL > 0 {
				// For profitable trades, verify green color
				if !strings.Contains(tradeString, "\033[32m") {
					t.Errorf("Expected profitable trade to be colored green")
				}
			} else {
				// For losing trades, verify red color
				if !strings.Contains(tradeString, "\033[31m") {
					t.Errorf("Expected losing trade to be colored red")
				}
			}

			// Verify accumulated PnL formatting
			accString := manager.GetAccumulatedPnLString(tc.trade.Book)
			if accString == "" {
				t.Error("Expected non-empty accumulated PnL string")
			}

			// Get accumulated PnL
			accPnL := manager.GetAccumulatedPnL(tc.trade.Book)
			if accPnL == nil {
				t.Fatal("Expected accumulated PnL to exist")
			}

			// Verify accumulated PnL color
			if accPnL.NetPnL > 0 {
				if !strings.Contains(accString, "\033[32m") {
					t.Errorf("Expected positive accumulated PnL to be colored green")
				}
			} else if accPnL.NetPnL < 0 {
				if !strings.Contains(accString, "\033[31m") {
					t.Errorf("Expected negative accumulated PnL to be colored red")
				}
			}
		})
	}
}

func TestColorizeTabularData(t *testing.T) {
	// Test data with mixed positive and negative values
	testData := `BOOK	SIDE	ENTRY	EXIT	AMOUNT	PnL	PnL%	FEES	NET_PNL
btc_usd	buy	50000.0000	51000.0000	0.1000	100.0000	2.00%	5.0000	95.0000
btc_usd	sell	51000.0000	50000.0000	0.1000	-100.0000	-2.00%	5.0000	-105.0000`

	coloredData := utils.ColorizeTabularData(testData)

	// Verify header is not colored
	if strings.Contains(coloredData, "\033[32m") || strings.Contains(coloredData, "\033[31m") {
		t.Error("Header should not be colored")
	}

	// Verify positive values are green
	if !strings.Contains(coloredData, "\033[32m") {
		t.Error("Positive values should be colored green")
	}

	// Verify negative values are red
	if !strings.Contains(coloredData, "\033[31m") {
		t.Error("Negative values should be colored red")
	}
}
