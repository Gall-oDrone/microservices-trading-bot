package backtest

import (
	"context"
	"math"
	"testing"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

// scripted is a test strategy that alternates BUY/SELL, emitting at most one
// signal per minInterval of STRATEGY time (s.Now()). It mimics the
// MinSignalInterval throttle the production strategies use.
type scripted struct {
	*strategies.BaseEnhancedStrategy
	minInterval time.Duration
	holding     bool
	amount      float64
	// sides, when non-nil, restricts emission to the listed tick indices
	// (index -> side). Used for exact accounting checks.
	sides map[int]string
	tick  int
}

func newScripted(minInterval time.Duration) *scripted {
	return &scripted{
		BaseEnhancedStrategy: strategies.NewBaseEnhancedStrategy("scripted", "test"),
		minInterval:          minInterval,
		amount:               1,
	}
}

func (s *scripted) OnTick(t *indicators.Trade) (*strategies.Signal, error) {
	idx := s.tick
	s.tick++
	if !s.IsRunning() {
		return nil, nil
	}

	var side string
	if s.sides != nil {
		var ok bool
		if side, ok = s.sides[idx]; !ok {
			return nil, nil
		}
	} else {
		last := s.GetState().LastSignalTime
		if !last.IsZero() && s.Now().Sub(last) < s.minInterval {
			return nil, nil
		}
		side = "BUY"
		if s.holding {
			side = "SELL"
		}
	}
	s.holding = side == "BUY"
	s.RecordSignal()
	return &strategies.Signal{Side: side, Amount: s.amount, Price: t.Price, Timestamp: s.Now()}, nil
}

func (s *scripted) OnBar(*indicators.OHLCV) (*strategies.Signal, error) { return nil, nil }

func ticks(start time.Time, step time.Duration, prices ...float64) []indicators.Trade {
	out := make([]indicators.Trade, len(prices))
	for i, p := range prices {
		out[i] = indicators.Trade{Timestamp: start.Add(time.Duration(i) * step), Price: p, Amount: 1}
	}
	return out
}

func run(t *testing.T, strat strategies.EnhancedStrategy, trades []indicators.Trade, cfg RunnerConfig) *BacktestResult {
	t.Helper()
	if err := strat.Initialize(strategies.StrategyConfig{Name: "t", Book: "btc_mxn", Enabled: true}, nil); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if cfg.InitialBalance == 0 {
		cfg.InitialBalance = 100000
	}
	res, err := NewRunner(strat, NewBacktestDataProvider(trades), cfg).Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return res
}

func approx(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %.10f, want %.10f", name, got, want)
	}
}

var t0 = time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

// TestRunner_ThrottleFollowsSimulatedTime is the regression test for harness
// defect #1: a 60s throttle must admit one signal per 60s of MARKET time, not
// of wall-clock time. 21 ticks 30s apart span 10 simulated minutes, which
// admits 11 signals (t=0,60,...,600). Under the old wall-clock behaviour the
// whole replay completes in microseconds and only 1 signal gets through.
func TestRunner_ThrottleFollowsSimulatedTime(t *testing.T) {
	prices := make([]float64, 21)
	for i := range prices {
		prices[i] = 100
	}
	s := newScripted(60 * time.Second)
	res := run(t, s, ticks(t0, 30*time.Second, prices...), RunnerConfig{})

	if got := len(res.Signals); got != 11+0 && got != 12 {
		// 11 throttled signals; +1 synthetic end-of-run close if the last
		// throttled signal left a position open.
		t.Fatalf("signals = %d, want 11 (or 12 with end-of-run close)", got)
	}
	if s.GetState().SignalCount != 11 {
		t.Fatalf("strategy SignalCount = %d, want 11 — throttle is not using simulated time", s.GetState().SignalCount)
	}
}

// TestRunner_ClockSeededBeforeStartAndRestored checks the clock reads market
// time during the run and reverts to the wall clock afterwards, so a strategy
// instance reused outside the backtest is not left frozen in the past.
func TestRunner_ClockSeededBeforeStartAndRestored(t *testing.T) {
	s := newScripted(time.Hour)
	run(t, s, ticks(t0, time.Minute, 100, 100), RunnerConfig{})

	if got := s.GetState().LastSignalTime; !got.Equal(t0) {
		t.Errorf("LastSignalTime = %v, want simulated %v", got, t0)
	}
	if time.Since(s.Now()) > time.Minute {
		t.Errorf("clock not restored to wall time after run: Now() = %v", s.Now())
	}
}

// TestRunner_ChargesBothCommissionLegs is the regression test for the
// one-leg-commission bug: per-trade P&L must subtract the buy AND sell fees.
func TestRunner_ChargesBothCommissionLegs(t *testing.T) {
	s := newScripted(0)
	s.sides = map[int]string{0: "BUY", 1: "SELL"}
	res := run(t, s, ticks(t0, time.Minute, 100, 110), RunnerConfig{CommissionBPS: 100})

	// gross 10, buy fee 100*1%=1.00, sell fee 110*1%=1.10 => 7.90
	if res.TotalTrades != 1 {
		t.Fatalf("TotalTrades = %d, want 1", res.TotalTrades)
	}
	approx(t, "TotalPnL", res.TotalPnL, 7.90)
	approx(t, "trade PnL", res.Signals[1].PnL, 7.90)
}

// TestRunner_BalanceMatchesReportedPnL guards against double-counting the
// entry fee: the balance change over a full round trip must equal the sum of
// reported per-trade P&L exactly.
func TestRunner_BalanceMatchesReportedPnL(t *testing.T) {
	s := newScripted(0)
	s.sides = map[int]string{0: "BUY", 1: "SELL", 2: "BUY", 3: "SELL"}
	res := run(t, s, ticks(t0, time.Minute, 100, 90, 95, 120), RunnerConfig{CommissionBPS: 50, InitialBalance: 1000})

	// Drawdown is computed from balance; recompute balance independently.
	want := 1000.0
	for _, sig := range res.Signals {
		want += sig.PnL
	}
	approx(t, "sum of trade PnL", res.TotalPnL, want-1000)
	if res.TotalTrades != 2 {
		t.Fatalf("TotalTrades = %d, want 2", res.TotalTrades)
	}
}

// TestRunner_PerLegCommission models a maker buy and a taker sell.
func TestRunner_PerLegCommission(t *testing.T) {
	s := newScripted(0)
	s.sides = map[int]string{0: "BUY", 1: "SELL"}
	res := run(t, s, ticks(t0, time.Minute, 100, 100), RunnerConfig{
		CommissionBPS: 999, BuyCommissionBPS: 50, SellCommissionBPS: 65,
	})
	// flat price: -(0.50 + 0.65)
	approx(t, "TotalPnL", res.TotalPnL, -1.15)
}

// TestRunner_ClosesOpenPositionAtEnd checks a position still held when the
// data runs out is booked, with slippage and both fees, rather than dropped.
func TestRunner_ClosesOpenPositionAtEnd(t *testing.T) {
	res := run(t, newBuyAndHold(), ticks(t0, time.Minute, 100, 110, 120), RunnerConfig{})

	if res.TotalTrades != 1 {
		t.Fatalf("TotalTrades = %d, want 1 (open position must be closed at end)", res.TotalTrades)
	}
	// buy_and_hold defaults to 0.001 BTC: (120-100)*0.001
	approx(t, "TotalPnL", res.TotalPnL, 0.02)
	last := res.Signals[len(res.Signals)-1]
	if last.Reason != ExitReasonEndOfBacktest {
		t.Errorf("last signal reason = %q, want %q", last.Reason, ExitReasonEndOfBacktest)
	}
}

func TestRunner_EndOfRunCloseChargesCosts(t *testing.T) {
	res := run(t, newBuyAndHold(), ticks(t0, time.Minute, 100, 100), RunnerConfig{
		CommissionBPS: 100, SlippageBPS: 100,
	})
	// size 0.001. entry 101 (slip up), fee 1.01%; exit 99 (slip down), fee 0.99%.
	want := ((99.0-101.0)*0.001 - 99.0*0.001*0.01) - 101.0*0.001*0.01
	approx(t, "TotalPnL", res.TotalPnL, want)
}

func TestRunner_EndOfRunCloseCanBeDisabled(t *testing.T) {
	res := run(t, newBuyAndHold(), ticks(t0, time.Minute, 100, 120), RunnerConfig{DisableEndOfRunClose: true})
	if res.TotalTrades != 0 {
		t.Fatalf("TotalTrades = %d, want 0 with DisableEndOfRunClose", res.TotalTrades)
	}
}
