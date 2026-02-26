package strategy

import (
	"fmt"
)

// StrategyFactory creates strategies by name
type StrategyFactory struct {
	constructors map[string]StrategyConstructor
}

// StrategyConstructor is a function that creates a strategy
type StrategyConstructor func(params map[string]interface{}) (Strategy, error)

// NewStrategyFactory creates a new strategy factory
func NewStrategyFactory() *StrategyFactory {
	factory := &StrategyFactory{
		constructors: make(map[string]StrategyConstructor),
	}

	// Register built-in strategies
	factory.Register("basic", NewBasicStrategy)
	factory.Register("trend", NewTrendStrategy)
	factory.Register("arbitrage", NewArbitrageStrategy)
	// Intraday indicator-based strategies
	factory.Register("vwap_deviation", NewVWAPDeviationStrategy)
	factory.Register("bollinger", NewBollingerStrategy)
	factory.Register("order_flow", NewOrderFlowStrategy)
	factory.Register("rsi_momentum", NewRSIMomentumStrategy)
	factory.Register("volatility_breakout", NewVolatilityBreakoutStrategy)

	return factory
}

// Register registers a strategy constructor
func (f *StrategyFactory) Register(name string, constructor StrategyConstructor) {
	f.constructors[name] = constructor
}

// Create creates a strategy by name
func (f *StrategyFactory) Create(name string, params map[string]interface{}) (Strategy, error) {
	constructor, exists := f.constructors[name]
	if !exists {
		return nil, fmt.Errorf("unknown strategy: %s", name)
	}

	return constructor(params)
}

// GetAvailableStrategies returns a list of available strategy names
func (f *StrategyFactory) GetAvailableStrategies() []string {
	names := make([]string, 0, len(f.constructors))
	for name := range f.constructors {
		names = append(names, name)
	}
	return names
}

// IsStrategyAvailable checks if a strategy is available
func (f *StrategyFactory) IsStrategyAvailable(name string) bool {
	_, exists := f.constructors[name]
	return exists
}

// Global factory instance
var defaultFactory = NewStrategyFactory()

// CreateStrategy creates a strategy using the default factory
func CreateStrategy(name string, params map[string]interface{}) (Strategy, error) {
	return defaultFactory.Create(name, params)
}

// RegisterStrategy registers a strategy with the default factory
func RegisterStrategy(name string, constructor StrategyConstructor) {
	defaultFactory.Register(name, constructor)
}

// GetAvailableStrategies returns available strategies from default factory
func GetAvailableStrategies() []string {
	return defaultFactory.GetAvailableStrategies()
}
