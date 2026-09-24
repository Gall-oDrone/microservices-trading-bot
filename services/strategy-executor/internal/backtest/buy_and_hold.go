package backtest

import (
	"sync"

	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

// buyAndHold is the passive benchmark: buy on the first usable tick, never
// sell. The runner closes the position at the last price when the replay ends,
// charging slippage and commission on both legs like any other trade.
//
// It is the bar an active strategy must clear. The random baseline answers
// "is this better than noise?"; buy-and-hold answers the harder and more
// relevant question, "is this better than doing nothing?". Over the
// 2026-08-19..2026-09-22 archive window btc_mxn rose ~29%, and buy-and-hold beat
// every active strategy by a wide margin. A strategy that trades hundreds of
// times and underperforms one round trip is destroying value through churn.
//
// Like randomBaseline it is registered only in the backtest engine's factory
// table and cannot be started by the live service.
type buyAndHold struct {
	*strategies.BaseEnhancedStrategy

	mu     sync.Mutex
	bought bool
	amount float64
}

func newBuyAndHold() strategies.EnhancedStrategy {
	return &buyAndHold{
		BaseEnhancedStrategy: strategies.NewBaseEnhancedStrategy("buy_and_hold", "1.0.0"),
		// Same notional as the other strategies' defaults so that per-trade
		// MXN figures are directly comparable.
		amount: 0.001,
	}
}

func init() {
	strategyFactories["buy_and_hold"] = newBuyAndHold
}

// Initialize reads the optional position size.
func (s *buyAndHold) Initialize(config strategies.StrategyConfig, indicatorSvc *indicators.Service) error {
	if err := s.BaseEnhancedStrategy.Initialize(config, indicatorSvc); err != nil {
		return err
	}
	if p := config.Parameters; p != nil {
		if v, ok := p["position_size"].(float64); ok && v > 0 {
			s.amount = v
		}
	}
	return nil
}

// OnTick buys exactly once.
func (s *buyAndHold) OnTick(tick *indicators.Trade) (*strategies.Signal, error) {
	if !s.IsRunning() || tick == nil || tick.Price <= 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bought {
		return nil, nil
	}
	s.bought = true
	s.RecordSignal()
	return &strategies.Signal{
		Strategy:   s.Name(),
		Book:       s.GetConfig().Book,
		Side:       "BUY",
		Amount:     s.amount,
		Price:      tick.Price,
		Confidence: 1,
		Reason:     "buy and hold entry",
		Timestamp:  tick.Timestamp,
		Metadata:   map[string]interface{}{"signal_type": "entry_long", "baseline": true},
	}, nil
}

// OnBar routes bars through OnTick.
func (s *buyAndHold) OnBar(bar *indicators.OHLCV) (*strategies.Signal, error) {
	if bar == nil {
		return nil, nil
	}
	return s.OnTick(&indicators.Trade{Timestamp: bar.Timestamp, Price: bar.Close, Amount: bar.Volume})
}

// Reset allows a fresh run.
func (s *buyAndHold) Reset() {
	s.BaseEnhancedStrategy.Reset()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bought = false
}

var _ strategies.EnhancedStrategy = (*buyAndHold)(nil)
