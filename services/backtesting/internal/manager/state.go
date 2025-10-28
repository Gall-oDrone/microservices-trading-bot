package manager

import (
	"fmt"

	"bitso-trading-platform/backtesting/internal/models"
)

// trackBacktest tracks a running backtest
func (m *BacktestManager) trackBacktest(backtest *models.Backtest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.runningBacktests[backtest.ID] = backtest
	
	if m.metricsCollector != nil {
		m.metricsCollector.RecordActiveBacktests(len(m.runningBacktests))
	}
}

// untrackBacktest stops tracking a backtest
func (m *BacktestManager) untrackBacktest(backtestID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	delete(m.runningBacktests, backtestID)
	
	if m.metricsCollector != nil {
		m.metricsCollector.RecordActiveBacktests(len(m.runningBacktests))
	}
}

// getTrackedBacktest retrieves a tracked backtest
func (m *BacktestManager) getTrackedBacktest(backtestID string) (*models.Backtest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	backtest, exists := m.runningBacktests[backtestID]
	if !exists {
		return nil, fmt.Errorf("backtest not found in running backtests: %s", backtestID)
	}
	
	return backtest, nil
}

// getRunningCount returns the number of currently running backtests
func (m *BacktestManager) getRunningCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.runningBacktests)
}

// canStartNewBacktest checks if a new backtest can be started
func (m *BacktestManager) canStartNewBacktest() bool {
	return m.getRunningCount() < m.maxConcurrent
}

// GetRunningBacktests returns all currently running backtests
func (m *BacktestManager) GetRunningBacktests() []*models.Backtest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	backtests := make([]*models.Backtest, 0, len(m.runningBacktests))
	for _, bt := range m.runningBacktests {
		backtests = append(backtests, bt)
	}
	
	return backtests
}

// GetQueuedBacktests returns all queued backtest IDs
func (m *BacktestManager) GetQueuedBacktests() []string {
	return m.queue.GetAll()
}

// GetStats returns manager statistics
func (m *BacktestManager) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"running_count":   m.getRunningCount(),
		"queued_count":    m.queue.Size(),
		"max_concurrent":  m.maxConcurrent,
		"queue_capacity":  m.queue.capacity,
	}
}

