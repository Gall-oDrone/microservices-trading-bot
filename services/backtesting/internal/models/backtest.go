package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// BacktestStatus represents the status of a backtest
type BacktestStatus string

const (
	BacktestStatusPending   BacktestStatus = "pending"
	BacktestStatusQueued    BacktestStatus = "queued"
	BacktestStatusRunning   BacktestStatus = "running"
	BacktestStatusCompleted BacktestStatus = "completed"
	BacktestStatusFailed    BacktestStatus = "failed"
	BacktestStatusCancelled BacktestStatus = "cancelled"
)

// Backtest represents a backtest instance
type Backtest struct {
	ID          string          `json:"id"`
	Config      *BacktestConfig `json:"config"`
	Status      BacktestStatus  `json:"status"`
	Progress    float64         `json:"progress"` // 0.0 to 1.0
	Result      *BacktestResult `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	StartedAt   *time.Time      `json:"started_at,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// NewBacktest creates a new backtest instance
func NewBacktest(config *BacktestConfig) *Backtest {
	now := time.Now()
	return &Backtest{
		ID:        generateBacktestID(),
		Config:    config,
		Status:    BacktestStatusPending,
		Progress:  0.0,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Start marks the backtest as started
func (b *Backtest) Start() {
	now := time.Now()
	b.Status = BacktestStatusRunning
	b.StartedAt = &now
	b.UpdatedAt = now
}

// UpdateProgress updates the backtest progress
func (b *Backtest) UpdateProgress(progress float64) {
	if progress < 0 {
		progress = 0
	}
	if progress > 1.0 {
		progress = 1.0
	}
	b.Progress = progress
	b.UpdatedAt = time.Now()
}

// Complete marks the backtest as completed with results
func (b *Backtest) Complete(result *BacktestResult) {
	now := time.Now()
	b.Status = BacktestStatusCompleted
	b.Progress = 1.0
	b.Result = result
	b.CompletedAt = &now
	b.UpdatedAt = now
}

// Fail marks the backtest as failed with an error
func (b *Backtest) Fail(err error) {
	now := time.Now()
	b.Status = BacktestStatusFailed
	b.Error = err.Error()
	b.CompletedAt = &now
	b.UpdatedAt = now
}

// Cancel marks the backtest as cancelled
func (b *Backtest) Cancel() {
	now := time.Now()
	b.Status = BacktestStatusCancelled
	b.CompletedAt = &now
	b.UpdatedAt = now
}

// IsActive returns true if the backtest is currently running
func (b *Backtest) IsActive() bool {
	return b.Status == BacktestStatusRunning || b.Status == BacktestStatusQueued
}

// IsCompleted returns true if the backtest has finished (completed, failed, or cancelled)
func (b *Backtest) IsCompleted() bool {
	return b.Status == BacktestStatusCompleted ||
		b.Status == BacktestStatusFailed ||
		b.Status == BacktestStatusCancelled
}

// Duration returns the duration of the backtest execution
func (b *Backtest) Duration() time.Duration {
	if b.StartedAt == nil {
		return 0
	}

	endTime := time.Now()
	if b.CompletedAt != nil {
		endTime = *b.CompletedAt
	}

	return endTime.Sub(*b.StartedAt)
}

// ToJSON converts the backtest to JSON
func (b *Backtest) ToJSON() ([]byte, error) {
	return json.Marshal(b)
}

// BacktestFromJSON creates a backtest from JSON
func BacktestFromJSON(data []byte) (*Backtest, error) {
	var backtest Backtest
	if err := json.Unmarshal(data, &backtest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal backtest: %w", err)
	}
	return &backtest, nil
}

// generateBacktestID generates a unique ID for a backtest
// Format: bt-<timestamp>-<random>
func generateBacktestID() string {
	return fmt.Sprintf("bt-%d", time.Now().UnixNano())
}
