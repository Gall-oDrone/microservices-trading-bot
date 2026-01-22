package portfolio

import (
	"testing"
	"time"

	"bitso-trading-platform/backtesting/internal/models"
)

func TestNewVirtualPortfolio(t *testing.T) {
	portfolio := NewVirtualPortfolio("test-1", 100000.0)

	if portfolio.ID != "test-1" {
		t.Errorf("Expected ID 'test-1', got '%s'", portfolio.ID)
	}

	if portfolio.InitialBalance != 100000.0 {
		t.Errorf("Expected initial balance 100000.0, got %f", portfolio.InitialBalance)
	}

	if portfolio.CurrentBalance != 100000.0 {
		t.Errorf("Expected current balance 100000.0, got %f", portfolio.CurrentBalance)
	}

	if portfolio.PeakBalance != 100000.0 {
		t.Errorf("Expected peak balance 100000.0, got %f", portfolio.PeakBalance)
	}
}

func TestExecuteBuyTrade(t *testing.T) {
	portfolio := NewVirtualPortfolio("test", 100000.0)
	now := time.Now()

	trade := models.NewTrade("buy", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
	// For buy trade, we need to set ExitPrice for validation
	trade.ExitPrice = 500000.0 // Temporary for validation
	trade.ExitTime = now

	err := portfolio.ExecuteTrade(trade)
	if err != nil {
		t.Fatalf("ExecuteTrade() error = %v", err)
	}

	// Check balance (should be reduced by cost)
	expectedBalance := 100000.0 - (500000.0*0.01 + 50.0)
	if portfolio.GetBalance() != expectedBalance {
		t.Errorf("Expected balance %f, got %f", expectedBalance, portfolio.GetBalance())
	}

	// Check position created
	pos, err := portfolio.GetPosition("btc_mxn")
	if err != nil {
		t.Fatalf("GetPosition() error = %v", err)
	}

	if pos.Size != 0.01 {
		t.Errorf("Expected position size 0.01, got %f", pos.Size)
	}

	// Check commission recorded
	if portfolio.TotalCommissions != 50.0 {
		t.Errorf("Expected total commissions 50.0, got %f", portfolio.TotalCommissions)
	}
}

func TestExecuteSellTrade(t *testing.T) {
	portfolio := NewVirtualPortfolio("test", 100000.0)
	now := time.Now()

	// First buy to create position
	buyTrade := models.NewTrade("buy", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
	buyTrade.ExitPrice = 500000.0 // For validation
	buyTrade.ExitTime = now
	err := portfolio.ExecuteTrade(buyTrade)
	if err != nil {
		t.Fatalf("ExecuteTrade(buy) error = %v", err)
	}

	initialBalance := portfolio.GetBalance()

	// Now sell
	sellTrade := models.NewTrade("sell", "btc_mxn", 510000.0, 0.01, 50.0, 5.0, now.Add(time.Hour))
	sellTrade.Close(510000.0, now.Add(time.Hour))

	err = portfolio.ExecuteTrade(sellTrade)
	if err != nil {
		t.Fatalf("ExecuteTrade(sell) error = %v", err)
	}

	// Balance should increase by proceeds
	expectedIncrease := (510000.0 * 0.01) - 50.0
	expectedBalance := initialBalance + expectedIncrease

	balance := portfolio.GetBalance()
	if balance < expectedBalance-1 || balance > expectedBalance+1 {
		t.Errorf("Expected balance ~%f, got %f", expectedBalance, balance)
	}

	// Position should be empty
	pos, _ := portfolio.GetPosition("btc_mxn")
	if !pos.IsEmpty() {
		t.Error("Expected position to be empty after full sell")
	}

	// Total P&L should be positive
	if portfolio.TotalPL <= 0 {
		t.Errorf("Expected positive P&L, got %f", portfolio.TotalPL)
	}
}

func TestInsufficientBalance(t *testing.T) {
	portfolio := NewVirtualPortfolio("test", 1000.0) // Small balance
	now := time.Now()

	// Try to buy more than we can afford
	trade := models.NewTrade("buy", "btc_mxn", 500000.0, 1.0, 50.0, 5.0, now)

	err := portfolio.ExecuteTrade(trade)
	if err == nil {
		t.Error("Expected error for insufficient balance")
	}

	// Balance should be unchanged
	if portfolio.GetBalance() != 1000.0 {
		t.Error("Balance should not change on failed trade")
	}
}

func TestInsufficientPosition(t *testing.T) {
	portfolio := NewVirtualPortfolio("test", 100000.0)
	now := time.Now()

	// Try to sell without position
	trade := models.NewTrade("sell", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
	trade.Close(500000.0, now.Add(time.Hour))

	err := portfolio.ExecuteTrade(trade)
	if err == nil {
		t.Error("Expected error for insufficient position")
	}
}

func TestCalculateEquity(t *testing.T) {
	portfolio := NewVirtualPortfolio("test", 100000.0)
	now := time.Now()

	// Buy position
	trade := models.NewTrade("buy", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
	trade.ExitPrice = 500000.0 // For validation
	trade.ExitTime = now
	portfolio.ExecuteTrade(trade)

	// Calculate equity with current prices
	prices := map[string]float64{
		"btc_mxn": 510000.0, // Price increased
	}

	equity := portfolio.CalculateEquity(prices)

	// Equity should be balance + unrealized P&L
	// Unrealized P&L = (510000 - 500000) * 0.01 = 100
	expectedEquity := portfolio.GetBalance() + 100.0

	if equity < expectedEquity-1 || equity > expectedEquity+1 {
		t.Errorf("Expected equity ~%f, got %f", expectedEquity, equity)
	}
}

func TestPortfolioClone(t *testing.T) {
	original := NewVirtualPortfolio("test", 100000.0)
	now := time.Now()

	// Add position
	trade := models.NewTrade("buy", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
	trade.ExitPrice = 500000.0
	trade.ExitTime = now
	original.ExecuteTrade(trade)

	// Clone
	clone := original.Clone()

	// Modify clone
	clone.CurrentBalance = 50000.0

	// Original should be unchanged
	if original.GetBalance() == 50000.0 {
		t.Error("Clone modified original balance")
	}
}

func TestPortfolioReset(t *testing.T) {
	portfolio := NewVirtualPortfolio("test", 100000.0)
	now := time.Now()

	// Add position
	trade := models.NewTrade("buy", "btc_mxn", 500000.0, 0.01, 50.0, 5.0, now)
	trade.ExitPrice = 500000.0
	trade.ExitTime = now
	portfolio.ExecuteTrade(trade)

	// Reset
	portfolio.Reset()

	// Everything should be back to initial state
	if portfolio.GetBalance() != 100000.0 {
		t.Error("Balance not reset")
	}
	if len(portfolio.Positions) != 0 {
		t.Error("Positions not cleared")
	}
	if portfolio.TotalPL != 0 {
		t.Error("Total P&L not reset")
	}
}

func TestBalanceHelpers(t *testing.T) {
	portfolio := NewVirtualPortfolio("test", 100000.0)

	// Test CalculateReturn
	portfolio.CurrentBalance = 105000.0
	returnAmount := CalculateReturn(portfolio)
	if returnAmount != 5000.0 {
		t.Errorf("Expected return 5000.0, got %f", returnAmount)
	}

	// Test CalculateReturnPercent
	returnPercent := CalculateReturnPercent(portfolio)
	if returnPercent != 5.0 {
		t.Errorf("Expected return percent 5.0, got %f", returnPercent)
	}

	// Test CanAfford
	if !CanAfford(portfolio, 50000.0) {
		t.Error("Expected to afford 50000.0")
	}
	if CanAfford(portfolio, 200000.0) {
		t.Error("Should not afford 200000.0")
	}

	// Test ValidateBalance
	if err := ValidateBalance(portfolio); err != nil {
		t.Errorf("ValidateBalance() error = %v", err)
	}

	// Test negative balance validation
	portfolio.CurrentBalance = -1000.0
	if err := ValidateBalance(portfolio); err == nil {
		t.Error("Expected error for negative balance")
	}
}

func TestDrawdownCalculation(t *testing.T) {
	portfolio := NewVirtualPortfolio("test", 100000.0)

	// Increase balance (new peak)
	portfolio.CurrentBalance = 110000.0
	portfolio.updatePeakAndDrawdown()

	if portfolio.PeakBalance != 110000.0 {
		t.Errorf("Expected peak 110000.0, got %f", portfolio.PeakBalance)
	}

	// Decrease balance (drawdown)
	portfolio.CurrentBalance = 105000.0
	portfolio.updatePeakAndDrawdown()

	drawdown := CalculateDrawdown(portfolio)
	expectedDrawdown := 110000.0 - 105000.0
	if drawdown != expectedDrawdown {
		t.Errorf("Expected drawdown %f, got %f", expectedDrawdown, drawdown)
	}

	// Test drawdown percent
	drawdownPercent := CalculateDrawdownPercent(portfolio)
	expectedPercent := (5000.0 / 110000.0) * 100
	if drawdownPercent < expectedPercent-0.1 || drawdownPercent > expectedPercent+0.1 {
		t.Errorf("Expected drawdown percent ~%f, got %f", expectedPercent, drawdownPercent)
	}
}
