package portfolio

import (
	"fmt"

	"bitso-trading-platform/backtesting/internal/models"
)

// OpenPosition opens or adds to a position
func OpenPosition(portfolio *VirtualPortfolio, book string, side string, amount, price, commission float64) error {
	portfolio.mu.Lock()
	defer portfolio.mu.Unlock()
	
	// Get or create position
	pos, exists := portfolio.Positions[book]
	if !exists {
		pos = models.NewPosition(book)
		portfolio.Positions[book] = pos
	}
	
	// Calculate cost
	cost := (price * amount) + commission
	
	// Check balance
	if cost > portfolio.CurrentBalance {
		return fmt.Errorf("insufficient balance: need %.2f, have %.2f", cost, portfolio.CurrentBalance)
	}
	
	// Deduct balance
	portfolio.CurrentBalance -= cost
	
	// Add to position
	pos.AddSize(amount, price)
	
	// Record commission
	portfolio.TotalCommissions += commission
	
	return nil
}

// ClosePosition closes or reduces a position
func ClosePosition(portfolio *VirtualPortfolio, book string, side string, amount, price, commission float64) error {
	portfolio.mu.Lock()
	defer portfolio.mu.Unlock()
	
	// Get position
	pos, exists := portfolio.Positions[book]
	if !exists || pos.IsEmpty() {
		return fmt.Errorf("no position to close for book: %s", book)
	}
	
	// Check if we have enough position
	if amount > pos.Size {
		return fmt.Errorf("insufficient position: need %.8f, have %.8f", amount, pos.Size)
	}
	
	// Calculate proceeds
	proceeds := (price * amount) - commission
	
	// Reduce position and get realized P&L
	realizedPL := pos.ReduceSize(amount, price)
	
	// Add proceeds to balance
	portfolio.CurrentBalance += proceeds
	
	// Update total P&L
	portfolio.TotalPL += realizedPL - commission
	
	// Record commission
	portfolio.TotalCommissions += commission
	
	return nil
}

// UpdatePosition updates position with new price information
func UpdatePosition(portfolio *VirtualPortfolio, book string, price float64) error {
	portfolio.mu.Lock()
	defer portfolio.mu.Unlock()
	
	pos, exists := portfolio.Positions[book]
	if !exists {
		return nil // No position to update
	}
	
	pos.UpdateCurrentPrice(price)
	return nil
}

// CalculatePositionPL calculates P&L for a position at exit price
func CalculatePositionPL(position *models.Position, exitPrice float64) float64 {
	if position.IsEmpty() {
		return 0
	}
	
	return (exitPrice - position.AveragePrice) * position.Size
}

// GetTotalPositionValue calculates the total value of all positions
func GetTotalPositionValue(portfolio *VirtualPortfolio, prices map[string]float64) float64 {
	portfolio.mu.RLock()
	defer portfolio.mu.RUnlock()
	
	totalValue := 0.0
	
	for book, pos := range portfolio.Positions {
		if currentPrice, exists := prices[book]; exists {
			totalValue += currentPrice * pos.Size
		} else {
			totalValue += pos.AveragePrice * pos.Size
		}
	}
	
	return totalValue
}

// GetUnrealizedPL calculates total unrealized P&L across all positions
func GetUnrealizedPL(portfolio *VirtualPortfolio, prices map[string]float64) float64 {
	portfolio.mu.RLock()
	defer portfolio.mu.RUnlock()
	
	totalUnrealizedPL := 0.0
	
	for book, pos := range portfolio.Positions {
		if currentPrice, exists := prices[book]; exists {
			pos.UpdateCurrentPrice(currentPrice)
			totalUnrealizedPL += pos.UnrealizedPL
		}
	}
	
	return totalUnrealizedPL
}

