package simulator

import (
	"fmt"

	"bitso-trading-platform/shared/pkg/models"
)

// calculateExecutionPrice calculates the execution price with slippage
func calculateExecutionPrice(basePrice float64, side string, slippage float64) float64 {
	if side == "buy" {
		// Buy orders get worse price (higher)
		return basePrice + slippage
	} else {
		// Sell orders get worse price (lower)
		return basePrice - slippage
	}
}

// calculateCommission calculates the commission for a trade
func calculateCommission(amount, price, rate float64) float64 {
	return amount * price * rate
}

// validateOrder validates an order before execution
func validateOrder(order *models.Order) error {
	if order == nil {
		return fmt.Errorf("order cannot be nil")
	}

	if order.Symbol == "" {
		return fmt.Errorf("order symbol is required")
	}

	if order.Side != "buy" && order.Side != "sell" {
		return fmt.Errorf("invalid order side: %s (must be 'buy' or 'sell')", order.Side)
	}

	if order.Amount <= 0 {
		return fmt.Errorf("order amount must be positive, got: %f", order.Amount)
	}

	if order.Price < 0 {
		return fmt.Errorf("order price cannot be negative, got: %f", order.Price)
	}

	return nil
}

// executeMarketOrder executes a market order at current price
func executeMarketOrder(sim *Simulator, order *models.Order) (*OrderExecution, error) {
	// Get current price
	currentPrice, err := sim.GetCurrentPrice(order.Symbol)
	if err != nil {
		return &OrderExecution{
			OrderID: order.ID,
			Success: false,
			Error:   err.Error(),
		}, err
	}

	// Calculate slippage
	slippage := sim.slippageModel.Calculate(order, currentPrice)

	// Calculate execution price
	executionPrice := calculateExecutionPrice(currentPrice, order.Side, slippage)

	// Market order = taker fee
	rate := sim.config.TakerFee
	if sim.config.MakerFee == 0 && sim.config.TakerFee == 0 {
		rate = sim.config.CommissionRate
	}
	commission := calculateCommission(order.Amount, executionPrice, rate)

	return &OrderExecution{
		OrderID:        order.ID,
		ExecutedPrice:  executionPrice,
		ExecutedAmount: order.Amount,
		Commission:     commission,
		Slippage:       slippage,
		Timestamp:      sim.lastUpdate,
		Success:        true,
	}, nil
}

// executeLimitOrder executes a limit order (if price is reached)
func executeLimitOrder(sim *Simulator, order *models.Order) (*OrderExecution, error) {
	// Get current price
	currentPrice, err := sim.GetCurrentPrice(order.Symbol)
	if err != nil {
		return &OrderExecution{
			OrderID: order.ID,
			Success: false,
			Error:   err.Error(),
		}, err
	}

	// Check if limit price is reached
	canExecute := false
	if order.Side == "buy" && currentPrice <= order.Price {
		canExecute = true
	} else if order.Side == "sell" && currentPrice >= order.Price {
		canExecute = true
	}

	if !canExecute {
		return &OrderExecution{
			OrderID: order.ID,
			Success: false,
			Error:   "limit price not reached",
		}, fmt.Errorf("limit price not reached")
	}

	// Execute at limit price (or better)
	executionPrice := order.Price

	// Calculate slippage (minimal for limit orders)
	slippage := sim.slippageModel.Calculate(order, executionPrice) * 0.5 // Reduced slippage
	executionPrice = calculateExecutionPrice(executionPrice, order.Side, slippage)

	// Limit order resting = maker fee
	rate := sim.config.MakerFee
	if sim.config.MakerFee == 0 && sim.config.TakerFee == 0 {
		rate = sim.config.CommissionRate
	}
	commission := calculateCommission(order.Amount, executionPrice, rate)

	return &OrderExecution{
		OrderID:        order.ID,
		ExecutedPrice:  executionPrice,
		ExecutedAmount: order.Amount,
		Commission:     commission,
		Slippage:       slippage,
		Timestamp:      sim.lastUpdate,
		Success:        true,
	}, nil
}
