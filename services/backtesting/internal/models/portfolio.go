package models

import (
	"fmt"
	"time"
)

// Position represents a trading position
type Position struct {
	Book         string    `json:"book"`
	Size         float64   `json:"size"`          // Positive for long, negative for short
	AveragePrice float64   `json:"average_price"` // Average entry price
	CurrentPrice float64   `json:"current_price"` // Current market price
	UnrealizedPL float64   `json:"unrealized_pl"` // Unrealized profit/loss
	CostBasis    float64   `json:"cost_basis"`    // Total cost of the position
	Timestamp    time.Time `json:"timestamp"`     // Last update time
}

// NewPosition creates a new empty position
func NewPosition(book string) *Position {
	return &Position{
		Book:         book,
		Size:         0,
		AveragePrice: 0,
		CurrentPrice: 0,
		UnrealizedPL: 0,
		CostBasis:    0,
		Timestamp:    time.Now(),
	}
}

// AddSize adds to the position size (opens or increases position)
func (p *Position) AddSize(amount, price float64) {
	if amount <= 0 {
		return
	}

	// Calculate new average price using weighted average
	totalCost := p.CostBasis + (amount * price)
	p.Size += amount
	p.CostBasis = totalCost

	if p.Size != 0 {
		p.AveragePrice = p.CostBasis / p.Size
	}

	p.Timestamp = time.Now()
	p.CalculateUnrealizedPL()
}

// ReduceSize reduces the position size (closes or decreases position)
// Returns the realized profit/loss for the closed portion
func (p *Position) ReduceSize(amount, price float64) float64 {
	if amount <= 0 || p.Size == 0 {
		return 0
	}

	// Limit amount to current size
	if amount > p.Size {
		amount = p.Size
	}

	// Calculate realized P&L for the closed portion
	realizedPL := (price - p.AveragePrice) * amount

	// Update position
	p.Size -= amount
	p.CostBasis -= p.AveragePrice * amount

	// If position is fully closed, reset values
	if p.Size == 0 {
		p.AveragePrice = 0
		p.CostBasis = 0
		p.UnrealizedPL = 0
	}

	p.Timestamp = time.Now()
	p.CalculateUnrealizedPL()

	return realizedPL
}

// UpdateCurrentPrice updates the current market price
func (p *Position) UpdateCurrentPrice(price float64) {
	p.CurrentPrice = price
	p.Timestamp = time.Now()
	p.CalculateUnrealizedPL()
}

// CalculateUnrealizedPL calculates the unrealized profit/loss
func (p *Position) CalculateUnrealizedPL() float64 {
	if p.Size == 0 {
		p.UnrealizedPL = 0
		return 0
	}

	// Unrealized P&L = (current_price - average_price) * size
	p.UnrealizedPL = (p.CurrentPrice - p.AveragePrice) * p.Size
	return p.UnrealizedPL
}

// IsEmpty returns true if the position has no size
func (p *Position) IsEmpty() bool {
	return p.Size == 0
}

// IsLong returns true if the position is long (positive size)
func (p *Position) IsLong() bool {
	return p.Size > 0
}

// IsShort returns true if the position is short (negative size)
func (p *Position) IsShort() bool {
	return p.Size < 0
}

// GetValue returns the current market value of the position
func (p *Position) GetValue() float64 {
	return p.CurrentPrice * p.Size
}

// GetProfitLossPercent returns the unrealized P&L as a percentage
func (p *Position) GetProfitLossPercent() float64 {
	if p.CostBasis == 0 {
		return 0
	}
	return (p.UnrealizedPL / p.CostBasis) * 100
}

// Clone creates a copy of the position
func (p *Position) Clone() *Position {
	return &Position{
		Book:         p.Book,
		Size:         p.Size,
		AveragePrice: p.AveragePrice,
		CurrentPrice: p.CurrentPrice,
		UnrealizedPL: p.UnrealizedPL,
		CostBasis:    p.CostBasis,
		Timestamp:    p.Timestamp,
	}
}

// Validate validates the position data
func (p *Position) Validate() error {
	if p.Book == "" {
		return fmt.Errorf("book is required")
	}

	if p.AveragePrice < 0 {
		return fmt.Errorf("average_price cannot be negative")
	}

	if p.CurrentPrice < 0 {
		return fmt.Errorf("current_price cannot be negative")
	}

	if p.CostBasis < 0 {
		return fmt.Errorf("cost_basis cannot be negative")
	}

	return nil
}

// String returns a string representation of the position
func (p *Position) String() string {
	direction := "FLAT"
	if p.IsLong() {
		direction = "LONG"
	} else if p.IsShort() {
		direction = "SHORT"
	}

	return fmt.Sprintf("Position{%s %s: size=%.8f, avg_price=%.2f, current=%.2f, P&L=%.2f (%.2f%%)}",
		p.Book, direction, p.Size, p.AveragePrice, p.CurrentPrice,
		p.UnrealizedPL, p.GetProfitLossPercent())
}
