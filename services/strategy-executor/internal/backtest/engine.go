package backtest

import (
	"context"
	"fmt"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

// EngineConfig configures a single historical backtest run.
type EngineConfig struct {
	Book           string
	StrategyType   string
	StrategyName   string
	Parameters     map[string]interface{}
	InitialBalance float64
	SlippageBPS    float64
	CommissionBPS  float64
}

// strategyFactories maps a strategy type to its constructor. These are the same
// built-in factories the live registry uses, but instantiated locally so a
// backtest never mutates live registry or global Prometheus state.
var strategyFactories = map[string]func() strategies.EnhancedStrategy{
	"mean_reversion": strategies.NewMeanReversionStrategyFactory(),
	"momentum":       strategies.NewMomentumStrategyFactory(),
	"limit_profit":   strategies.NewLimitProfitStrategyFactory(),
}

// SupportedStrategyTypes returns the strategy types the backtest engine can run.
func SupportedStrategyTypes() []string {
	out := make([]string, 0, len(strategyFactories))
	for t := range strategyFactories {
		out = append(out, t)
	}
	return out
}

// RunHistorical replays the given trades through the requested strategy and
// returns performance metrics. Indicators are computed from a growing window of
// past trades only (via ReplayProvider + BeforeTick), so the run is free of
// look-ahead bias and uses the same bar-first indicator math as production.
//
// Indicator values are written directly into a dedicated in-memory store so the
// backtest does not perturb the live strategy-executor Prometheus gauges.
func RunHistorical(ctx context.Context, trades []indicators.Trade, cfg EngineConfig) (*BacktestResult, error) {
	if cfg.Book == "" {
		return nil, fmt.Errorf("book is required")
	}
	factory, ok := strategyFactories[cfg.StrategyType]
	if !ok {
		return nil, fmt.Errorf("unsupported strategy type %q (supported: %v)", cfg.StrategyType, SupportedStrategyTypes())
	}
	if len(trades) == 0 {
		return nil, fmt.Errorf("no trades to backtest")
	}

	replay := NewReplayProvider()
	store := indicators.NewInMemoryIndicatorStore()

	indCfg := indicators.DefaultServiceConfig()
	// Historical bars carry past timestamps; disable the wall-clock staleness gate
	// so replayed indicators are usable.
	indCfg.MaxStaleness = 100 * 365 * 24 * time.Hour
	indSvc := indicators.NewService(indCfg, store, replay, nil)

	comp := newIndicatorComputer(indCfg)

	strat := factory()
	name := cfg.StrategyName
	if name == "" {
		name = fmt.Sprintf("%s_%s_backtest", cfg.StrategyType, cfg.Book)
	}
	scfg := strategies.StrategyConfig{
		Name:       name,
		Type:       cfg.StrategyType,
		Version:    "1.0.0",
		Enabled:    true,
		Book:       cfg.Book,
		Parameters: cfg.Parameters,
	}
	if err := strat.Initialize(scfg, indSvc); err != nil {
		return nil, fmt.Errorf("initialize strategy: %w", err)
	}

	provider := NewBacktestDataProvider(trades)
	runnerCfg := RunnerConfig{
		InitialBalance:  cfg.InitialBalance,
		SlippageBPS:     cfg.SlippageBPS,
		CommissionBPS:   cfg.CommissionBPS,
		FillProbability: 1.0,
		BeforeTick: func(ctx context.Context, t *indicators.Trade) {
			replay.Observe(*t)
			comp.computeInto(ctx, store, replay, cfg.Book)
		},
	}
	if runnerCfg.InitialBalance <= 0 {
		runnerCfg.InitialBalance = DefaultRunnerConfig().InitialBalance
	}

	runner := NewRunner(strat, provider, runnerCfg)
	result, err := runner.Run(ctx)
	if err != nil {
		return nil, err
	}
	result.Book = cfg.Book
	result.StrategyName = name
	return result, nil
}

// indicatorComputer recomputes indicators from the replay window and writes them
// to the in-memory store, mirroring indicators.Service.ComputeAndStore but
// without any global Prometheus side effects.
type indicatorComputer struct {
	cfg       *indicators.ServiceConfig
	sma       *indicators.SMA
	ema       *indicators.EMA
	rsi       *indicators.RSI
	bollinger *indicators.Bollinger
	atr       *indicators.ATR
	vwap      *indicators.VWAP
}

func newIndicatorComputer(cfg *indicators.ServiceConfig) *indicatorComputer {
	return &indicatorComputer{
		cfg:       cfg,
		sma:       indicators.NewSMA(cfg.SMAPeriod),
		ema:       indicators.NewEMA(cfg.EMAPeriod),
		rsi:       indicators.NewRSI(cfg.RSIPeriod),
		bollinger: indicators.NewBollinger(cfg.BollingerPeriod, cfg.BollingerStdDev),
		atr:       indicators.NewATR(cfg.ATRPeriod),
		vwap:      indicators.NewVWAP(cfg.VWAPPeriod),
	}
}

func (c *indicatorComputer) computeInto(ctx context.Context, store indicators.IndicatorStore, replay *ReplayProvider, book string) {
	bars, _ := replay.GetRecentBars(ctx, book, c.cfg.BarInterval, 0)
	if len(bars) == 0 {
		return
	}
	closes := make([]float64, len(bars))
	for i, b := range bars {
		closes[i] = b.Close
	}
	now := time.Now()

	if v, err := c.sma.Compute(closes); err == nil {
		_ = store.Set(ctx, book, "sma", c.cfg.SMAPeriod, &indicators.IndicatorValue{Name: "sma", Period: c.cfg.SMAPeriod, Value: v, Timestamp: now, Book: book})
	}
	if v, err := c.ema.Compute(closes); err == nil {
		_ = store.Set(ctx, book, "ema", c.cfg.EMAPeriod, &indicators.IndicatorValue{Name: "ema", Period: c.cfg.EMAPeriod, Value: v, Timestamp: now, Book: book})
	}
	if v, err := c.rsi.Compute(closes); err == nil {
		_ = store.Set(ctx, book, "rsi", c.cfg.RSIPeriod, &indicators.IndicatorValue{Name: "rsi", Period: c.cfg.RSIPeriod, Value: v, Timestamp: now, Book: book})
	}
	if bb, err := c.bollinger.ComputeBands(closes); err == nil {
		_ = store.SetBollinger(ctx, book, c.cfg.BollingerPeriod, bb)
		_ = store.Set(ctx, book, "bollinger_middle", c.cfg.BollingerPeriod, &indicators.IndicatorValue{
			Name: "bollinger_middle", Period: c.cfg.BollingerPeriod, Value: bb.Middle, Timestamp: now, Book: book,
			Extra: map[string]float64{"upper": bb.Upper, "lower": bb.Lower, "stddev": bb.StdDev},
		})
	}
	if v, err := c.atr.ComputeFromBars(bars); err == nil {
		_ = store.Set(ctx, book, "atr", c.cfg.ATRPeriod, &indicators.IndicatorValue{Name: "atr", Period: c.cfg.ATRPeriod, Value: v, Timestamp: now, Book: book})
	}
	if trades, err := replay.GetRecentTrades(ctx, book, 100); err == nil && len(trades) > 0 {
		if v, err := c.vwap.ComputeFromTrades(trades); err == nil {
			_ = store.Set(ctx, book, "vwap", c.cfg.VWAPPeriod, &indicators.IndicatorValue{Name: "vwap", Period: c.cfg.VWAPPeriod, Value: v, Timestamp: now, Book: book})
		}
	}
}
