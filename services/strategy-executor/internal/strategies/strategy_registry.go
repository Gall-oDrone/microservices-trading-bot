package strategies

import (
	"fmt"
	"sync"

	"bitso-trading-platform/shared/pkg/models"
)

// Registry manages strategy registration and lifecycle
type Registry struct {
	strategies map[string]Strategy
	factories  map[string]StrategyFactory
	mu         sync.RWMutex
}

// StrategyFactory creates strategy instances
type StrategyFactory func(config *models.TradingConfig) (Strategy, error)

// NewRegistry creates a new strategy registry
func NewRegistry() *Registry {
	registry := &Registry{
		strategies: make(map[string]Strategy),
		factories:  make(map[string]StrategyFactory),
	}

	// Register built-in strategies
	registry.RegisterFactory("basic", NewBasicStrategyFactory())
	registry.RegisterFactory("trend", NewTrendStrategyFactory())
	registry.RegisterFactory("arbitrage", NewArbitrageStrategyFactory())

	return registry
}

// RegisterFactory registers a strategy factory
func (r *Registry) RegisterFactory(name string, factory StrategyFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("strategy factory '%s' already registered", name)
	}

	r.factories[name] = factory
	return nil
}

// UnregisterFactory unregisters a strategy factory
func (r *Registry) UnregisterFactory(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[name]; !exists {
		return fmt.Errorf("strategy factory '%s' not found", name)
	}

	delete(r.factories, name)
	return nil
}

// CreateStrategy creates a strategy instance
func (r *Registry) CreateStrategy(name string, config *models.TradingConfig) (Strategy, error) {
	r.mu.RLock()
	factory, exists := r.factories[name]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("strategy '%s' not found", name)
	}

	strategy, err := factory(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create strategy '%s': %w", name, err)
	}

	// Register the strategy instance
	r.mu.Lock()
	r.strategies[name] = strategy
	r.mu.Unlock()

	return strategy, nil
}

// GetStrategy retrieves a strategy by name
func (r *Registry) GetStrategy(name string) (Strategy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return nil, fmt.Errorf("strategy '%s' not found", name)
	}

	return strategy, nil
}

// RemoveStrategy removes a strategy from the registry
func (r *Registry) RemoveStrategy(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return fmt.Errorf("strategy '%s' not found", name)
	}

	// Stop the strategy before removing
	if err := strategy.Stop(); err != nil {
		return fmt.Errorf("failed to stop strategy '%s': %w", name, err)
	}

	delete(r.strategies, name)
	return nil
}

// GetAllStrategies returns all registered strategies
func (r *Registry) GetAllStrategies() map[string]Strategy {
	r.mu.RLock()
	defer r.mu.RUnlock()

	strategies := make(map[string]Strategy)
	for name, strategy := range r.strategies {
		strategies[name] = strategy
	}

	return strategies
}

// GetAvailableStrategies returns all available strategy names
func (r *Registry) GetAvailableStrategies() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}

	return names
}

// GetActiveStrategies returns all active strategy names
func (r *Registry) GetActiveStrategies() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.strategies))
	for name := range r.strategies {
		names = append(names, name)
	}

	return names
}

// StopAll stops all registered strategies
func (r *Registry) StopAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for name, strategy := range r.strategies {
		if err := strategy.Stop(); err != nil {
			lastErr = fmt.Errorf("failed to stop strategy '%s': %w", name, err)
		}
	}

	// Clear all strategies
	r.strategies = make(map[string]Strategy)

	return lastErr
}
