package strategy

import (
	"fmt"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// TrendStrategy implements a trend-following strategy for backtesting.
// Uses price history from trades/tickers; buys on bullish trend + momentum, sells on bearish.
type TrendStrategy struct {
	name string

	// Parameters
	momentumThreshold float64 // min momentum to trigger (e.g. 0.015)
	minInterval       time.Duration
	maxHistorySize    int // cap price history (e.g. 30)

	// State
	priceHistory   []float64
	lastSignalTime time.Time
}

// NewTrendStrategy creates a new trend strategy for backtesting.
func NewTrendStrategy(params map[string]interface{}) (Strategy, error) {
	s := &TrendStrategy{
		name:              "trend",
		momentumThreshold: 0.015,
		minInterval:       5 * time.Second,
		maxHistorySize:    30,
		priceHistory:      make([]float64, 0),
	}
	if err := s.Initialize(params); err != nil {
		return nil, err
	}
	return s, nil
}

// Initialize initializes the strategy with parameters.
func (s *TrendStrategy) Initialize(params map[string]interface{}) error {
	if v, ok := params["momentum_threshold_pct"]; ok {
		switch x := v.(type) {
		case float64:
			s.momentumThreshold = x
		case int:
			s.momentumThreshold = float64(x)
		}
	}
	if v, ok := params["trend_period_minutes"]; ok {
		switch x := v.(type) {
		case float64:
			s.maxHistorySize = int(x)
		case int:
			s.maxHistorySize = x
		}
	}
	if s.maxHistorySize < 5 {
		s.maxHistorySize = 5
	}
	return nil
}

func (s *TrendStrategy) addPrice(price float64) {
	s.priceHistory = append(s.priceHistory, price)
	if len(s.priceHistory) > s.maxHistorySize {
		s.priceHistory = s.priceHistory[len(s.priceHistory)-s.maxHistorySize:]
	}
}

func (s *TrendStrategy) calculateTrend() float64 {
	if len(s.priceHistory) < 2 {
		return 0
	}
	first := s.priceHistory[0]
	last := s.priceHistory[len(s.priceHistory)-1]
	return (last - first) / first
}

func (s *TrendStrategy) calculateMomentum() float64 {
	if len(s.priceHistory) < 3 {
		return 0
	}
	recent := s.priceHistory[len(s.priceHistory)-3:]
	momentum := 0.0
	for i := 1; i < len(recent); i++ {
		momentum += (recent[i] - recent[i-1]) / recent[i-1]
	}
	return momentum / float64(len(recent)-1)
}

// OnTrade processes a trade event.
func (s *TrendStrategy) OnTrade(trade *bitso.Trade) (*Signal, error) {
	price := trade.Price.Float64()
	book := trade.Book.String()
	ts := trade.CreatedAt.Time()

	s.addPrice(price)

	if len(s.priceHistory) < 5 {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	if time.Since(s.lastSignalTime) < s.minInterval {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	trend := s.calculateTrend()
	momentum := s.calculateMomentum()

	if trend > 0 && momentum > s.momentumThreshold {
		s.lastSignalTime = ts
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("Bullish trend %.2f%%, momentum %.2f%%", trend*100, momentum*100)), nil
	}
	if trend < 0 && momentum > s.momentumThreshold {
		s.lastSignalTime = ts
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("Bearish trend %.2f%%, momentum %.2f%%", trend*100, momentum*100)), nil
	}
	return NewSignal(SignalHold, book, price, 0), nil
}

// OnTicker processes a ticker event.
func (s *TrendStrategy) OnTicker(ticker *bitso.Ticker) (*Signal, error) {
	price := ticker.Last.Float64()
	book := ticker.Book.String()
	ts := ticker.CreatedAt.Time()

	s.addPrice(price)

	if len(s.priceHistory) < 5 {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	if time.Since(s.lastSignalTime) < s.minInterval {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	trend := s.calculateTrend()
	momentum := s.calculateMomentum()

	if trend > 0 && momentum > s.momentumThreshold {
		s.lastSignalTime = ts
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("Bullish trend %.2f%%, momentum %.2f%%", trend*100, momentum*100)), nil
	}
	if trend < 0 && momentum > s.momentumThreshold {
		s.lastSignalTime = ts
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("Bearish trend %.2f%%, momentum %.2f%%", trend*100, momentum*100)), nil
	}
	return NewSignal(SignalHold, book, price, 0), nil
}

// OnOrderBook processes an order book event (no signal).
func (s *TrendStrategy) OnOrderBook(orderBook interface{}) (*Signal, error) {
	return NewSignal(SignalHold, "", 0, 0), nil
}

// GetName returns the strategy name.
func (s *TrendStrategy) GetName() string {
	return s.name
}

// Reset resets the strategy state.
func (s *TrendStrategy) Reset() error {
	s.priceHistory = make([]float64, 0)
	s.lastSignalTime = time.Time{}
	return nil
}
