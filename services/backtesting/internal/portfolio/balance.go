package portfolio

import (
	"fmt"
)

// UpdateBalance updates the portfolio balance by the given amount
func UpdateBalance(portfolio *VirtualPortfolio, amount float64) {
	portfolio.mu.Lock()
	defer portfolio.mu.Unlock()

	portfolio.CurrentBalance += amount
	updatePeakBalance(portfolio)
}

// RecordCommission records a commission charge
func RecordCommission(portfolio *VirtualPortfolio, commission float64) {
	portfolio.mu.Lock()
	defer portfolio.mu.Unlock()

	portfolio.TotalCommissions += commission
	portfolio.CurrentBalance -= commission
}

// updatePeakBalance updates the peak balance if current is higher
func updatePeakBalance(portfolio *VirtualPortfolio) {
	if portfolio.CurrentBalance > portfolio.PeakBalance {
		portfolio.PeakBalance = portfolio.CurrentBalance
	}
}

// CalculateDrawdown calculates the current drawdown from peak
func CalculateDrawdown(portfolio *VirtualPortfolio) float64 {
	portfolio.mu.RLock()
	defer portfolio.mu.RUnlock()

	drawdown := portfolio.PeakBalance - portfolio.CurrentBalance
	if drawdown < 0 {
		drawdown = 0
	}

	return drawdown
}

// CalculateDrawdownPercent calculates drawdown as a percentage of peak
func CalculateDrawdownPercent(portfolio *VirtualPortfolio) float64 {
	portfolio.mu.RLock()
	defer portfolio.mu.RUnlock()

	if portfolio.PeakBalance == 0 {
		return 0
	}

	drawdown := portfolio.PeakBalance - portfolio.CurrentBalance
	if drawdown < 0 {
		return 0
	}

	return (drawdown / portfolio.PeakBalance) * 100
}

// GetTotalValue calculates total portfolio value (balance + position values)
func GetTotalValue(portfolio *VirtualPortfolio, prices map[string]float64) float64 {
	portfolio.mu.RLock()
	defer portfolio.mu.RUnlock()

	totalValue := portfolio.CurrentBalance

	// Add value of all positions
	for book, pos := range portfolio.Positions {
		if currentPrice, exists := prices[book]; exists {
			totalValue += currentPrice * pos.Size
		} else {
			totalValue += pos.AveragePrice * pos.Size
		}
	}

	return totalValue
}

// CalculateReturn calculates the total return
func CalculateReturn(portfolio *VirtualPortfolio) float64 {
	portfolio.mu.RLock()
	defer portfolio.mu.RUnlock()

	return portfolio.CurrentBalance - portfolio.InitialBalance
}

// CalculateReturnPercent calculates the return as a percentage
func CalculateReturnPercent(portfolio *VirtualPortfolio) float64 {
	portfolio.mu.RLock()
	defer portfolio.mu.RUnlock()

	if portfolio.InitialBalance == 0 {
		return 0
	}

	return ((portfolio.CurrentBalance - portfolio.InitialBalance) / portfolio.InitialBalance) * 100
}

// ValidateBalance validates the portfolio balance
func ValidateBalance(portfolio *VirtualPortfolio) error {
	portfolio.mu.RLock()
	defer portfolio.mu.RUnlock()

	if portfolio.CurrentBalance < 0 {
		return fmt.Errorf("negative balance: %.2f", portfolio.CurrentBalance)
	}

	return nil
}

// CanAfford checks if the portfolio can afford a given amount
func CanAfford(portfolio *VirtualPortfolio, amount float64) bool {
	portfolio.mu.RLock()
	defer portfolio.mu.RUnlock()

	return portfolio.CurrentBalance >= amount
}
