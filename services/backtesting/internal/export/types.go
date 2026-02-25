package export

import "context"

// BacktestCompletionEvent is the payload sent to webhooks, Kafka, and S3 export
type BacktestCompletionEvent struct {
	Event       string  `json:"event"`        // "backtest_completion"
	Outcome     string  `json:"outcome"`      // "completed" or "failed"
	BacktestID  string  `json:"backtest_id"`
	Strategy    string  `json:"strategy"`
	Book        string  `json:"book"`
	Name        string  `json:"name"`
	StartDate   string  `json:"start_date"`
	EndDate     string  `json:"end_date"`
	StrategyParamsJSON string `json:"strategy_params_json,omitempty"`

	// Completed
	TotalReturnPercent  float64 `json:"total_return_percent,omitempty"`
	SharpeRatio         float64 `json:"sharpe_ratio,omitempty"`
	MaxDrawdownPercent  float64 `json:"max_drawdown_percent,omitempty"`
	WinRate             float64 `json:"win_rate,omitempty"`
	TotalTrades         int     `json:"total_trades,omitempty"`
	MetThresholds       bool    `json:"met_thresholds,omitempty"`
	FailureReason       string  `json:"failure_reason,omitempty"`

	// Failed
	Error string `json:"error,omitempty"`
}

// Notifier sends completion events to an external system
type Notifier interface {
	Notify(ctx context.Context, event *BacktestCompletionEvent) error
	Name() string
}
