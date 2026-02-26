package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ConfigSnapshot is an immutable snapshot of backtest config stored with the result
// so that strategy name, parameters, and run settings are preserved for reports and export
type ConfigSnapshot struct {
	Name        string                 `json:"name"`
	Book        string                 `json:"book"`
	Strategy    string                 `json:"strategy"`
	StrategyParams map[string]interface{} `json:"strategy_params"`
	StartDate   time.Time              `json:"start_date"`
	EndDate     time.Time              `json:"end_date"`
	InitialBalance float64             `json:"initial_balance"`
	SlippageModel   string  `json:"slippage_model"`
	SlippageValue   float64 `json:"slippage_value"`
	CommissionRate  float64 `json:"commission_rate"`
	MakerFee        float64 `json:"maker_fee,omitempty"`
	TakerFee        float64 `json:"taker_fee,omitempty"`
	DataSource      string  `json:"data_source"`
	DataGranularity string             `json:"data_granularity"`
}

// BacktestResult represents the complete results of a backtest
type BacktestResult struct {
	ID         string `json:"id"`
	BacktestID string `json:"backtest_id"`
	ConfigID   string `json:"config_id"`

	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	Error    string  `json:"error,omitempty"`

	// ConfigSnapshot preserves strategy and parameters for reports and export
	Config *ConfigSnapshot `json:"config,omitempty"`

	Summary     *PerformanceSummary `json:"summary"`
	Trades      []Trade             `json:"trades"`
	EquityCurve []EquityPoint       `json:"equity_curve"`
	Positions   []Position          `json:"positions"`

	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Duration    int64      `json:"duration"` // Duration in seconds

	// MetThresholds is set when SuccessCriteria is evaluated (completed runs only)
	MetThresholds bool   `json:"met_thresholds,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"` // Why thresholds were not met, if any

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
	Equity    float64   `json:"equity"`   // Balance + unrealized P&L
	Return    float64   `json:"return"`   // Cumulative return
	Drawdown  float64   `json:"drawdown"` // Current drawdown from peak
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

// SetConfigSnapshot stores a snapshot of the backtest config for reports and export
func (r *BacktestResult) SetConfigSnapshot(cfg *BacktestConfig) {
	if cfg == nil {
		return
	}
	params := make(map[string]interface{})
	if cfg.StrategyParams != nil {
		for k, v := range cfg.StrategyParams {
			params[k] = v
		}
	}
	r.Config = &ConfigSnapshot{
		Name:             cfg.Name,
		Book:             cfg.Book,
		Strategy:         cfg.Strategy,
		StrategyParams:   params,
		StartDate:        cfg.StartDate,
		EndDate:          cfg.EndDate,
		InitialBalance:   cfg.InitialBalance,
		SlippageModel:    cfg.SlippageModel,
		SlippageValue:    cfg.SlippageValue,
		CommissionRate:   cfg.CommissionRate,
		MakerFee:         cfg.MakerFee,
		TakerFee:         cfg.TakerFee,
		DataSource:       cfg.DataSource,
		DataGranularity:  cfg.DataGranularity,
	}
}

// EvaluateSuccessCriteria evaluates optional success criteria against the summary and sets
// MetThresholds and FailureReason. No-op if criteria is nil or result has no summary.
func (r *BacktestResult) EvaluateSuccessCriteria(criteria *SuccessCriteria) {
	if criteria == nil || r.Summary == nil || r.Status != "completed" {
		return
	}
	var reasons []string
	s := r.Summary

	if criteria.MinSharpeRatio > 0 && s.SharpeRatio < criteria.MinSharpeRatio {
		reasons = append(reasons, fmt.Sprintf("sharpe_ratio %.2f < min %.2f", s.SharpeRatio, criteria.MinSharpeRatio))
	}
	if criteria.MaxDrawdownPercent > 0 && (-s.MaxDrawdownPercent) > criteria.MaxDrawdownPercent {
		reasons = append(reasons, fmt.Sprintf("max_drawdown %.2f%% exceeds -%.2f%%", s.MaxDrawdownPercent, criteria.MaxDrawdownPercent))
	}
	if criteria.MinTotalTrades > 0 && s.TotalTrades < criteria.MinTotalTrades {
		reasons = append(reasons, fmt.Sprintf("total_trades %d < min %d", s.TotalTrades, criteria.MinTotalTrades))
	}
	if criteria.MinWinRate > 0 && s.WinRate < criteria.MinWinRate {
		reasons = append(reasons, fmt.Sprintf("win_rate %.2f < min %.2f", s.WinRate, criteria.MinWinRate))
	}
	if criteria.MinTotalReturnPct != 0 && s.TotalReturnPercent < criteria.MinTotalReturnPct {
		reasons = append(reasons, fmt.Sprintf("total_return_percent %.2f < min %.2f", s.TotalReturnPercent, criteria.MinTotalReturnPct))
	}

	if len(reasons) > 0 {
		r.MetThresholds = false
		r.FailureReason = strings.Join(reasons, "; ")
	} else {
		r.MetThresholds = true
		r.FailureReason = ""
	}
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
