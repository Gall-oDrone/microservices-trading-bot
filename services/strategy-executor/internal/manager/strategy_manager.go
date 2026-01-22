package manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/strategy-executor/internal/logger"
	"bitso-trading-platform/strategy-executor/internal/metrics"
	"bitso-trading-platform/strategy-executor/internal/processor"
	"bitso-trading-platform/strategy-executor/internal/risk"
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

// StrategyManager manages strategy execution and lifecycle
type StrategyManager interface {
	Start(ctx context.Context) error
	Stop() error
	StartStrategy(name string, config *models.TradingConfig) error
	StopStrategy(name string) error
	GetStrategyStatus(name string) (*StrategyStatus, error)
	UpdateStrategyConfig(name string, config *models.TradingConfig) error
	GetAllStatuses() map[string]*StrategyStatus
	ProcessMarketData(event *processor.ProcessedEvent) error
}

// Manager implements StrategyManager
type Manager struct {
	logger   *logger.Logger
	metrics  *metrics.Metrics
	registry *strategies.Registry
	riskMgr  *risk.Manager

	// Strategy execution state
	strategyStates map[string]*StrategyState
	mu             sync.RWMutex

	// Signal channels
	signalChannels map[string]chan *strategies.TradingSignal
	signalsMu      sync.RWMutex

	// Control channels
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// StrategyState tracks the state of a strategy
type StrategyState struct {
	Name          string
	Strategy      strategies.Strategy
	Config        *models.TradingConfig
	Status        StrategyStatusType
	StartTime     time.Time
	LastExecution time.Time
	Executions    int64
	Signals       int64
	Errors        int64
	LastError     string
}

// StrategyStatus represents the status of a strategy
type StrategyStatus struct {
	Name          string             `json:"name"`
	Status        StrategyStatusType `json:"status"`
	Book          string             `json:"book"`
	StartTime     time.Time          `json:"start_time"`
	LastExecution time.Time          `json:"last_execution"`
	Executions    int64              `json:"executions"`
	Signals       int64              `json:"signals"`
	Errors        int64              `json:"errors"`
	LastError     string             `json:"last_error,omitempty"`
}

// StrategyStatusType represents the status type
type StrategyStatusType string

const (
	StrategyStatusActive   StrategyStatusType = "active"
	StrategyStatusInactive StrategyStatusType = "inactive"
	StrategyStatusError    StrategyStatusType = "error"
	StrategyStatusStopping StrategyStatusType = "stopping"
)

// NewManager creates a new strategy manager
func NewManager(
	logger *logger.Logger,
	metrics *metrics.Metrics,
	registry *strategies.Registry,
	riskMgr *risk.Manager,
) *Manager {
	return &Manager{
		logger:         logger,
		metrics:        metrics,
		registry:       registry,
		riskMgr:        riskMgr,
		strategyStates: make(map[string]*StrategyState),
		signalChannels: make(map[string]chan *strategies.TradingSignal),
		stopChan:       make(chan struct{}),
	}
}

// Start starts the strategy manager
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting strategy manager...")

	// Start signal processor
	m.wg.Add(1)
	go m.processSignals(ctx)

	m.logger.Info("Strategy manager started successfully")
	return nil
}

// Stop stops the strategy manager
func (m *Manager) Stop() error {
	m.logger.Info("Stopping strategy manager...")

	// Signal stop
	close(m.stopChan)

	// Stop all strategies
	if err := m.stopAllStrategies(); err != nil {
		m.logger.Errorf("Error stopping strategies: %v", err)
	}

	// Wait for goroutines
	m.wg.Wait()

	m.logger.Info("Strategy manager stopped")
	return nil
}

// StartStrategy starts a strategy with the given configuration
func (m *Manager) StartStrategy(name string, config *models.TradingConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if strategy is already running
	if _, exists := m.strategyStates[name]; exists {
		return fmt.Errorf("strategy '%s' is already running", name)
	}

	// Create strategy instance
	strategy, err := m.registry.CreateStrategy(name, config)
	if err != nil {
		return fmt.Errorf("failed to create strategy: %w", err)
	}

	// Create strategy state
	state := &StrategyState{
		Name:      name,
		Strategy:  strategy,
		Config:    config,
		Status:    StrategyStatusActive,
		StartTime: time.Now(),
	}

	m.strategyStates[name] = state

	// Create signal channels
	signalChan := make(chan *strategies.TradingSignal, 100)
	m.signalsMu.Lock()
	m.signalChannels[name] = signalChan
	m.signalsMu.Unlock()

	// Start monitoring signals from strategy
	m.wg.Add(2)
	go m.monitorBuySignals(name, strategy.GetBuySignalChannel())
	go m.monitorSellSignals(name, strategy.GetSellSignalChannel())

	m.logger.Infof("Strategy '%s' started successfully", name)
	m.metrics.RecordActiveStrategies(len(m.strategyStates))

	return nil
}

// StopStrategy stops a running strategy
func (m *Manager) StopStrategy(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.strategyStates[name]
	if !exists {
		return fmt.Errorf("strategy '%s' not found", name)
	}

	// Update status
	state.Status = StrategyStatusStopping

	// Remove from registry (this will also stop the strategy)
	if err := m.registry.RemoveStrategy(name); err != nil {
		state.Status = StrategyStatusError
		state.LastError = err.Error()
		return fmt.Errorf("failed to remove strategy: %w", err)
	}

	// Close signal channel
	m.signalsMu.Lock()
	if signalChan, exists := m.signalChannels[name]; exists {
		close(signalChan)
		delete(m.signalChannels, name)
	}
	m.signalsMu.Unlock()

	// Remove state
	delete(m.strategyStates, name)

	m.logger.Infof("Strategy '%s' stopped successfully", name)
	m.metrics.RecordActiveStrategies(len(m.strategyStates))

	return nil
}

// GetStrategyStatus returns the status of a strategy
func (m *Manager) GetStrategyStatus(name string) (*StrategyStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.strategyStates[name]
	if !exists {
		return nil, fmt.Errorf("strategy '%s' not found", name)
	}

	return &StrategyStatus{
		Name:          state.Name,
		Status:        state.Status,
		Book:          state.Config.Book.String(),
		StartTime:     state.StartTime,
		LastExecution: state.LastExecution,
		Executions:    state.Executions,
		Signals:       state.Signals,
		Errors:        state.Errors,
		LastError:     state.LastError,
	}, nil
}

// UpdateStrategyConfig updates a strategy's configuration
func (m *Manager) UpdateStrategyConfig(name string, config *models.TradingConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.strategyStates[name]
	if !exists {
		return fmt.Errorf("strategy '%s' not found", name)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Update configuration
	state.Config = config

	m.logger.Infof("Strategy '%s' configuration updated", name)
	return nil
}

// GetAllStatuses returns the status of all strategies
func (m *Manager) GetAllStatuses() map[string]*StrategyStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make(map[string]*StrategyStatus)
	for name, state := range m.strategyStates {
		statuses[name] = &StrategyStatus{
			Name:          state.Name,
			Status:        state.Status,
			Book:          state.Config.Book.String(),
			StartTime:     state.StartTime,
			LastExecution: state.LastExecution,
			Executions:    state.Executions,
			Signals:       state.Signals,
			Errors:        state.Errors,
			LastError:     state.LastError,
		}
	}

	return statuses
}

// ProcessMarketData processes market data and executes strategies
func (m *Manager) ProcessMarketData(event *processor.ProcessedEvent) error {
	// Convert event to ticker format
	ticker, err := m.convertEventToTicker(event)
	if err != nil {
		return fmt.Errorf("failed to convert event to ticker: %w", err)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Execute all active strategies
	for name, state := range m.strategyStates {
		if state.Status != StrategyStatusActive {
			continue
		}

		// Execute strategy
		start := time.Now()
		if err := m.executeStrategy(name, state, ticker); err != nil {
			m.logger.Errorf("Strategy '%s' execution failed: %v", name, err)
			state.Errors++
			state.LastError = err.Error()
			state.Status = StrategyStatusError
			m.metrics.RecordStrategyError(name, ticker.Book.String(), "execution_error")
		} else {
			state.Executions++
			state.LastExecution = time.Now()
			m.metrics.RecordStrategyExecution(name, ticker.Book.String(), time.Since(start))
		}
	}

	return nil
}

// executeStrategy executes a strategy with the given ticker
func (m *Manager) executeStrategy(name string, state *StrategyState, ticker *bitso.Ticker) error {
	// Check if the book matches
	if ticker.Book.String() != state.Config.Book.String() {
		return nil // Skip if book doesn't match
	}

	// Check trading hours
	if err := m.riskMgr.CheckTradingHours(); err != nil {
		return nil // Skip if outside trading hours
	}

	// Execute the strategy
	if err := state.Strategy.Execute(ticker); err != nil {
		return fmt.Errorf("strategy execution failed: %w", err)
	}

	return nil
}

// monitorBuySignals monitors buy signals from a strategy
func (m *Manager) monitorBuySignals(strategyName string, signalChan chan strategies.TradingSignal) {
	defer m.wg.Done()

	for {
		select {
		case <-m.stopChan:
			return
		case signal, ok := <-signalChan:
			if !ok {
				return
			}

			// Process buy signal
			m.processBuySignal(strategyName, &signal)
		}
	}
}

// monitorSellSignals monitors sell signals from a strategy
func (m *Manager) monitorSellSignals(strategyName string, signalChan chan strategies.TradingSignal) {
	defer m.wg.Done()

	for {
		select {
		case <-m.stopChan:
			return
		case signal, ok := <-signalChan:
			if !ok {
				return
			}

			// Process sell signal
			m.processSellSignal(strategyName, &signal)
		}
	}
}

// processBuySignal processes a buy signal
func (m *Manager) processBuySignal(strategyName string, signal *strategies.TradingSignal) {
	m.logger.Infof("Buy signal from strategy '%s': %s", strategyName, signal.Reason)

	// Validate with risk manager
	if err := m.riskMgr.ValidateTradeAmount(signal.Amount, signal.Price); err != nil {
		m.logger.Warnf("Buy signal rejected by risk manager: %v", err)
		m.metrics.RecordRiskViolation("trade_amount")
		return
	}

	// Update state
	m.mu.Lock()
	if state, exists := m.strategyStates[strategyName]; exists {
		state.Signals++
	}
	m.mu.Unlock()

	// Send to signal channel
	m.signalsMu.RLock()
	if signalChan, exists := m.signalChannels[strategyName]; exists {
		select {
		case signalChan <- signal:
			m.metrics.RecordStrategySignal(strategyName, signal.Book.String(), "buy")
		default:
			m.logger.Warn("Signal channel full, dropping signal")
		}
	}
	m.signalsMu.RUnlock()
}

// processSellSignal processes a sell signal
func (m *Manager) processSellSignal(strategyName string, signal *strategies.TradingSignal) {
	m.logger.Infof("Sell signal from strategy '%s': %s", strategyName, signal.Reason)

	// Validate with risk manager
	if err := m.riskMgr.ValidateTradeAmount(signal.Amount, signal.Price); err != nil {
		m.logger.Warnf("Sell signal rejected by risk manager: %v", err)
		m.metrics.RecordRiskViolation("trade_amount")
		return
	}

	// Update state
	m.mu.Lock()
	if state, exists := m.strategyStates[strategyName]; exists {
		state.Signals++
	}
	m.mu.Unlock()

	// Send to signal channel
	m.signalsMu.RLock()
	if signalChan, exists := m.signalChannels[strategyName]; exists {
		select {
		case signalChan <- signal:
			m.metrics.RecordStrategySignal(strategyName, signal.Book.String(), "sell")
		default:
			m.logger.Warn("Signal channel full, dropping signal")
		}
	}
	m.signalsMu.RUnlock()
}

// processSignals processes signals from all strategies
func (m *Manager) processSignals(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Process pending signals
			m.processPendingSignals()
		}
	}
}

// processPendingSignals processes any pending signals
func (m *Manager) processPendingSignals() {
	m.signalsMu.RLock()
	defer m.signalsMu.RUnlock()

	for strategyName, signalChan := range m.signalChannels {
		select {
		case signal, ok := <-signalChan:
			if !ok {
				continue
			}

			// Log signal for now
			m.logger.Debugf("Processing signal from strategy '%s': %s", strategyName, signal.Reason)
		default:
			// No signals pending
		}
	}
}

// stopAllStrategies stops all running strategies
func (m *Manager) stopAllStrategies() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for name := range m.strategyStates {
		if err := m.StopStrategy(name); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// convertEventToTicker converts a processed event to a ticker
func (m *Manager) convertEventToTicker(event *processor.ProcessedEvent) (*bitso.Ticker, error) {
	// Parse book from string
	book := &bitso.Book{}
	if err := book.UnmarshalJSON([]byte(`"` + event.Book + `"`)); err != nil {
		return nil, fmt.Errorf("invalid book: %s", event.Book)
	}

	// Extract ticker data from event metadata
	ticker := &bitso.Ticker{
		Book: *book,
	}

	// Extract bid, ask, last from metadata
	if bid, ok := event.Metadata["bid"].(float64); ok {
		ticker.Bid = bitso.ToMonetary(bid)
	}

	if ask, ok := event.Metadata["ask"].(float64); ok {
		ticker.Ask = bitso.ToMonetary(ask)
	}

	if last, ok := event.Metadata["last"].(float64); ok {
		ticker.Last = bitso.ToMonetary(last)
	}

	// Use price from metadata if available (for trade events)
	if price, ok := event.Metadata["price"].(float64); ok {
		monetary := bitso.ToMonetary(price)
		// Check if values are empty (not set)
		if string(ticker.Last) == "" {
			ticker.Last = monetary
		}
		if string(ticker.Bid) == "" {
			ticker.Bid = monetary
		}
		if string(ticker.Ask) == "" {
			ticker.Ask = monetary
		}
	}

	return ticker, nil
}

// GetSignalChannel returns the signal channel for a strategy
func (m *Manager) GetSignalChannel(strategyName string) (chan *strategies.TradingSignal, error) {
	m.signalsMu.RLock()
	defer m.signalsMu.RUnlock()

	signalChan, exists := m.signalChannels[strategyName]
	if !exists {
		return nil, fmt.Errorf("strategy '%s' not found", strategyName)
	}

	return signalChan, nil
}
