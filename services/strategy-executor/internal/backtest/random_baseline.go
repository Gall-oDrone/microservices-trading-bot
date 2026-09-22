package backtest

import (
	"math/rand"
	"sync"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

// randomBaseline is a null-hypothesis strategy: it enters and exits on a coin
// flip, with no reference to any indicator.
//
// It exists to answer the only question that makes a backtest number
// meaningful — "is this better than nothing?". A strategy that turns a profit
// on a rising sample is not demonstrating edge; a random entry would do the
// same. Comparing against this baseline separates signal from drift.
//
// It is deliberately confined to the backtest package and registered only in
// the backtest engine's factory table. It is not in the live enhanced registry
// and cannot be started by the running service.
type randomBaseline struct {
	*strategies.BaseEnhancedStrategy

	mu         sync.Mutex
	rng        *rand.Rand
	inPosition bool
	amount     float64
	// entryProb/exitProb are per-tick probabilities. They are kept low because
	// the engine ticks once per trade; a high probability would churn every
	// tick and measure nothing but the fee drag.
	entryProb float64
	exitProb  float64
}

// RandomBaselineSeed is the default RNG seed. It is fixed so that a baseline
// comparison is reproducible — a baseline that changes between runs cannot be
// used to judge whether a strategy improved.
const RandomBaselineSeed int64 = 20260919

func newRandomBaseline() strategies.EnhancedStrategy {
	return &randomBaseline{
		BaseEnhancedStrategy: strategies.NewBaseEnhancedStrategy("random_baseline", "1.0.0"),
		rng:                  rand.New(rand.NewSource(RandomBaselineSeed)),
		amount:               0.001,
		entryProb:            0.002,
		exitProb:             0.01,
	}
}

// init registers the baseline with the engine's factory table.
//
// Registration happens here rather than in engine.go so that adding the
// baseline touches no existing file and cannot perturb the real strategies.
func init() {
	strategyFactories["random_baseline"] = newRandomBaseline
}

// Initialize reads the optional tuning parameters.
func (s *randomBaseline) Initialize(config strategies.StrategyConfig, indicatorSvc *indicators.Service) error {
	if err := s.BaseEnhancedStrategy.Initialize(config, indicatorSvc); err != nil {
		return err
	}
	if p := config.Parameters; p != nil {
		if v, ok := p["position_size"].(float64); ok && v > 0 {
			s.amount = v
		}
		if v, ok := p["entry_probability"].(float64); ok && v > 0 {
			s.entryProb = v
		}
		if v, ok := p["exit_probability"].(float64); ok && v > 0 {
			s.exitProb = v
		}
		if v, ok := p["seed"].(float64); ok {
			s.rng = rand.New(rand.NewSource(int64(v)))
		}
	}
	return nil
}

// OnTick flips a coin.
func (s *randomBaseline) OnTick(tick *indicators.Trade) (*strategies.Signal, error) {
	if !s.IsRunning() || tick == nil || tick.Price <= 0 {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.inPosition {
		if s.rng.Float64() >= s.entryProb {
			return nil, nil
		}
		s.inPosition = true
		s.RecordSignal()
		return &strategies.Signal{
			Strategy:   s.Name(),
			Book:       s.GetConfig().Book,
			Side:       "BUY",
			Amount:     s.amount,
			Price:      tick.Price,
			Confidence: 0.5,
			Reason:     "random baseline entry",
			Timestamp:  tick.Timestamp,
			Metadata:   map[string]interface{}{"signal_type": "entry_long", "baseline": true},
		}, nil
	}

	if s.rng.Float64() >= s.exitProb {
		return nil, nil
	}
	s.inPosition = false
	s.RecordSignal()
	return &strategies.Signal{
		Strategy:   s.Name(),
		Book:       s.GetConfig().Book,
		Side:       "SELL",
		Amount:     s.amount,
		Price:      tick.Price,
		Confidence: 0.5,
		Reason:     "random baseline exit",
		Timestamp:  tick.Timestamp,
		Metadata:   map[string]interface{}{"signal_type": "exit", "baseline": true},
	}, nil
}

// OnBar routes bars through the same coin flip.
func (s *randomBaseline) OnBar(bar *indicators.OHLCV) (*strategies.Signal, error) {
	if bar == nil {
		return nil, nil
	}
	return s.OnTick(&indicators.Trade{Timestamp: bar.Timestamp, Price: bar.Close, Amount: bar.Volume})
}

// Reset clears position state and restores the seed so repeated runs match.
func (s *randomBaseline) Reset() {
	s.BaseEnhancedStrategy.Reset()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inPosition = false
	s.rng = rand.New(rand.NewSource(RandomBaselineSeed))
}

var _ strategies.EnhancedStrategy = (*randomBaseline)(nil)
var _ = time.Now // keep the time import stable for future timestamp work
