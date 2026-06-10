// Package strategies provides the strategy framework for trading strategies.
package strategies

import (
	"context"
	"fmt"
	"sync"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/metrics"
)

// EnhancedRegistry manages enhanced strategy registration and lifecycle
type EnhancedRegistry struct {
	strategies       map[string]EnhancedStrategy
	factories        map[string]EnhancedStrategyFactory
	indicatorSvc     *indicators.Service
	feeRates         MakerTakerFeeProvider
	limitProfitStore LimitProfitRawStateStore
	pendingBuyCancel PendingBuyCancelClient
	mu               sync.RWMutex
}

// NewEnhancedRegistry creates a new enhanced strategy registry
func NewEnhancedRegistry(indicatorSvc *indicators.Service) *EnhancedRegistry {
	registry := &EnhancedRegistry{
		strategies:   make(map[string]EnhancedStrategy),
		factories:    make(map[string]EnhancedStrategyFactory),
		indicatorSvc: indicatorSvc,
	}

	registry.registerBuiltInFactories()

	return registry
}

// registerBuiltInFactories registers built-in strategy factories
func (r *EnhancedRegistry) registerBuiltInFactories() {
	r.factories["mean_reversion"] = NewMeanReversionStrategyFactory()
	r.factories["momentum"] = NewMomentumStrategyFactory()
	r.factories["limit_profit"] = NewLimitProfitStrategyFactory()
}

// RegisterFactory registers a strategy factory
func (r *EnhancedRegistry) RegisterFactory(strategyType string, factory EnhancedStrategyFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[strategyType]; exists {
		return fmt.Errorf("strategy factory '%s' already registered", strategyType)
	}

	r.factories[strategyType] = factory
	return nil
}

// CreateAndRegister creates a strategy from config and registers it
func (r *EnhancedRegistry) CreateAndRegister(config StrategyConfig) (EnhancedStrategy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	factory, exists := r.factories[config.Type]
	if !exists {
		return nil, fmt.Errorf("strategy type '%s' not found", config.Type)
	}

	strategy := factory()

	if err := strategy.Initialize(config, r.indicatorSvc); err != nil {
		return nil, fmt.Errorf("initialize strategy '%s': %w", config.Name, err)
	}

	if r.feeRates != nil {
		if inj, ok := strategy.(feeRatesInjectable); ok {
			inj.SetFeeRatesProvider(r.feeRates)
		}
	}

	if r.limitProfitStore != nil {
		if lp, ok := strategy.(*LimitProfitStrategy); ok {
			lp.SetLimitProfitRawStateStore(r.limitProfitStore)
		}
	}
	if r.pendingBuyCancel != nil {
		if lp, ok := strategy.(*LimitProfitStrategy); ok {
			lp.SetPendingBuyCancelClient(r.pendingBuyCancel)
		}
	}

	// Wire up limit_profit Prometheus metrics
	if lp, ok := strategy.(*LimitProfitStrategy); ok {
		r.wireLimitProfitMetrics(lp)
	}

	r.strategies[config.Name] = strategy

	return strategy, nil
}

// SetLimitProfitRawStateStore registers Redis (or compatible) persistence for limit_profit strategies.
func (r *EnhancedRegistry) SetLimitProfitRawStateStore(store LimitProfitRawStateStore) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.limitProfitStore = store
	for _, strategy := range r.strategies {
		if lp, ok := strategy.(*LimitProfitStrategy); ok {
			lp.SetLimitProfitRawStateStore(store)
		}
	}
}

// SetFeeRatesProvider registers a Bitso (or compatible) fee source for strategies that support it.
// Existing running strategy instances are updated immediately.
func (r *EnhancedRegistry) SetFeeRatesProvider(p MakerTakerFeeProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.feeRates = p
	for _, strategy := range r.strategies {
		if inj, ok := strategy.(feeRatesInjectable); ok {
			inj.SetFeeRatesProvider(p)
		}
	}
}

// SetPendingBuyCancelClient registers order-management cancel RPC for limit_profit pending-buy timeout.
func (r *EnhancedRegistry) SetPendingBuyCancelClient(c PendingBuyCancelClient) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pendingBuyCancel = c
	for _, strategy := range r.strategies {
		if lp, ok := strategy.(*LimitProfitStrategy); ok {
			lp.SetPendingBuyCancelClient(c)
		}
	}
}

// Get retrieves a strategy by name
func (r *EnhancedRegistry) Get(name string) (EnhancedStrategy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return nil, fmt.Errorf("strategy '%s' not found", name)
	}

	return strategy, nil
}

// GetAll returns all registered strategies
func (r *EnhancedRegistry) GetAll() map[string]EnhancedStrategy {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]EnhancedStrategy)
	for name, strategy := range r.strategies {
		result[name] = strategy
	}

	return result
}

// GetAvailableTypes returns all available strategy types
func (r *EnhancedRegistry) GetAvailableTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}

	return types
}

// GetActiveStrategies returns names of all active (running) strategies
func (r *EnhancedRegistry) GetActiveStrategies() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0)
	for name, strategy := range r.strategies {
		if strategy.IsRunning() {
			names = append(names, name)
		}
	}

	return names
}

// Start starts a strategy by name
func (r *EnhancedRegistry) Start(ctx context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return fmt.Errorf("strategy '%s' not found", name)
	}

	if strategy.IsRunning() {
		return fmt.Errorf("strategy '%s' is already running", name)
	}

	err := strategy.Start(ctx)
	if err == nil {
		r.updatePrometheusMetrics()
	}
	return err
}

// Stop stops a strategy by name
func (r *EnhancedRegistry) Stop(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return fmt.Errorf("strategy '%s' not found", name)
	}

	if !strategy.IsRunning() {
		return fmt.Errorf("strategy '%s' is not running", name)
	}

	err := strategy.Stop()
	if err == nil {
		r.updatePrometheusMetrics()
	}
	return err
}

// Remove removes a strategy from the registry
func (r *EnhancedRegistry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return fmt.Errorf("strategy '%s' not found", name)
	}

	if strategy.IsRunning() {
		if err := strategy.Stop(); err != nil {
			return fmt.Errorf("stop strategy '%s': %w", name, err)
		}
	}

	strategyName := strategy.Name()
	cfg := strategy.GetConfig()
	if r.limitProfitStore != nil && cfg.Type == "limit_profit" {
		_ = r.limitProfitStore.Delete(context.Background(), strategyName)
	}
	delete(r.strategies, name)

	promMetrics := metrics.GetPrometheusMetrics()
	promMetrics.RemoveStrategy(strategyName)
	r.updatePrometheusMetrics()

	return nil
}

// StartAll starts all registered strategies
func (r *EnhancedRegistry) StartAll(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for name, strategy := range r.strategies {
		if !strategy.IsRunning() {
			if err := strategy.Start(ctx); err != nil {
				lastErr = fmt.Errorf("start strategy '%s': %w", name, err)
			}
		}
	}

	return lastErr
}

// StopAll stops all registered strategies
func (r *EnhancedRegistry) StopAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for name, strategy := range r.strategies {
		if strategy.IsRunning() {
			if err := strategy.Stop(); err != nil {
				lastErr = fmt.Errorf("stop strategy '%s': %w", name, err)
			}
		}
	}

	return lastErr
}

// ProcessTick sends a tick to all running strategies and collects signals
func (r *EnhancedRegistry) ProcessTick(tick *indicators.Trade, book string) ([]*Signal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var signals []*Signal

	for _, strategy := range r.strategies {
		if !strategy.IsRunning() {
			continue
		}

		config := strategy.GetConfig()
		if config.Book != book {
			continue
		}

		signal, err := strategy.OnTick(tick)
		if err != nil {
			continue
		}

		if signal != nil {
			EnsureSignalStrategy(signal, strategy.Name())
			signals = append(signals, signal)
			promMetrics := metrics.GetPrometheusMetrics()
			promMetrics.IncSignalsGenerated(strategy.Name(), signal.Side)
			r.updatePrometheusMetrics()
		}
	}

	return signals, nil
}

// ProcessBar sends a bar to all running strategies and collects signals
func (r *EnhancedRegistry) ProcessBar(bar *indicators.OHLCV, book string) ([]*Signal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var signals []*Signal

	for _, strategy := range r.strategies {
		if !strategy.IsRunning() {
			continue
		}

		config := strategy.GetConfig()
		if config.Book != book {
			continue
		}

		signal, err := strategy.OnBar(bar)
		if err != nil {
			continue
		}

		if signal != nil {
			EnsureSignalStrategy(signal, strategy.Name())
			signals = append(signals, signal)
		}
	}

	return signals, nil
}

// StrategyInfo contains strategy status information
type StrategyInfo struct {
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Version    string                 `json:"version"`
	Book       string                 `json:"book"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Running    bool                   `json:"running"`
	Enabled    bool                   `json:"enabled"`
	State      StrategyState          `json:"state"`
	Metrics    StrategyMetrics        `json:"metrics"`
}

// GetStrategyInfo returns information about a strategy
func (r *EnhancedRegistry) GetStrategyInfo(name string) (*StrategyInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return nil, fmt.Errorf("strategy '%s' not found", name)
	}

	config := strategy.GetConfig()

	return &StrategyInfo{
		Name:       strategy.Name(),
		Type:       config.Type,
		Version:    strategy.Version(),
		Book:       config.Book,
		Parameters: config.Parameters,
		Running:    strategy.IsRunning(),
		Enabled:    config.Enabled,
		State:      strategy.GetState(),
		Metrics:    strategy.GetMetrics(),
	}, nil
}

// GetAllStrategyInfo returns information about all strategies
func (r *EnhancedRegistry) GetAllStrategyInfo() []*StrategyInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]*StrategyInfo, 0, len(r.strategies))

	for _, strategy := range r.strategies {
		config := strategy.GetConfig()
		infos = append(infos, &StrategyInfo{
			Name:       strategy.Name(),
			Type:       config.Type,
			Version:    strategy.Version(),
			Book:       config.Book,
			Parameters: config.Parameters,
			Running:    strategy.IsRunning(),
			Enabled:    config.Enabled,
			State:      strategy.GetState(),
			Metrics:    strategy.GetMetrics(),
		})
	}

	return infos
}

// RegistryStats contains registry statistics
type RegistryStats struct {
	TotalStrategies  int       `json:"total_strategies"`
	ActiveStrategies int       `json:"active_strategies"`
	AvailableTypes   []string  `json:"available_types"`
	LastUpdated      time.Time `json:"last_updated"`
}

// GetStats returns registry statistics
func (r *EnhancedRegistry) GetStats() *RegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	active := 0
	for _, strategy := range r.strategies {
		if strategy.IsRunning() {
			active++
		}
	}

	return &RegistryStats{
		TotalStrategies:  len(r.strategies),
		ActiveStrategies: active,
		AvailableTypes:   r.GetAvailableTypes(),
		LastUpdated:      time.Now(),
	}
}

// updatePrometheusMetrics updates all Prometheus metrics for strategies
// NOTE: Must be called with r.mu held (either Lock or RLock)
func (r *EnhancedRegistry) updatePrometheusMetrics() {
	promMetrics := metrics.GetPrometheusMetrics()

	activeCount := 0
	for _, strategy := range r.strategies {
		strategyName := strategy.Name()
		running := strategy.IsRunning()

		promMetrics.SetStrategyRunning(strategyName, running)

		if running {
			activeCount++
		}

		state := strategy.GetState()
		strategyMetrics := strategy.GetMetrics()

		promMetrics.SetStrategyWinRate(strategyName, strategyMetrics.WinRate)
		promMetrics.SetStrategyPnL(strategyName, strategyMetrics.TotalPnL)
		promMetrics.SetStrategyConsecutiveLosses(strategyName, state.ConsecutiveLoss)
	}

	promMetrics.SetActiveStrategies(activeCount)
}

// UpdateMetricsForSignal updates metrics when a signal is generated
func (r *EnhancedRegistry) UpdateMetricsForSignal(strategyName, side string) {
	promMetrics := metrics.GetPrometheusMetrics()
	promMetrics.IncSignalsGenerated(strategyName, side)

	r.mu.RLock()
	defer r.mu.RUnlock()
	r.updatePrometheusMetrics()
}

// NotifyOrderFilled delivers a fill to strategies that implement OrderFillAware (e.g. limit_profit BUY fills).
func (r *EnhancedRegistry) NotifyOrderFilled(fill OrderFill) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, strategy := range r.strategies {
		fillAware, ok := strategy.(OrderFillAware)
		if !ok || !strategy.IsRunning() {
			continue
		}
		fillAware.OnOrderFilled(fill)
	}
	r.updatePrometheusMetrics()
}

// wireLimitProfitMetrics injects Prometheus metric callbacks into a limit_profit strategy.
func (r *EnhancedRegistry) wireLimitProfitMetrics(lp *LimitProfitStrategy) {
	promMetrics := metrics.GetPrometheusMetrics()

	lp.SetMetrics(&LimitProfitMetrics{
		EntrySignals: func(strategy, book string) {
			promMetrics.IncLimitProfitEntrySignals(strategy, book)
		},
		ExitSignals: func(strategy, book, reason string) {
			promMetrics.IncLimitProfitExitSignals(strategy, book, reason)
		},
		PendingBuyDuration: func(strategy, book string, seconds float64) {
			promMetrics.RecordLimitProfitPendingBuyDuration(strategy, book, seconds)
		},
		PositionHoldDuration: func(strategy, book string, seconds float64) {
			promMetrics.RecordLimitProfitPositionHoldDuration(strategy, book, seconds)
		},
		PendingCancelFailures: func(strategy, book string) {
			promMetrics.IncLimitProfitPendingCancelFailures(strategy, book)
		},
		DailyRealizedPnL: func(strategy, book string, value float64) {
			promMetrics.SetLimitProfitDailyRealizedPnL(strategy, book, value)
		},
		CircuitBreakerActive: func(strategy, book string, active bool) {
			promMetrics.SetLimitProfitCircuitBreakerActive(strategy, book, active)
		},
	})
}
