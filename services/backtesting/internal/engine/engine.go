package engine

import (
	"context"
	"fmt"
	"sync"

	"bitso-trading-platform/backtesting/internal/data"
	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/metrics"
	"bitso-trading-platform/backtesting/internal/models"
	"bitso-trading-platform/backtesting/internal/storage"
)

// BacktestEngine defines the interface for running backtests
type BacktestEngine interface {
	// Run executes a backtest with the given configuration
	Run(ctx context.Context, config *models.BacktestConfig) (*models.BacktestResult, error)
	
	// Cancel cancels a running backtest
	Cancel(backtestID string) error
	
	// GetProgress returns the progress of a running backtest
	GetProgress(backtestID string) (float64, error)
}

// Engine implements the BacktestEngine interface
type Engine struct {
	dataProvider     data.DataProvider
	resultStorage    storage.ResultStorage
	logger           logger.Logger
	metricsCollector *metrics.MetricsCollector
	
	runningBacktests map[string]*runningBacktest
	mu               sync.RWMutex
}

// runningBacktest tracks a running backtest
type runningBacktest struct {
	ID       string
	Config   *models.BacktestConfig
	Progress float64
	Cancel   context.CancelFunc
}

// NewEngine creates a new backtest engine
func NewEngine(
	dataProvider data.DataProvider,
	resultStorage storage.ResultStorage,
	log logger.Logger,
	metricsCollector *metrics.MetricsCollector,
) *Engine {
	return &Engine{
		dataProvider:     dataProvider,
		resultStorage:    resultStorage,
		logger:           log,
		metricsCollector: metricsCollector,
		runningBacktests: make(map[string]*runningBacktest),
	}
}

// Run executes a backtest
func (e *Engine) Run(ctx context.Context, config *models.BacktestConfig) (*models.BacktestResult, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	
	e.logger.Info("Starting backtest", map[string]interface{}{
		"backtest_id": config.ID,
		"name":        config.Name,
		"book":        config.Book,
		"strategy":    config.Strategy,
	})
	
	// Record metrics
	if e.metricsCollector != nil {
		defer e.metricsCollector.StartBacktestTimer()("completed")
	}
	
	// Create cancellable context
	backtestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	
	// Track running backtest
	e.trackBacktest(config.ID, config, 0.0, cancel)
	defer e.untrackBacktest(config.ID)
	
	// Create and run backtest runner
	runner, err := NewBacktestRunner(config, e.dataProvider, e.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}
	
	// Set progress callback
	runner.SetProgressCallback(func(progress float64) {
		e.updateProgress(config.ID, progress)
	})
	
	// Initialize runner
	if err := runner.Initialize(backtestCtx); err != nil {
		return nil, fmt.Errorf("failed to initialize runner: %w", err)
	}
	
	// Execute backtest
	result, err := runner.Execute(backtestCtx)
	if err != nil {
		return nil, fmt.Errorf("backtest execution failed: %w", err)
	}
	
	// Save result
	if e.resultStorage != nil {
		if err := e.resultStorage.Save(ctx, result); err != nil {
			e.logger.Error("Failed to save result", map[string]interface{}{"error": err})
		}
	}
	
	e.logger.Info("Backtest completed", map[string]interface{}{
		"backtest_id": config.ID,
		"status":      result.Status,
		"trades":      len(result.Trades),
	})
	
	return result, nil
}

// Cancel cancels a running backtest
func (e *Engine) Cancel(backtestID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	running, exists := e.runningBacktests[backtestID]
	if !exists {
		return fmt.Errorf("backtest not found: %s", backtestID)
	}
	
	e.logger.Info("Cancelling backtest", map[string]interface{}{
		"backtest_id": backtestID,
	})
	
	running.Cancel()
	delete(e.runningBacktests, backtestID)
	
	return nil
}

// GetProgress returns the progress of a running backtest
func (e *Engine) GetProgress(backtestID string) (float64, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	running, exists := e.runningBacktests[backtestID]
	if !exists {
		return 0, fmt.Errorf("backtest not found: %s", backtestID)
	}
	
	return running.Progress, nil
}

// trackBacktest tracks a running backtest
func (e *Engine) trackBacktest(id string, config *models.BacktestConfig, progress float64, cancel context.CancelFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	e.runningBacktests[id] = &runningBacktest{
		ID:       id,
		Config:   config,
		Progress: progress,
		Cancel:   cancel,
	}
	
	if e.metricsCollector != nil {
		e.metricsCollector.RecordActiveBacktests(len(e.runningBacktests))
	}
}

// untrackBacktest stops tracking a backtest
func (e *Engine) untrackBacktest(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	delete(e.runningBacktests, id)
	
	if e.metricsCollector != nil {
		e.metricsCollector.RecordActiveBacktests(len(e.runningBacktests))
	}
}

// updateProgress updates the progress of a running backtest
func (e *Engine) updateProgress(id string, progress float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if running, exists := e.runningBacktests[id]; exists {
		running.Progress = progress
	}
}

