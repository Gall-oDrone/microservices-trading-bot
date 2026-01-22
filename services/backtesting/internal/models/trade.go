package models

import (
	"fmt"
	"time"
)

// Trade represents a completed trade in the backtest
type Trade struct {
	ID                string    `json:"id"`
	EntryTime         time.Time `json:"entry_time"`
	ExitTime          time.Time `json:"exit_time"`
	Book              string    `json:"book"`
	Side              string    `json:"side"` // "buy", "sell"
	EntryPrice        float64   `json:"entry_price"`
	ExitPrice         float64   `json:"exit_price"`
	Amount            float64   `json:"amount"`
	ProfitLoss        float64   `json:"profit_loss"`
	ProfitLossPercent float64   `json:"profit_loss_percent"`
	Commission        float64   `json:"commission"`
	Slippage          float64   `json:"slippage"`
	HoldingTime       int64     `json:"holding_time"` // Duration in seconds
	StrategySignal    string    `json:"strategy_signal,omitempty"`
}

// NewTrade creates a new trade
func NewTrade(side, book string, entryPrice, amount, commission, slippage float64, entryTime time.Time) *Trade {
	return &Trade{
		ID:          generateTradeID(),
		Side:        side,
		Book:        book,
		EntryPrice:  entryPrice,
		Amount:      amount,
		Commission:  commission,
		Slippage:    slippage,
		EntryTime:   entryTime,
		HoldingTime: 0,
	}
}

// Close closes the trade with exit price and time
func (t *Trade) Close(exitPrice float64, exitTime time.Time) {
	t.ExitPrice = exitPrice
	t.ExitTime = exitTime
	t.HoldingTime = int64(exitTime.Sub(t.EntryTime).Seconds())
	t.CalculatePL()
}

// CalculatePL calculates the profit/loss for the trade
func (t *Trade) CalculatePL() {
	if t.Side == "buy" {
		// Long position: profit = (exit_price - entry_price) * amount
		t.ProfitLoss = (t.ExitPrice - t.EntryPrice) * t.Amount
	} else {
		// Short position: profit = (entry_price - exit_price) * amount
		t.ProfitLoss = (t.EntryPrice - t.ExitPrice) * t.Amount
	}

	// Subtract commission
	t.ProfitLoss -= t.Commission

	// Calculate percentage
	if t.EntryPrice > 0 {
		t.ProfitLossPercent = (t.ProfitLoss / (t.EntryPrice * t.Amount)) * 100
	}
}

// IsWinning returns true if the trade was profitable
func (t *Trade) IsWinning() bool {
	return t.ProfitLoss > 0
}

// IsLosing returns true if the trade was unprofitable
func (t *Trade) IsLosing() bool {
	return t.ProfitLoss < 0
}

// IsBreakEven returns true if the trade broke even
func (t *Trade) IsBreakEven() bool {
	return t.ProfitLoss == 0
}

// GetHoldingDuration returns the holding duration as a time.Duration
func (t *Trade) GetHoldingDuration() time.Duration {
	return time.Duration(t.HoldingTime) * time.Second
}

// GetCostBasis returns the cost basis of the trade
func (t *Trade) GetCostBasis() float64 {
	return t.EntryPrice * t.Amount
}

// GetProceeds returns the proceeds from closing the trade
func (t *Trade) GetProceeds() float64 {
	return t.ExitPrice * t.Amount
}

// GetNetProfit returns the net profit (after commission)
func (t *Trade) GetNetProfit() float64 {
	return t.ProfitLoss
}

// GetGrossProfit returns the gross profit (before commission)
func (t *Trade) GetGrossProfit() float64 {
	return t.ProfitLoss + t.Commission
}

// Validate validates the trade data
func (t *Trade) Validate() error {
	if t.Book == "" {
		return fmt.Errorf("book is required")
	}

	if t.Side != "buy" && t.Side != "sell" {
		return fmt.Errorf("side must be 'buy' or 'sell', got: %s", t.Side)
	}

	if t.EntryPrice <= 0 {
		return fmt.Errorf("entry_price must be positive, got: %f", t.EntryPrice)
	}

	if t.ExitPrice <= 0 {
		return fmt.Errorf("exit_price must be positive, got: %f", t.ExitPrice)
	}

	if t.Amount <= 0 {
		return fmt.Errorf("amount must be positive, got: %f", t.Amount)
	}

	if t.EntryTime.IsZero() {
		return fmt.Errorf("entry_time is required")
	}

	if t.ExitTime.IsZero() {
		return fmt.Errorf("exit_time is required")
	}

	if t.ExitTime.Before(t.EntryTime) {
		return fmt.Errorf("exit_time must be after entry_time")
	}

	return nil
}

// String returns a string representation of the trade
func (t *Trade) String() string {
	return fmt.Sprintf("Trade{%s %s @ %s: entry=%.2f, exit=%.2f, amount=%.8f, P&L=%.2f (%.2f%%), duration=%ds}",
		t.ID, t.Side, t.Book, t.EntryPrice, t.ExitPrice, t.Amount,
		t.ProfitLoss, t.ProfitLossPercent, t.HoldingTime)
}

// generateTradeID generates a unique ID for a trade
func generateTradeID() string {
	return fmt.Sprintf("trd-%d", time.Now().UnixNano())
}
