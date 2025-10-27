package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// BacktestResult represents the complete results of a backtest
type BacktestResult struct {
	ID         string `json:"id"`
	BacktestID string `json:"backtest_id"`
	ConfigID   string `json:"config_id"`

	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	Error    string  `json:"error,omitempty"`

	Summary     *PerformanceSummary `json:"summary"`
	Trades      []Trade             `json:"trades"`
	EquityCurve []EquityPoint       `json:"equity_curve"`
	Positions   []Position          `json:"positions"`

	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Duration    int64      `json:"duration"` // Duration in seconds

	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// PerformanceSummary contains aggregated performance metrics
type PerformanceSummary struct {
	// Returns
	TotalReturn        float64 `json:"total_return"`
	TotalReturnPercent float64 `json:"total_return_percent"`
	AnnualizedReturn   float64 `json:"annualized_return"`

	// Risk metrics
	Volatility         float64 `json:"volatility"`
	SharpeRatio        float64 `json:"sharpe_ratio"`
	SortinoRatio       float64 `json:"sortino_ratio"`
	MaxDrawdown        float64 `json:"max_drawdown"`
	MaxDrawdownPercent float64 `json:"max_drawdown_percent"`

	// Trade statistics
	TotalTrades   int     `json:"total_trades"`
	WinningTrades int     `json:"winning_trades"`
	LosingTrades  int     `json:"losing_trades"`
	WinRate       float64 `json:"win_rate"`
	AverageWin    float64 `json:"average_win"`
	AverageLoss   float64 `json:"average_loss"`
	ProfitFactor  float64 `json:"profit_factor"`

	// Position metrics
	AverageHoldingTime int64   `json:"average_holding_time"` // Seconds
	MaxPosition        float64 `json:"max_position"`

	// P&L
	GrossProfitLoss  float64 `json:"gross_profit_loss"`
	NetProfitLoss    float64 `json:"net_profit_loss"`
	TotalCommissions float64 `json:"total_commissions"`

	// Portfolio
	FinalBalance   float64 `json:"final_balance"`
	PeakBalance    float64 `json:"peak_balance"`
	InitialBalance float64 `json:"initial_balance"`
}

// EquityPoint represents a point in the equity curve
type EquityPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Balance   float64   `json:"balance"`
	Equity    float64   `json:"equity"`    // Balance + unrealized P&L
	Return    float64   `json:"return"`    // Cumulative return
	Drawdown  float64   `json:"drawdown"`  // Current drawdown from peak
}

// NewBacktestResult creates a new backtest result
func NewBacktestResult(backtestID, configID string) *BacktestResult {
	return &BacktestResult{
		ID:          generateResultID(),
		BacktestID:  backtestID,
		ConfigID:    configID,
		Status:      "running",
		Progress:    0.0,
		Trades:      make([]Trade, 0),
		EquityCurve: make([]EquityPoint, 0),
		Positions:   make([]Position, 0),
		StartedAt:   time.Now(),
		Metadata:    make(map[string]interface{}),
	}
}

// AddTrade adds a trade to the results
func (r *BacktestResult) AddTrade(trade Trade) {
	r.Trades = append(r.Trades, trade)
}

// AddEquityPoint adds an equity point to the curve
func (r *BacktestResult) AddEquityPoint(point EquityPoint) {
	r.EquityCurve = append(r.EquityCurve, point)
}

// SetSummary sets the performance summary
func (r *BacktestResult) SetSummary(summary *PerformanceSummary) {
	r.Summary = summary
}

// MarkCompleted marks the result as completed
func (r *BacktestResult) MarkCompleted() {
	now := time.Now()
	r.Status = "completed"
	r.Progress = 1.0
	r.CompletedAt = &now
	r.Duration = int64(now.Sub(r.StartedAt).Seconds())
}

// MarkFailed marks the result as failed
func (r *BacktestResult) MarkFailed(err error) {
	now := time.Now()
	r.Status = "failed"
	r.Error = err.Error()
	r.CompletedAt = &now
	r.Duration = int64(now.Sub(r.StartedAt).Seconds())
}

// ToJSON converts the result to JSON
func (r *BacktestResult) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// BacktestResultFromJSON creates a result from JSON
func BacktestResultFromJSON(data []byte) (*BacktestResult, error) {
	var result BacktestResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal backtest result: %w", err)
	}
	return &result, nil
}

// GetTradeCount returns the total number of trades
func (r *BacktestResult) GetTradeCount() int {
	return len(r.Trades)
}

// GetWinningTrades returns only winning trades
func (r *BacktestResult) GetWinningTrades() []Trade {
	winning := make([]Trade, 0)
	for _, trade := range r.Trades {
		if trade.IsWinning() {
			winning = append(winning, trade)
		}
	}
	return winning
}

// GetLosingTrades returns only losing trades
func (r *BacktestResult) GetLosingTrades() []Trade {
	losing := make([]Trade, 0)
	for _, trade := range r.Trades {
		if trade.IsLosing() {
			losing = append(losing, trade)
		}
	}
	return losing
}

// generateResultID generates a unique ID for a result
func generateResultID() string {
	return fmt.Sprintf("res-%d", time.Now().UnixNano())
}

