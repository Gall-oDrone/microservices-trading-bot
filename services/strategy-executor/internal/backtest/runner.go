// Package backtest provides infrastructure for running strategies on historical data.
package backtest

import (
	"context"
	"fmt"
	"math"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

// BacktestResult holds the results of a backtest run.
type BacktestResult struct {
	StrategyName string    `json:"strategy_name"`
	Book         string    `json:"book"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`

	// Trade statistics
	TotalTrades    int     `json:"total_trades"`
	WinningTrades  int     `json:"winning_trades"`
	LosingTrades   int     `json:"losing_trades"`
	WinRate        float64 `json:"win_rate"`
	AverageTrade   float64 `json:"average_trade"`
	AverageWin     float64 `json:"average_win"`
	AverageLoss    float64 `json:"average_loss"`
	ProfitFactor   float64 `json:"profit_factor"`

	// P&L statistics
	TotalPnL       float64 `json:"total_pnl"`
	GrossProfits   float64 `json:"gross_profits"`
	GrossLosses    float64 `json:"gross_losses"`
	MaxDrawdown    float64 `json:"max_drawdown"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`

	// Risk-adjusted returns
	SharpeRatio float64 `json:"sharpe_ratio"`
	SortinoRatio float64 `json:"sortino_ratio"`

	// Time statistics
	AverageHoldTime  time.Duration `json:"average_hold_time"`
	MaxHoldTime      time.Duration `json:"max_hold_time"`
	TicksProcessed   int           `json:"ticks_processed"`

	// Signal list for analysis
	Signals []SignalRecord `json:"signals,omitempty"`
}

// SignalRecord captures a signal emitted during backtest.
type SignalRecord struct {
	Timestamp  time.Time              `json:"timestamp"`
	Side       string                 `json:"side"`
	Price      float64                `json:"price"`
	Amount     float64                `json:"amount"`
	Reason     string                 `json:"reason"`
	PnL        float64                `json:"pnl,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// Runner executes a strategy against historical data.
type Runner struct {
	strategy strategies.EnhancedStrategy
	provider *BacktestDataProvider
	config   RunnerConfig
}

// RunnerConfig holds configuration for the backtest runner.
type RunnerConfig struct {
	InitialBalance float64 // Starting quote balance
	SlippageBPS    float64 // Slippage in basis points
	CommissionBPS  float64 // Commission in basis points
	FillProbability float64 // Probability of limit orders filling (0-1)
	// BeforeTick, when set, runs immediately before each strategy.OnTick call.
	// Used by the historical engine to advance the indicator window so indicators
	// reflect only past data at each replayed tick (no look-ahead).
	BeforeTick func(ctx context.Context, trade *indicators.Trade)
}

// DefaultRunnerConfig returns default configuration.
func DefaultRunnerConfig() RunnerConfig {
	return RunnerConfig{
		InitialBalance:  100000,
		SlippageBPS:     10,
		CommissionBPS:   25,
		FillProbability: 0.95,
	}
}

// NewRunner creates a new backtest runner.
func NewRunner(strategy strategies.EnhancedStrategy, provider *BacktestDataProvider, config RunnerConfig) *Runner {
	return &Runner{
		strategy: strategy,
		provider: provider,
		config:   config,
	}
}

// Run executes the backtest and returns results.
func (r *Runner) Run(ctx context.Context) (*BacktestResult, error) {
	result := &BacktestResult{
		StrategyName: r.strategy.Name(),
		Signals:      []SignalRecord{},
	}

	// Start the strategy
	if err := r.strategy.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start strategy: %w", err)
	}
	defer r.strategy.Stop()

	var (
		balance       = r.config.InitialBalance
		peakBalance   = balance
		maxDrawdown   float64
		totalPnL      float64
		grossProfits  float64
		grossLosses   float64
		returns       []float64
		negReturns    []float64
		holdTimes     []time.Duration
		entryTime     time.Time
		entryPrice    float64
		positionSize  float64
		inPosition    bool
	)

	// Process each trade
	for !r.provider.IsExhausted() {
		trade := r.provider.NextTrade()
		if trade == nil {
			break
		}

		if result.StartTime.IsZero() {
			result.StartTime = trade.Timestamp
		}
		result.EndTime = trade.Timestamp
		result.TicksProcessed++

		// Advance indicator window before the strategy reads indicators.
		if r.config.BeforeTick != nil {
			r.config.BeforeTick(ctx, trade)
		}

		// Run strategy tick
		signal, err := r.strategy.OnTick(trade)
		if err != nil {
			continue
		}

		if signal == nil {
			continue
		}

		// Apply slippage
		price := signal.Price
		if r.config.SlippageBPS > 0 {
			slippage := price * r.config.SlippageBPS / 10000
			if signal.Side == "BUY" {
				price += slippage
			} else {
				price -= slippage
			}
		}

		// Apply commission
		commission := price * signal.Amount * r.config.CommissionBPS / 10000

		// Record signal
		record := SignalRecord{
			Timestamp: signal.Timestamp,
			Side:      signal.Side,
			Price:     price,
			Amount:    signal.Amount,
			Reason:    signal.Reason,
			Metadata:  signal.Metadata,
		}

		if signal.Side == "BUY" && !inPosition {
			// Entry
			entryTime = trade.Timestamp
			entryPrice = price
			positionSize = signal.Amount
			inPosition = true
			balance -= commission

			// Simulate fill notification if strategy supports it
			if fillAware, ok := r.strategy.(strategies.OrderFillAware); ok {
				eventID := ""
				if eid, ok := signal.Metadata["event_id"].(string); ok {
					eventID = eid
				}
				fillAware.OnOrderFilled(strategies.OrderFill{
					EventID:      eventID,
					Book:         r.strategy.GetConfig().Book,
					Side:         "buy",
					AveragePrice: price,
					FilledAmount: positionSize,
				})
			}

		} else if signal.Side == "SELL" && inPosition {
			// Exit
			pnl := (price - entryPrice) * positionSize - commission
			record.PnL = pnl

			totalPnL += pnl
			if pnl > 0 {
				result.WinningTrades++
				grossProfits += pnl
			} else {
				result.LosingTrades++
				grossLosses += math.Abs(pnl)
			}
			result.TotalTrades++
			returns = append(returns, pnl/balance*100)
			if pnl < 0 {
				negReturns = append(negReturns, pnl/balance*100)
			}

			holdTime := trade.Timestamp.Sub(entryTime)
			holdTimes = append(holdTimes, holdTime)
			if holdTime > result.MaxHoldTime {
				result.MaxHoldTime = holdTime
			}

			balance += pnl
			if balance > peakBalance {
				peakBalance = balance
			}
			dd := (peakBalance - balance) / peakBalance * 100
			if dd > maxDrawdown {
				maxDrawdown = dd
			}

			inPosition = false
			positionSize = 0
		}

		result.Signals = append(result.Signals, record)
	}

	// Calculate final statistics
	result.TotalPnL = totalPnL
	result.GrossProfits = grossProfits
	result.GrossLosses = grossLosses
	result.MaxDrawdownPct = maxDrawdown
	result.MaxDrawdown = peakBalance * maxDrawdown / 100

	if result.TotalTrades > 0 {
		result.WinRate = float64(result.WinningTrades) / float64(result.TotalTrades) * 100
		result.AverageTrade = totalPnL / float64(result.TotalTrades)
	}
	if result.WinningTrades > 0 {
		result.AverageWin = grossProfits / float64(result.WinningTrades)
	}
	if result.LosingTrades > 0 {
		result.AverageLoss = grossLosses / float64(result.LosingTrades)
	}
	if grossLosses > 0 {
		result.ProfitFactor = grossProfits / grossLosses
	}

	// Calculate hold time average
	if len(holdTimes) > 0 {
		var totalHold time.Duration
		for _, h := range holdTimes {
			totalHold += h
		}
		result.AverageHoldTime = totalHold / time.Duration(len(holdTimes))
	}

	// Calculate Sharpe ratio (simplified, assuming risk-free rate = 0)
	if len(returns) > 1 {
		meanReturn := mean(returns)
		stdReturn := stddev(returns)
		if stdReturn > 0 {
			result.SharpeRatio = meanReturn / stdReturn * math.Sqrt(252) // Annualized
		}
	}

	// Calculate Sortino ratio
	if len(negReturns) > 1 {
		meanReturn := mean(returns)
		downDev := stddev(negReturns)
		if downDev > 0 {
			result.SortinoRatio = meanReturn / downDev * math.Sqrt(252)
		}
	}

	return result, nil
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func stddev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	m := mean(values)
	sum := 0.0
	for _, v := range values {
		sum += (v - m) * (v - m)
	}
	return math.Sqrt(sum / float64(len(values)-1))
}

// PassesGates checks if the backtest result passes the validation gates.
func (r *BacktestResult) PassesGates(minSharpe, maxDrawdownPct, minWinRate, minProfitFactor float64, minTrades int) (bool, []string) {
	var failures []string

	if r.SharpeRatio < minSharpe {
		failures = append(failures, fmt.Sprintf("Sharpe ratio %.2f < minimum %.2f", r.SharpeRatio, minSharpe))
	}
	if r.MaxDrawdownPct > maxDrawdownPct {
		failures = append(failures, fmt.Sprintf("Max drawdown %.2f%% > maximum %.2f%%", r.MaxDrawdownPct, maxDrawdownPct))
	}
	if r.WinRate < minWinRate {
		failures = append(failures, fmt.Sprintf("Win rate %.2f%% < minimum %.2f%%", r.WinRate, minWinRate))
	}
	if r.ProfitFactor < minProfitFactor {
		failures = append(failures, fmt.Sprintf("Profit factor %.2f < minimum %.2f", r.ProfitFactor, minProfitFactor))
	}
	if r.TotalTrades < minTrades {
		failures = append(failures, fmt.Sprintf("Total trades %d < minimum %d", r.TotalTrades, minTrades))
	}

	return len(failures) == 0, failures
}
