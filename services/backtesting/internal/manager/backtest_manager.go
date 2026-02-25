package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"bitso-trading-platform/backtesting/internal/engine"
	"bitso-trading-platform/backtesting/internal/export"
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
	notifiers        []export.Notifier

	maxConcurrent    int
	runningBacktests map[string]*models.Backtest
	createdBacktests map[string]*models.Backtest // pending backtests not yet started
	mu               sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

// SetNotifiers sets optional completion notifiers (webhook, Kafka, S3 export)
func (m *BacktestManager) SetNotifiers(notifiers []export.Notifier) {
	m.notifiers = notifiers
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
		// Persist failed result so GetBacktest can find it (include config for reports/export)
		failedResult := models.NewBacktestResult(backtestID, config.ID)
		failedResult.SetConfigSnapshot(config)
		failedResult.Status = "failed"
		failedResult.Error = err.Error()
		failedResult.MarkFailed(err)
		_ = m.storage.Save(m.ctx, failedResult)
		m.untrackBacktest(backtestID)
		m.logBacktestCompletion("failed", backtestID, config, nil, "", false, err.Error())
		return
	}
	backtest.Complete(result)
	// Engine already saved result; ensure status is updated if storage supports it
	_ = m.storage.UpdateStatus(m.ctx, backtestID, result.Status, result.Progress)
	m.untrackBacktest(backtestID)
	m.logBacktestCompletion("completed", backtestID, config, result, result.FailureReason, result.MetThresholds, "")
}

// logBacktestCompletion writes one structured log line per completion for export to CloudWatch/S3
// and triggers optional notifiers (webhook, Kafka, S3 export)
func (m *BacktestManager) logBacktestCompletion(
	event string,
	backtestID string,
	config *models.BacktestConfig,
	result *models.BacktestResult,
	failureReason string,
	metThresholds bool,
	errMsg string,
) {
	fields := map[string]interface{}{
		"event":       "backtest_completion",
		"outcome":     event,
		"backtest_id": backtestID,
		"strategy":    config.Strategy,
		"book":        config.Book,
		"start_date":  config.StartDate.Format(time.RFC3339),
		"end_date":    config.EndDate.Format(time.RFC3339),
		"name":        config.Name,
	}
	var strategyParamsJSON string
	if config.StrategyParams != nil && len(config.StrategyParams) > 0 {
		if b, e := json.Marshal(config.StrategyParams); e == nil {
			fields["strategy_params_json"] = string(b)
			strategyParamsJSON = string(b)
		}
	}
	if event == "failed" {
		fields["error"] = errMsg
		m.logger.Info("backtest_completion", fields)
	} else {
		if result != nil && result.Summary != nil {
			s := result.Summary
			fields["total_return_percent"] = s.TotalReturnPercent
			fields["sharpe_ratio"] = s.SharpeRatio
			fields["max_drawdown_percent"] = s.MaxDrawdownPercent
			fields["win_rate"] = s.WinRate
			fields["total_trades"] = s.TotalTrades
			fields["met_thresholds"] = metThresholds
			if failureReason != "" {
				fields["failure_reason"] = failureReason
			}
		}
		m.logger.Info("backtest_completion", fields)
	}

	// Build and send to notifiers (fire-and-forget with timeout)
	ev := &export.BacktestCompletionEvent{
		Event:              "backtest_completion",
		Outcome:            event,
		BacktestID:         backtestID,
		Strategy:           config.Strategy,
		Book:               config.Book,
		Name:               config.Name,
		StartDate:          config.StartDate.Format(time.RFC3339),
		EndDate:            config.EndDate.Format(time.RFC3339),
		StrategyParamsJSON: strategyParamsJSON,
		Error:              errMsg,
		FailureReason:      failureReason,
		MetThresholds:      metThresholds,
	}
	if event == "completed" && result != nil && result.Summary != nil {
		s := result.Summary
		ev.TotalReturnPercent = s.TotalReturnPercent
		ev.SharpeRatio = s.SharpeRatio
		ev.MaxDrawdownPercent = s.MaxDrawdownPercent
		ev.WinRate = s.WinRate
		ev.TotalTrades = s.TotalTrades
	}
	for _, n := range m.notifiers {
		go m.notifyWithTimeout(n, ev)
	}
}

func (m *BacktestManager) notifyWithTimeout(n export.Notifier, ev *export.BacktestCompletionEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := n.Notify(ctx, ev); err != nil {
		m.logger.Warn("Completion notifier failed", map[string]interface{}{
			"notifier": n.Name(),
			"error":    err.Error(),
		})
	}
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
