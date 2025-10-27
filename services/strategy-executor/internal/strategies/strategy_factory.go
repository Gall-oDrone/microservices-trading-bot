package strategies

import (
	"fmt"

	"bitso-trading-platform/shared/pkg/models"
)

// NewBasicStrategyFactory creates a factory for basic strategies
func NewBasicStrategyFactory() StrategyFactory {
	return func(config *models.TradingConfig) (Strategy, error) {
		if config == nil {
			return nil, fmt.Errorf("trading config cannot be nil")
		}

		if config.Book == nil {
			return nil, fmt.Errorf("trading book is required")
		}

		return NewBasicStrategy(config.Book), nil
	}
}

// NewTrendStrategyFactory creates a factory for trend strategies
func NewTrendStrategyFactory() StrategyFactory {
	return func(config *models.TradingConfig) (Strategy, error) {
		if config == nil {
			return nil, fmt.Errorf("trading config cannot be nil")
		}

		if config.Book == nil {
			return nil, fmt.Errorf("trading book is required")
		}

		return NewTrendStrategy(config.Book), nil
	}
}

// NewArbitrageStrategyFactory creates a factory for arbitrage strategies
func NewArbitrageStrategyFactory() StrategyFactory {
	return func(config *models.TradingConfig) (Strategy, error) {
		if config == nil {
			return nil, fmt.Errorf("trading config cannot be nil")
		}

		if config.Book == nil {
			return nil, fmt.Errorf("trading book is required")
		}

		return NewArbitrageStrategy(config.Book), nil
	}
}

// StrategyFactoryBuilder helps build custom strategy factories
type StrategyFactoryBuilder struct {
	createFunc func(config *models.TradingConfig) (Strategy, error)
}

// NewStrategyFactoryBuilder creates a new strategy factory builder
func NewStrategyFactoryBuilder() *StrategyFactoryBuilder {
	return &StrategyFactoryBuilder{}
}

// WithCreateFunc sets the create function
func (b *StrategyFactoryBuilder) WithCreateFunc(fn func(config *models.TradingConfig) (Strategy, error)) *StrategyFactoryBuilder {
	b.createFunc = fn
	return b
}

// Build builds the strategy factory
func (b *StrategyFactoryBuilder) Build() StrategyFactory {
	return func(config *models.TradingConfig) (Strategy, error) {
		if b.createFunc == nil {
			return nil, fmt.Errorf("create function not set")
		}
		return b.createFunc(config)
	}
}
