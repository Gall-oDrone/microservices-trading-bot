package risk

import (
	"bitso-trading-platform/shared/pkg/models"
	"fmt"
)

// Manager handles risk management for trading operations
type Manager struct {
	config *models.TradingConfig
}

// NewManager creates a new risk manager
func NewManager(config *models.TradingConfig) *Manager {
	return &Manager{
		config: config,
	}
}

// ValidateTradeAmount checks if the trade amount is within acceptable limits
func (m *Manager) ValidateTradeAmount(amount float64, price float64) error {
	if !m.config.IsWithinTradeLimits(amount, price) {
		return fmt.Errorf("trade amount %.8f at price %.2f exceeds configured limits", amount, price)
	}
	return nil
}

// CheckPositionLimits verifies position limits are not exceeded
func (m *Manager) CheckPositionLimits(currentPositions int) error {
	if currentPositions >= m.config.MaxOpenPositions {
		return fmt.Errorf("maximum open positions (%d) reached", m.config.MaxOpenPositions)
	}
	return nil
}

// CheckTradingHours verifies if trading is allowed at the current time
func (m *Manager) CheckTradingHours() error {
	if !m.config.IsWithinTradingHours() {
		return fmt.Errorf("trading outside configured hours")
	}
	return nil
}

// CalculateStopLoss calculates the stop loss price for a position
func (m *Manager) CalculateStopLoss(entryPrice float64, isBuy bool) float64 {
	if isBuy {
		return entryPrice * (1 - m.config.StopLossPercent/100)
	}
	return entryPrice * (1 + m.config.StopLossPercent/100)
}

// CalculateTakeProfit calculates the take profit price for a position
func (m *Manager) CalculateTakeProfit(entryPrice float64, isBuy bool) float64 {
	if isBuy {
		return entryPrice * (1 + m.config.TakeProfitPercent/100)
	}
	return entryPrice * (1 - m.config.TakeProfitPercent/100)
}
