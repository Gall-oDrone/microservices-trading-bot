package backtest

import (
	"context"
	"math"
	"testing"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

// feeProbe records the fee provider the engine injects.
type feeProbe struct {
	*strategies.BaseEnhancedStrategy
	provider strategies.MakerTakerFeeProvider
}

func (p *feeProbe) SetFeeRatesProvider(fp strategies.MakerTakerFeeProvider) { p.provider = fp }
func (p *feeProbe) OnTick(*indicators.Trade) (*strategies.Signal, error)    { return nil, nil }
func (p *feeProbe) OnBar(*indicators.OHLCV) (*strategies.Signal, error)     { return nil, nil }

func withProbe(t *testing.T) *feeProbe {
	t.Helper()
	probe := &feeProbe{BaseEnhancedStrategy: strategies.NewBaseEnhancedStrategy("fee_probe", "test")}
	strategyFactories["fee_probe"] = func() strategies.EnhancedStrategy { return probe }
	t.Cleanup(func() { delete(strategyFactories, "fee_probe") })
	return probe
}

func probeTrades() []indicators.Trade {
	return ticks(time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC), time.Minute, 100, 101)
}

// TestRunHistorical_InjectsRunnerCostIntoStrategy is the guard for the defect
// that let the fee gate no-op in every backtest: the engine must hand the
// strategy the SAME per-leg cost the runner charges (commission + slippage).
func TestRunHistorical_InjectsRunnerCostIntoStrategy(t *testing.T) {
	probe := withProbe(t)
	_, err := RunHistorical(context.Background(), probeTrades(), EngineConfig{
		Book: "btc_mxn", StrategyType: "fee_probe",
		BuyCommissionBPS: 50, SellCommissionBPS: 65, SlippageBPS: 10,
	})
	if err != nil {
		t.Fatalf("RunHistorical: %v", err)
	}
	if probe.provider == nil {
		t.Fatal("engine did not inject a fee provider; fee gates would silently no-op")
	}
	buy, sell, ok := probe.provider.MakerTakerRatesForBook(context.Background(), "btc_mxn")
	if !ok {
		t.Fatal("injected provider reports no rates")
	}
	if math.Abs(buy-0.0060) > 1e-12 || math.Abs(sell-0.0075) > 1e-12 {
		t.Errorf("rates = buy %.6f sell %.6f, want 0.006000 / 0.007500 (commission + slippage per leg)", buy, sell)
	}
}

func TestRunHistorical_DisableFeeRatesSkipsInjection(t *testing.T) {
	probe := withProbe(t)
	if _, err := RunHistorical(context.Background(), probeTrades(), EngineConfig{
		Book: "btc_mxn", StrategyType: "fee_probe", CommissionBPS: 65, DisableFeeRates: true,
	}); err != nil {
		t.Fatalf("RunHistorical: %v", err)
	}
	if probe.provider != nil {
		t.Fatal("DisableFeeRates must leave the strategy without a fee provider")
	}
}
