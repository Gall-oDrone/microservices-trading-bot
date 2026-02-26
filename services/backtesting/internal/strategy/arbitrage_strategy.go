package strategy

import (
	"fmt"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// ArbitrageStrategy implements an arbitrage-style strategy for backtesting.
// With only trade prices (no bid/ask), we use price change vs previous trade:
// buy when price increased above threshold, sell when price decreased.
type ArbitrageStrategy struct {
	name string

	// Parameters
	profitThreshold float64
	minInterval     time.Duration

	// State
	lastPrice       float64
	lastSignalTime  time.Time
}

// NewArbitrageStrategy creates a new arbitrage strategy for backtesting.
func NewArbitrageStrategy(params map[string]interface{}) (Strategy, error) {
	s := &ArbitrageStrategy{
		name:            "arbitrage",
		profitThreshold: 0.005,
		minInterval:     2 * time.Second,
	}
	if err := s.Initialize(params); err != nil {
		return nil, err
	}
	return s, nil
}

// Initialize initializes the strategy with parameters.
func (s *ArbitrageStrategy) Initialize(params map[string]interface{}) error {
	if v, ok := params["profit_threshold_pct"]; ok {
		switch x := v.(type) {
		case float64:
			s.profitThreshold = x
		case int:
			s.profitThreshold = float64(x) / 100
		}
	}
	return nil
}

// OnTrade processes a trade event.
func (s *ArbitrageStrategy) OnTrade(trade *bitso.Trade) (*Signal, error) {
	price := trade.Price.Float64()
	book := trade.Book.String()
	ts := trade.CreatedAt.Time()

	if s.lastPrice <= 0 {
		s.lastPrice = price
		return NewSignal(SignalHold, book, price, 0), nil
	}

	if time.Since(s.lastSignalTime) < s.minInterval {
		s.lastPrice = price
		return NewSignal(SignalHold, book, price, 0), nil
	}

	change := (price - s.lastPrice) / s.lastPrice
	s.lastPrice = price

	if change > s.profitThreshold {
		s.lastSignalTime = ts
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("Price up %.2f%% (threshold %.2f%%)", change*100, s.profitThreshold*100)), nil
	}
	if change < -s.profitThreshold {
		s.lastSignalTime = ts
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("Price down %.2f%% (threshold %.2f%%)", -change*100, s.profitThreshold*100)), nil
	}
	return NewSignal(SignalHold, book, price, 0), nil
}

// OnTicker processes a ticker event (use last price as single price).
func (s *ArbitrageStrategy) OnTicker(ticker *bitso.Ticker) (*Signal, error) {
	price := ticker.Last.Float64()
	book := ticker.Book.String()
	ts := ticker.CreatedAt.Time()

	if s.lastPrice <= 0 {
		s.lastPrice = price
		return NewSignal(SignalHold, book, price, 0), nil
	}

	if time.Since(s.lastSignalTime) < s.minInterval {
		s.lastPrice = price
		return NewSignal(SignalHold, book, price, 0), nil
	}

	change := (price - s.lastPrice) / s.lastPrice
	s.lastPrice = price

	if change > s.profitThreshold {
		s.lastSignalTime = ts
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("Price up %.2f%%", change*100)), nil
	}
	if change < -s.profitThreshold {
		s.lastSignalTime = ts
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("Price down %.2f%%", -change*100)), nil
	}
	return NewSignal(SignalHold, book, price, 0), nil
}

// OnOrderBook processes an order book event (no signal).
func (s *ArbitrageStrategy) OnOrderBook(orderBook interface{}) (*Signal, error) {
	return NewSignal(SignalHold, "", 0, 0), nil
}

// GetName returns the strategy name.
func (s *ArbitrageStrategy) GetName() string {
	return s.name
}

// Reset resets the strategy state.
func (s *ArbitrageStrategy) Reset() error {
	s.lastPrice = 0
	s.lastSignalTime = time.Time{}
	return nil
}
