// Package strategies provides the strategy framework for trading strategies.
package strategies

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/metrics"
)

// Sentinel errors for lifecycle calls. Returned errors keep their historical
// messages but unwrap to one of these so callers (the HTTP API) can tell a
// missing strategy from a state conflict.
var (
	ErrStrategyNotFound   = errors.New("strategy not found")
	ErrStrategyRunning    = errors.New("strategy already running")
	ErrStrategyNotRunning = errors.New("strategy not running")
	ErrStrategyHeld       = errors.New("strategy held by operator")
)

// lifecycleError carries a human message and a sentinel kind.
type lifecycleError struct {
	kind error
	msg  string
}

func (e *lifecycleError) Error() string { return e.msg }
func (e *lifecycleError) Unwrap() error { return e.kind }

func lifecycleErr(kind error, format string, args ...interface{}) error {
	return &lifecycleError{kind: kind, msg: fmt.Sprintf(format, args...)}
}

// EnhancedRegistry manages enhanced strategy registration and lifecycle
type EnhancedRegistry struct {
	strategies       map[string]EnhancedStrategy
	factories        map[string]EnhancedStrategyFactory
	indicatorSvc     *indicators.Service
	feeRates         MakerTakerFeeProvider
	limitProfitStore LimitProfitRawStateStore
	pendingBuyCancel PendingBuyCancelClient
	holds            *HoldList
	mu               sync.RWMutex
}

// NewEnhancedRegistry creates a new enhanced strategy registry
func NewEnhancedRegistry(indicatorSvc *indicators.Service) *EnhancedRegistry {
	registry := &EnhancedRegistry{
		strategies:   make(map[string]EnhancedStrategy),
		factories:    make(map[string]EnhancedStrategyFactory),
		indicatorSvc: indicatorSvc,
		holds:        NewMemoryHoldList(),
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

	// Wire up momentum Prometheus metrics
	if mom, ok := strategy.(*MomentumStrategy); ok {
		r.wireMomentumMetrics(mom)
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

// SetHoldList replaces the operator hold list (normally one loaded from
// STRATEGY_HOLD_FILE at boot). A nil list resets to an empty in-memory one.
func (r *EnhancedRegistry) SetHoldList(h *HoldList) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h == nil {
		h = NewMemoryHoldList()
	}
	r.holds = h
}

// Holds returns a copy of the operator holds, including names that are not
// (yet) registered.
func (r *EnhancedRegistry) Holds() map[string]HoldEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.holds.All()
}

// Get retrieves a strategy by name
func (r *EnhancedRegistry) Get(name string) (EnhancedStrategy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return nil, lifecycleErr(ErrStrategyNotFound, "strategy '%s' not found", name)
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

// Start starts a strategy by name. A strategy on the operator hold list is
// refused with ErrStrategyHeld; use StartReleasingHold for an operator start.
func (r *EnhancedRegistry) Start(ctx context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return lifecycleErr(ErrStrategyNotFound, "strategy '%s' not found", name)
	}

	if strategy.IsRunning() {
		return lifecycleErr(ErrStrategyRunning, "strategy '%s' is already running", name)
	}

	if h, held := r.holds.Get(name); held {
		return lifecycleErr(ErrStrategyHeld, "strategy '%s' is held by operator %q (%s); release the hold to start it", name, h.By, h.Reason)
	}

	err := strategy.Start(ctx)
	if err == nil {
		r.updatePrometheusMetrics()
	}
	return err
}

// StartReleasingHold is the operator start: it removes any hold on name
// (persisting the change) and then starts the strategy. If the start fails the
// hold is put back, so a failed attempt never leaves the strategy unprotected.
// It returns the hold that was released, if there was one.
func (r *EnhancedRegistry) StartReleasingHold(ctx context.Context, name string) (*HoldEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return nil, lifecycleErr(ErrStrategyNotFound, "strategy '%s' not found", name)
	}
	if strategy.IsRunning() {
		return nil, lifecycleErr(ErrStrategyRunning, "strategy '%s' is already running", name)
	}

	var released *HoldEntry
	if h, held := r.holds.Get(name); held {
		if err := r.holds.Delete(name); err != nil {
			return nil, fmt.Errorf("release hold on '%s': %w", name, err)
		}
		released = &h
	}

	if err := strategy.Start(ctx); err != nil {
		if released != nil {
			_ = r.holds.Put(name, *released)
		}
		return nil, err
	}
	r.updatePrometheusMetrics()
	return released, nil
}

// Stop stops a strategy by name
func (r *EnhancedRegistry) Stop(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return lifecycleErr(ErrStrategyNotFound, "strategy '%s' not found", name)
	}

	if !strategy.IsRunning() {
		return lifecycleErr(ErrStrategyNotRunning, "strategy '%s' is not running", name)
	}

	err := strategy.Stop()
	if err == nil {
		r.updatePrometheusMetrics()
	}
	return err
}

// Hold is the operator stop: it persists a hold for name first and then stops
// the strategy if it is running. Holding an already-stopped strategy is fine
// (it records the hold so nothing starts it later). It reports whether the
// strategy was running. If persisting fails nothing is changed.
func (r *EnhancedRegistry) Hold(name string, entry HoldEntry) (wasRunning bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return false, lifecycleErr(ErrStrategyNotFound, "strategy '%s' not found", name)
	}
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	}
	if err := r.holds.Put(name, entry); err != nil {
		return false, fmt.Errorf("persist hold on '%s': %w", name, err)
	}
	if !strategy.IsRunning() {
		return false, nil
	}
	if err := strategy.Stop(); err != nil {
		return true, fmt.Errorf("stop strategy '%s': %w", name, err)
	}
	r.updatePrometheusMetrics()
	return true, nil
}

// Remove removes a strategy from the registry
func (r *EnhancedRegistry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return lifecycleErr(ErrStrategyNotFound, "strategy '%s' not found", name)
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

// StartAll starts all registered strategies except those on the operator hold
// list.
func (r *EnhancedRegistry) StartAll(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for name, strategy := range r.strategies {
		if _, held := r.holds.Get(name); held {
			continue
		}
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
	// Hold is set when an operator stopped the strategy and asked it to stay
	// stopped (see HoldList).
	Hold *HoldEntry `json:"hold,omitempty"`
}

// GetStrategyInfo returns information about a strategy
func (r *EnhancedRegistry) GetStrategyInfo(name string) (*StrategyInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	strategy, exists := r.strategies[name]
	if !exists {
		return nil, lifecycleErr(ErrStrategyNotFound, "strategy '%s' not found", name)
	}

	return r.infoLocked(name, strategy), nil
}

// GetAllStrategyInfo returns information about all strategies
func (r *EnhancedRegistry) GetAllStrategyInfo() []*StrategyInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]*StrategyInfo, 0, len(r.strategies))

	for name, strategy := range r.strategies {
		infos = append(infos, r.infoLocked(name, strategy))
	}

	return infos
}

// infoLocked builds a StrategyInfo. Must be called with r.mu held.
func (r *EnhancedRegistry) infoLocked(name string, strategy EnhancedStrategy) *StrategyInfo {
	config := strategy.GetConfig()
	info := &StrategyInfo{
		Name:       strategy.Name(),
		Type:       config.Type,
		Version:    strategy.Version(),
		Book:       config.Book,
		Parameters: config.Parameters,
		Running:    strategy.IsRunning(),
		Enabled:    config.Enabled,
		State:      strategy.GetState(),
		Metrics:    strategy.GetMetrics(),
	}
	if h, held := r.holds.Get(name); held {
		info.Hold = &h
	}
	return info
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
	r.recordFeeDrift(fill)
	r.updatePrometheusMetrics()
}

// recordFeeDrift records realized fee rates from fills and, when a fee provider
// is available, the drift vs the assumed maker/taker rate (POINT-9 §7). This is
// the direct defense against the May 2026 loss where the configured liquidity
// assumption diverged from what Bitso actually charged.
func (r *EnhancedRegistry) recordFeeDrift(fill OrderFill) {
	if fill.FeeRate <= 0 || fill.Book == "" {
		return
	}
	side := strings.ToLower(strings.TrimSpace(fill.Side))
	liquidity := strings.ToLower(strings.TrimSpace(fill.Liquidity))
	if liquidity == "" {
		liquidity = "unknown"
	}

	promMetrics := metrics.GetPrometheusMetrics()
	promMetrics.RecordRealizedFeeRate(fill.Book, side, liquidity, fill.FeeRate)

	if r.feeRates == nil || liquidity == "unknown" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	maker, taker, ok := r.feeRates.MakerTakerRatesForBook(ctx, fill.Book)
	if !ok {
		return
	}
	assumed := taker
	if liquidity == "maker" {
		assumed = maker
	}
	if assumed <= 0 {
		return
	}
	promMetrics.SetAssumedFeeRate(fill.Book, liquidity, assumed)
	promMetrics.SetFeeDriftRatio(fill.Book, side, liquidity, fill.FeeRate/assumed)
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

// wireMomentumMetrics injects Prometheus metric callbacks into a momentum strategy.
func (r *EnhancedRegistry) wireMomentumMetrics(mom *MomentumStrategy) {
	promMetrics := metrics.GetPrometheusMetrics()

	mom.SetMetrics(&MomentumMetrics{
		EntrySignals: func(strategy, book, side string) {
			promMetrics.IncMomentumEntrySignals(strategy, book, side)
		},
		ExitSignals: func(strategy, book, reason string) {
			promMetrics.IncMomentumExitSignals(strategy, book, reason)
		},
		PositionHoldDuration: func(strategy, book string, seconds float64) {
			promMetrics.RecordMomentumPositionHoldDuration(strategy, book, seconds)
		},
		DailyRealizedPnL: func(strategy, book string, value float64) {
			promMetrics.SetMomentumDailyRealizedPnL(strategy, book, value)
		},
		CircuitBreakerActive: func(strategy, book string, active bool) {
			promMetrics.SetMomentumCircuitBreakerActive(strategy, book, active)
		},
	})
}
