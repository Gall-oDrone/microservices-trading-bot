package engine

import (
	"context"
	"fmt"
	"time"

	"bitso-trading-platform/backtesting/internal/analyzer"
	"bitso-trading-platform/backtesting/internal/data"
	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/metrics"
	"bitso-trading-platform/backtesting/internal/models"
	"bitso-trading-platform/backtesting/internal/portfolio"
	"bitso-trading-platform/backtesting/internal/simulator"
	"bitso-trading-platform/backtesting/internal/strategy"
)

// BacktestRunner orchestrates a single backtest execution
type BacktestRunner struct {
	config           *models.BacktestConfig
	dataProvider     data.DataProvider
	simulator        simulator.MarketSimulator
	strategyExecutor *strategy.StrategyExecutor
	portfolio        *portfolio.VirtualPortfolio
	analyzer         *analyzer.PerformanceAnalyzer
	logger           logger.Logger
	metricsCollector *metrics.MetricsCollector

	progressCallback func(float64)
	cancelChan       <-chan struct{}
}

// NewBacktestRunner creates a new backtest runner
func NewBacktestRunner(
	config *models.BacktestConfig,
	dataProvider data.DataProvider,
	log logger.Logger,
	metricsCollector *metrics.MetricsCollector,
) (*BacktestRunner, error) {
	return &BacktestRunner{
		config:           config,
		dataProvider:     dataProvider,
		logger:           log,
		metricsCollector: metricsCollector,
	}, nil
}

// Initialize initializes all components for the backtest
func (r *BacktestRunner) Initialize(ctx context.Context) error {
	r.logger.Info("Initializing backtest runner", map[string]interface{}{
		"backtest_id": r.config.ID,
	})

	// Initialize virtual portfolio
	r.portfolio = portfolio.NewVirtualPortfolio(r.config.ID, r.config.InitialBalance)

	// Initialize market simulator (Bitso-style maker/taker fees when set)
	simConfig := &simulator.SimulatorConfig{
		SlippageModel:   r.config.SlippageModel,
		SlippageValue:   r.config.SlippageValue,
		CommissionRate:  r.config.CommissionRate,
		MakerFee:        r.config.MakerFee,
		TakerFee:        r.config.TakerFee,
	}
	r.simulator = simulator.NewSimulator(simConfig, r.logger)
	if err := r.simulator.Initialize(ctx, simConfig); err != nil {
		return fmt.Errorf("failed to initialize simulator: %w", err)
	}

	// Initialize strategy
	strategyImpl, err := strategy.CreateStrategy(r.config.Strategy, r.config.StrategyParams)
	if err != nil {
		return fmt.Errorf("failed to create strategy: %w", err)
	}
	r.strategyExecutor = strategy.NewStrategyExecutor(strategyImpl, r.logger)

	// Initialize analyzer
	r.analyzer = analyzer.NewAnalyzer(r.logger)

	r.logger.Info("Backtest runner initialized", nil)

	return nil
}

// Execute executes the backtest
func (r *BacktestRunner) Execute(ctx context.Context) (*models.BacktestResult, error) {
	startTime := time.Now()

	r.logger.Info("Executing backtest", map[string]interface{}{
		"book":       r.config.Book,
		"start_date": r.config.StartDate.Format("2006-01-02"),
		"end_date":   r.config.EndDate.Format("2006-01-02"),
	})

	// Create result and attach config snapshot for reports and export
	result := models.NewBacktestResult(r.config.ID, r.config.ID)
	result.SetConfigSnapshot(r.config)

	// Load historical data
	request := data.NewDataRequest(r.config.Book, r.config.StartDate, r.config.EndDate).
		WithEventTypes(models.EventTypeTrade).
		WithGranularity(r.config.DataGranularity)

	dataLoadStart := time.Now()
	events, err := r.dataProvider.LoadHistoricalData(ctx, request)
	if r.metricsCollector != nil {
		r.metricsCollector.RecordDataLoadDuration(r.config.DataSource, time.Since(dataLoadStart))
	}
	if err != nil {
		result.MarkFailed(err)
		return result, fmt.Errorf("failed to load historical data: %w", err)
	}

	r.logger.Info("Historical data loaded", map[string]interface{}{
		"event_count": len(events),
	})

	// Run event loop
	if err := r.runEventLoop(ctx, events, result); err != nil {
		result.MarkFailed(err)
		return result, fmt.Errorf("event loop failed: %w", err)
	}

	// Record events processed
	if r.metricsCollector != nil {
		r.metricsCollector.RecordEventsProcessed("trade", len(events))
	}

	// Analyze performance
	summary, err := r.analyzer.Analyze(r.portfolio, result.Trades, result.EquityCurve)
	if err != nil {
		r.logger.Error("Failed to analyze performance", map[string]interface{}{"error": err})
	} else {
		result.SetSummary(summary)
		result.EvaluateSuccessCriteria(r.config.SuccessCriteria)
	}

	// Mark completed
	result.MarkCompleted()
	result.Duration = int64(time.Since(startTime).Seconds())

	r.logger.Info("Backtest execution completed", map[string]interface{}{
		"duration":    time.Since(startTime),
		"trade_count": len(result.Trades),
	})

	return result, nil
}

// SetProgressCallback sets the progress callback function
func (r *BacktestRunner) SetProgressCallback(callback func(float64)) {
	r.progressCallback = callback
}

// cleanup performs cleanup after backtest
func (r *BacktestRunner) cleanup() {
	// Reset components
	if r.simulator != nil {
		r.simulator.Reset()
	}
	if r.strategyExecutor != nil {
		r.strategyExecutor.Reset()
	}
}
