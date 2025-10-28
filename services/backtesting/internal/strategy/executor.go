package strategy

import (
	"fmt"

	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/models"
)

// StrategyExecutor executes a strategy and processes market events
type StrategyExecutor struct {
	strategy Strategy
	logger   logger.Logger
}

// NewStrategyExecutor creates a new strategy executor
func NewStrategyExecutor(strategy Strategy, log logger.Logger) *StrategyExecutor {
	return &StrategyExecutor{
		strategy: strategy,
		logger:   log,
	}
}

// ProcessEvent processes a market event and returns a signal
func (e *StrategyExecutor) ProcessEvent(event *models.MarketEvent) (*Signal, error) {
	var signal *Signal
	var err error
	
	// Process based on event type
	switch event.EventType {
	case models.EventTypeTrade:
		trade, err := event.GetTrade()
		if err != nil {
			return nil, fmt.Errorf("failed to get trade: %w", err)
		}
		signal, err = e.strategy.OnTrade(trade)
		
	case models.EventTypeTicker:
		ticker, err := event.GetTicker()
		if err != nil {
			return nil, fmt.Errorf("failed to get ticker: %w", err)
		}
		signal, err = e.strategy.OnTicker(ticker)
		
	case models.EventTypeOrderBook:
		orderBook, err := event.GetOrderBook()
		if err != nil {
			return nil, fmt.Errorf("failed to get order book: %w", err)
		}
		signal, err = e.strategy.OnOrderBook(orderBook)
		
	default:
		return nil, fmt.Errorf("unsupported event type: %s", event.EventType)
	}
	
	if err != nil {
		return nil, fmt.Errorf("strategy processing error: %w", err)
	}
	
	// Log signal if actionable
	if signal != nil && signal.IsActionableSignal() && e.logger != nil {
		e.logger.Debug("Strategy signal generated", map[string]interface{}{
			"type":   signal.Type,
			"book":   signal.Book,
			"price":  signal.Price,
			"amount": signal.Amount,
			"reason": signal.Reason,
		})
	}
	
	return signal, nil
}

// Reset resets the strategy state
func (e *StrategyExecutor) Reset() error {
	return e.strategy.Reset()
}

// GetStrategy returns the underlying strategy
func (e *StrategyExecutor) GetStrategy() Strategy {
	return e.strategy
}

// GetStrategyName returns the strategy name
func (e *StrategyExecutor) GetStrategyName() string {
	return e.strategy.GetName()
}

