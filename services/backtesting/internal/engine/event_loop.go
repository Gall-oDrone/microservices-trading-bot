package engine

import (
	"context"
	"fmt"
	"time"

	"bitso-trading-platform/backtesting/internal/models"
	"bitso-trading-platform/backtesting/internal/simulator"
	"bitso-trading-platform/backtesting/internal/strategy"
	sharedModels "bitso-trading-platform/shared/pkg/models"
)

// runEventLoop processes all market events in chronological order
func (r *BacktestRunner) runEventLoop(ctx context.Context, events []models.MarketEvent, result *models.BacktestResult) error {
	totalEvents := len(events)
	if totalEvents == 0 {
		return fmt.Errorf("no events to process")
	}
	
	r.logger.Info("Starting event loop", map[string]interface{}{
		"total_events": totalEvents,
	})
	
	// Track equity at start
	r.recordEquityPoint(result, r.config.StartDate)
	
	progressUpdateInterval := 1000 // Update progress every 1000 events
	
	for i, event := range events {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		
		// Process event
		if err := r.processEvent(&event, result); err != nil {
			r.logger.Warn("Failed to process event", map[string]interface{}{
				"index": i,
				"error": err,
			})
			// Continue with next event
		}
		
		// Update progress
		if i%progressUpdateInterval == 0 || i == totalEvents-1 {
			progress := float64(i+1) / float64(totalEvents)
			if r.progressCallback != nil {
				r.progressCallback(progress)
			}
		}
	}
	
	// Record final equity point
	r.recordEquityPoint(result, r.config.EndDate)
	
	r.logger.Info("Event loop completed", map[string]interface{}{
		"events_processed": totalEvents,
		"trades_generated": len(result.Trades),
	})
	
	return nil
}

// processEvent processes a single market event
func (r *BacktestRunner) processEvent(event *models.MarketEvent, result *models.BacktestResult) error {
	// Update simulator with market data
	if err := r.simulator.ProcessEvent(event); err != nil {
		return fmt.Errorf("simulator process error: %w", err)
	}
	
	// Get strategy signal
	signal, err := r.strategyExecutor.ProcessEvent(event)
	if err != nil {
		return fmt.Errorf("strategy process error: %w", err)
	}
	
	// Execute signal if actionable
	if signal != nil && signal.IsActionableSignal() {
		if err := r.handleSignal(signal, result); err != nil {
			return fmt.Errorf("signal handling error: %w", err)
		}
	}
	
	return nil
}

// handleSignal handles a trading signal
func (r *BacktestRunner) handleSignal(signal *strategy.Signal, result *models.BacktestResult) error {
	// Create order from signal
	order := &sharedModels.Order{
		ID:     fmt.Sprintf("order-%d", time.Now().UnixNano()),
		Symbol: signal.Book,
		Side:   string(signal.Type),
		Amount: signal.Amount,
		Price:  signal.Price,
	}
	
	// Normalize side
	if order.Side == "BUY" {
		order.Side = "buy"
	} else if order.Side == "SELL" {
		order.Side = "sell"
	}
	
	// Execute order in simulator
	execution, err := r.simulator.ExecuteOrder(order)
	if err != nil || !execution.Success {
		r.logger.Debug("Order execution failed", map[string]interface{}{
			"signal": signal.Type,
			"error":  err,
		})
		return nil // Don't fail backtest, just skip this trade
	}
	
	// Update portfolio with execution
	if err := r.updatePortfolio(execution, result); err != nil {
		return fmt.Errorf("portfolio update error: %w", err)
	}
	
	// Record equity point after trade
	r.recordEquityPoint(result, signal.Timestamp)
	
	return nil
}

// updatePortfolio updates the portfolio with an order execution
func (r *BacktestRunner) updatePortfolio(execution *simulator.OrderExecution, result *models.BacktestResult) error {
	// Create trade for portfolio
	trade := models.NewTrade(
		execution.OrderID[len("order-"):], // Extract simple ID
		r.config.Book,
		execution.ExecutedPrice,
		execution.ExecutedAmount,
		execution.Commission,
		execution.Slippage,
		execution.Timestamp,
	)
	
	// For buy trades, just record entry
	// For sell trades, match with previous position
	trade.ExitPrice = execution.ExecutedPrice
	trade.ExitTime = execution.Timestamp
	trade.CalculatePL()
	
	// Execute in portfolio
	if err := r.portfolio.ExecuteTrade(trade); err != nil {
		return fmt.Errorf("failed to execute trade in portfolio: %w", err)
	}
	
	// Record trade in results
	result.AddTrade(*trade)
	
	return nil
}

// recordEquityPoint records an equity curve point
func (r *BacktestRunner) recordEquityPoint(result *models.BacktestResult, timestamp time.Time) error {
	// Get current prices
	currentPrices := make(map[string]float64)
	if price, err := r.simulator.GetCurrentPrice(r.config.Book); err == nil {
		currentPrices[r.config.Book] = price
	}
	
	// Calculate equity
	balance := r.portfolio.GetBalance()
	equity := r.portfolio.CalculateEquity(currentPrices)
	
	// Calculate return
	returnValue := 0.0
	if r.config.InitialBalance > 0 {
		returnValue = (equity - r.config.InitialBalance) / r.config.InitialBalance
	}
	
	// Calculate drawdown
	summary := r.portfolio.GetSummary()
	drawdown := 0.0
	if summary.PeakBalance > 0 {
		drawdown = (summary.PeakBalance - balance) / summary.PeakBalance
	}
	
	point := models.EquityPoint{
		Timestamp: timestamp,
		Balance:   balance,
		Equity:    equity,
		Return:    returnValue,
		Drawdown:  drawdown,
	}
	
	result.AddEquityPoint(point)
	
	return nil
}

