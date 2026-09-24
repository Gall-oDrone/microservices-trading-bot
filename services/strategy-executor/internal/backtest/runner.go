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
	Timestamp time.Time `json:"timestamp"`
	// TickTime is the SIMULATED time of the replayed trade that produced this
	// signal. It is distinct from Timestamp because strategies stamp
	// Signal.Timestamp with time.Now() — during a historical replay that is the
	// wall-clock moment of the backtest run, not the market moment. Any
	// time-based analysis (per-regime attribution, hold times, session
	// bucketing) must use TickTime or it will silently attribute every trade to
	// the instant the backtest happened to execute.
	TickTime time.Time              `json:"tick_time"`
	Side     string                 `json:"side"`
	Price    float64                `json:"price"`
	Amount   float64                `json:"amount"`
	Reason   string                 `json:"reason"`
	PnL      float64                `json:"pnl,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
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
	CommissionBPS  float64 // Commission in basis points, both legs unless overridden below
	// BuyCommissionBPS / SellCommissionBPS override CommissionBPS per leg when
	// either is non-zero. This models mixed liquidity, e.g. a resting maker buy
	// followed by a taker sell. Leaving both at zero keeps the single-rate
	// behaviour, so existing callers are unaffected.
	BuyCommissionBPS  float64
	SellCommissionBPS float64
	FillProbability float64 // Probability of limit orders filling (0-1)
	// BeforeTick, when set, runs immediately before each strategy.OnTick call.
	// Used by the historical engine to advance the indicator window so indicators
	// reflect only past data at each replayed tick (no look-ahead).
	BeforeTick func(ctx context.Context, trade *indicators.Trade)
	// DisableEndOfRunClose, when true, leaves a position that is still open when
	// the data runs out unaccounted for (the pre-2026-09-24 behaviour). By
	// default the runner closes it at the last trade price so that its P&L is
	// counted rather than silently dropped.
	DisableEndOfRunClose bool
}

// legCommissionBPS returns the commission rate for one leg.
func (c RunnerConfig) legCommissionBPS(side string) float64 {
	if c.BuyCommissionBPS == 0 && c.SellCommissionBPS == 0 {
		return c.CommissionBPS
	}
	if side == "BUY" {
		return c.BuyCommissionBPS
	}
	return c.SellCommissionBPS
}

// ExitReasonEndOfBacktest tags the synthetic close the runner performs when a
// position is still open at the end of the replay.
const ExitReasonEndOfBacktest = "end_of_backtest"

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

	// Drive the strategy with SIMULATED market time.
	//
	// Strategies throttle signals (MinSignalInterval), time out pending orders
	// and time-box positions by comparing against "now". With the wall clock, a
	// multi-week replay that completes in minutes lets through only a handful
	// of signals, and the result measures CPU speed rather than strategy
	// behaviour. The clock is seeded from the first trade BEFORE Start() so
	// that start-up computations are anchored to market time too.
	var simNow time.Time
	if first, ok := r.provider.FirstTimestamp(); ok {
		simNow = first
	}
	if ca, ok := r.strategy.(strategies.ClockAware); ok {
		ca.SetClock(func() time.Time { return simNow })
		defer ca.SetClock(nil)
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
		entryFee      float64
		positionSize  float64
		inPosition    bool
		lastTrade     *indicators.Trade
	)

	// closePosition books a completed round trip and returns its P&L.
	//
	// Per-trade P&L is net of BOTH commission legs. The entry commission was
	// already debited from balance when the position opened, so only the exit
	// side (gross move less exit commission) is credited here; charging the
	// entry fee again would double-count it in the balance.
	closePosition := func(exitPrice, exitFee float64, at time.Time) float64 {
		exitNet := (exitPrice-entryPrice)*positionSize - exitFee
		pnl := exitNet - entryFee

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

		holdTime := at.Sub(entryTime)
		holdTimes = append(holdTimes, holdTime)
		if holdTime > result.MaxHoldTime {
			result.MaxHoldTime = holdTime
		}

		balance += exitNet
		if balance > peakBalance {
			peakBalance = balance
		}
		dd := (peakBalance - balance) / peakBalance * 100
		if dd > maxDrawdown {
			maxDrawdown = dd
		}

		inPosition = false
		positionSize = 0
		entryFee = 0
		return pnl
	}

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
		simNow = trade.Timestamp
		lastTrade = trade

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

		// Apply commission at this leg's rate.
		commission := price * signal.Amount * r.config.legCommissionBPS(signal.Side) / 10000

		// Record signal
		record := SignalRecord{
			Timestamp: signal.Timestamp,
			TickTime:  trade.Timestamp,
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
			entryFee = commission
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
			record.PnL = closePosition(price, commission, trade.Timestamp)

			// Notify the strategy that the exit filled.
			//
			// This mirrors the BUY notification above and is required for
			// correctness, not just completeness: OrderFillAware strategies set
			// PendingSell when they emit an exit and clear it only when a sell
			// fill is reported. Without this callback PendingSell stays set
			// forever, OnTick short-circuits, and the strategy executes exactly
			// one round trip for the entire replay regardless of its length.
			if fillAware, ok := r.strategy.(strategies.OrderFillAware); ok {
				eventID := ""
				if eid, ok := signal.Metadata["event_id"].(string); ok {
					eventID = eid
				}
				fillAware.OnOrderFilled(strategies.OrderFill{
					EventID:      eventID,
					Book:         r.strategy.GetConfig().Book,
					Side:         "sell",
					AveragePrice: price,
					FilledAmount: signal.Amount,
				})
			}
		}

		result.Signals = append(result.Signals, record)
	}

	// Close out a position that is still open when the data runs out.
	//
	// Previously such a position was silently dropped: its entry commission hit
	// the balance but its mark-to-market gain or loss never reached the
	// results. That biases every strategy that happens to be holding at the
	// end, and makes a buy-and-hold baseline report zero trades. The close is
	// charged slippage and commission like any other exit.
	if inPosition && lastTrade != nil && !r.config.DisableEndOfRunClose {
		price := lastTrade.Price
		if r.config.SlippageBPS > 0 {
			price -= price * r.config.SlippageBPS / 10000
		}
		amount := positionSize
		fee := price * amount * r.config.legCommissionBPS("SELL") / 10000
		pnl := closePosition(price, fee, lastTrade.Timestamp)
		result.Signals = append(result.Signals, SignalRecord{
			Timestamp: lastTrade.Timestamp,
			TickTime:  lastTrade.Timestamp,
			Side:      "SELL",
			Price:     price,
			Amount:    amount,
			Reason:    ExitReasonEndOfBacktest,
			PnL:       pnl,
			Metadata:  map[string]interface{}{"exit_reason": ExitReasonEndOfBacktest},
		})
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
