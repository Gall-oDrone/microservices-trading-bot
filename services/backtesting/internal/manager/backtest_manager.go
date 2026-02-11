package manager

import (
	"context"
	"fmt"
	"sync"

	"bitso-trading-platform/backtesting/internal/engine"
	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/metrics"
	"bitso-trading-platform/backtesting/internal/models"
	"bitso-trading-platform/backtesting/internal/storage"
)

// BacktestManager manages the lifecycle of backtests
type BacktestManager struct {
	engine           engine.BacktestEngine
	queue            *BacktestQueue
	storage          storage.ResultStorage
	logger           logger.Logger
	metricsCollector *metrics.MetricsCollector

	maxConcurrent    int
	runningBacktests map[string]*models.Backtest
	createdBacktests map[string]*models.Backtest // pending backtests not yet started
	mu               sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

// NewBacktestManager creates a new backtest manager
func NewBacktestManager(
	eng engine.BacktestEngine,
	stor storage.ResultStorage,
	maxConcurrent int,
	log logger.Logger,
	metricsCollector *metrics.MetricsCollector,
) *BacktestManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &BacktestManager{
		engine:           eng,
		queue:            NewBacktestQueue(maxConcurrent * 2), // Queue size = 2x concurrent
		storage:          stor,
		logger:           log,
		metricsCollector: metricsCollector,
		maxConcurrent:    maxConcurrent,
		runningBacktests: make(map[string]*models.Backtest),
		createdBacktests: make(map[string]*models.Backtest),
		ctx:              ctx,
		cancel:           cancel,
	}
}

// CreateBacktest creates a new backtest
func (m *BacktestManager) CreateBacktest(config *models.BacktestConfig) (*models.Backtest, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Create backtest
	backtest := models.NewBacktest(config)

	m.mu.Lock()
	m.createdBacktests[backtest.ID] = backtest
	m.mu.Unlock()

	// Record metrics
	if m.metricsCollector != nil {
		m.metricsCollector.RecordBacktestCreated()
	}

	m.logger.Info("Backtest created", map[string]interface{}{
		"backtest_id": backtest.ID,
		"name":        config.Name,
	})

	return backtest, nil
}

// StartBacktest starts a backtest (queues it for execution)
func (m *BacktestManager) StartBacktest(backtestID string) error {
	// Check if we can start new backtest
	if !m.canStartNewBacktest() {
		// Queue the backtest
		if err := m.queue.Enqueue(backtestID); err != nil {
			return fmt.Errorf("failed to queue backtest: %w", err)
		}

		m.logger.Info("Backtest queued", map[string]interface{}{
			"backtest_id": backtestID,
			"queue_size":  m.queue.Size(),
		})

		return nil
	}

	// Start immediately
	return m.startBacktestNow(backtestID)
}

// GetBacktest retrieves a backtest by ID
func (m *BacktestManager) GetBacktest(backtestID string) (*models.Backtest, error) {
	// Check running backtests first (and refresh progress from engine)
	if bt, err := m.getTrackedBacktest(backtestID); err == nil {
		if progress, err := m.engine.GetProgress(backtestID); err == nil {
			bt.UpdateProgress(progress)
		}
		return bt, nil
	}

	// Check created (pending) backtests
	m.mu.RLock()
	bt := m.createdBacktests[backtestID]
	m.mu.RUnlock()
	if bt != nil {
		return bt, nil
	}

	// Check storage
	result, err := m.storage.Get(m.ctx, backtestID)
	if err != nil {
		return nil, fmt.Errorf("backtest not found: %s", backtestID)
	}

	// Convert result to backtest (include progress and times for API response)
	backtest := &models.Backtest{
		ID:        backtestID,
		Status:    models.BacktestStatus(result.Status),
		Progress:  result.Progress,
		Result:    result,
		StartedAt: &result.StartedAt,
	}
	if result.CompletedAt != nil {
		backtest.CompletedAt = result.CompletedAt
	}

	return backtest, nil
}

// GetBacktestResult retrieves backtest results
func (m *BacktestManager) GetBacktestResult(backtestID string) (*models.BacktestResult, error) {
	return m.storage.Get(m.ctx, backtestID)
}

// ListBacktests lists backtests with filters
func (m *BacktestManager) ListBacktests(filters *storage.ListFilters) ([]*models.Backtest, error) {
	results, err := m.storage.List(m.ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list backtests: %w", err)
	}

	// Convert results to backtests
	backtests := make([]*models.Backtest, len(results))
	for i, result := range results {
		backtests[i] = &models.Backtest{
			ID:     result.BacktestID,
			Status: models.BacktestStatus(result.Status),
			Result: result,
		}
	}

	return backtests, nil
}

// CancelBacktest cancels a running backtest
func (m *BacktestManager) CancelBacktest(backtestID string) error {
	// Cancel in engine
	if err := m.engine.Cancel(backtestID); err != nil {
		// Try to remove from queue
		if removed := m.queue.Remove(backtestID); !removed {
			return err
		}
	}

	// Untrack
	m.untrackBacktest(backtestID)

	m.logger.Info("Backtest cancelled", map[string]interface{}{
		"backtest_id": backtestID,
	})

	return nil
}

// Start starts the manager (processes queue)
func (m *BacktestManager) Start(ctx context.Context) error {
	m.logger.Info("Starting backtest manager", map[string]interface{}{
		"max_concurrent": m.maxConcurrent,
	})

	// Start queue processor
	go m.processQueue()

	return nil
}

// Stop stops the manager
func (m *BacktestManager) Stop() error {
	m.logger.Info("Stopping backtest manager", nil)

	m.cancel()

	// Wait for running backtests to complete or timeout
	// (In a production system, we'd want graceful shutdown)

	return nil
}

// Private methods

func (m *BacktestManager) startBacktestNow(backtestID string) error {
	m.mu.Lock()
	backtest, exists := m.createdBacktests[backtestID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("backtest not found: %s", backtestID)
	}
	delete(m.createdBacktests, backtestID)
	m.mu.Unlock()

	config := backtest.Config
	if config == nil {
		return fmt.Errorf("backtest has no config: %s", backtestID)
	}
	config.ID = backtestID // engine tracks by config.ID

	m.logger.Info("Starting backtest", map[string]interface{}{
		"backtest_id": backtestID,
	})

	backtest.Start()
	m.trackBacktest(backtest)

	go m.runBacktest(backtestID, config)
	return nil
}

// runBacktest executes the backtest in the engine and updates status on completion
func (m *BacktestManager) runBacktest(backtestID string, config *models.BacktestConfig) {
	result, err := m.engine.Run(m.ctx, config)
	m.mu.Lock()
	backtest := m.runningBacktests[backtestID]
	m.mu.Unlock()
	if backtest == nil {
		return
	}
	if err != nil {
		backtest.Fail(err)
		// Persist failed result so GetBacktest can find it (UpdateStatus requires existing record)
		failedResult := models.NewBacktestResult(backtestID, config.ID)
		failedResult.Status = "failed"
		failedResult.Error = err.Error()
		failedResult.MarkFailed(err)
		_ = m.storage.Save(m.ctx, failedResult)
		m.untrackBacktest(backtestID)
		m.logger.Error("Backtest failed", map[string]interface{}{
			"backtest_id": backtestID,
			"error":       err.Error(),
		})
		return
	}
	backtest.Complete(result)
	// Engine already saved result; ensure status is updated if storage supports it
	_ = m.storage.UpdateStatus(m.ctx, backtestID, result.Status, result.Progress)
	m.untrackBacktest(backtestID)
	m.logger.Info("Backtest completed", map[string]interface{}{
		"backtest_id": backtestID,
		"status":      result.Status,
	})
}

func (m *BacktestManager) processQueue() {
	// Process queued backtests
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
			if m.canStartNewBacktest() && !m.queue.IsEmpty() {
				backtestID, err := m.queue.Dequeue()
				if err == nil {
					m.startBacktestNow(backtestID)
				}
			}
		}
	}
}
