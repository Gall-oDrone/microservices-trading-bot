package strategy

import (
	"fmt"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/indicators"
)

// RSIMomentumStrategy combines RSI overbought/oversold with short-term momentum confirmation.
// Buy: RSI oversold AND momentum (1m) positive or improving. Sell: RSI overbought AND momentum negative.
const maxPricesRSIMom = 100

// RSIMomentumStrategy for backtesting.
type RSIMomentumStrategy struct {
	name string

	rsiPeriod     int
	rsiOversold   float64
	rsiOverbought float64
	momentumLookback int
	minInterval   time.Duration

	priceHistory   []float64
	lastSignalTime time.Time
}

// NewRSIMomentumStrategy creates a new RSI + momentum strategy.
func NewRSIMomentumStrategy(params map[string]interface{}) (Strategy, error) {
	s := &RSIMomentumStrategy{
		name:             "rsi_momentum",
		rsiPeriod:        14,
		rsiOversold:       indicators.DefaultRSIOversold,
		rsiOverbought:    indicators.DefaultRSIOverbought,
		momentumLookback:  5,
		minInterval:      1 * time.Minute,
		priceHistory:     make([]float64, 0, maxPricesRSIMom),
	}
	if err := s.Initialize(params); err != nil {
		return nil, err
	}
	return s, nil
}

// Initialize initializes the strategy with parameters.
func (s *RSIMomentumStrategy) Initialize(params map[string]interface{}) error {
	if v, ok := params["rsi_period"]; ok {
		switch x := v.(type) {
		case float64:
			s.rsiPeriod = int(x)
		case int:
			s.rsiPeriod = x
		}
	}
	if v, ok := params["rsi_oversold"]; ok {
		switch x := v.(type) {
		case float64:
			s.rsiOversold = x
		case int:
			s.rsiOversold = float64(x)
		}
	}
	if v, ok := params["rsi_overbought"]; ok {
		switch x := v.(type) {
		case float64:
			s.rsiOverbought = x
		case int:
			s.rsiOverbought = float64(x)
		}
	}
	if v, ok := params["momentum_lookback"]; ok {
		switch x := v.(type) {
		case float64:
			s.momentumLookback = int(x)
		case int:
			s.momentumLookback = x
		}
	}
	return nil
}

func (s *RSIMomentumStrategy) addPrice(price float64) {
	s.priceHistory = append(s.priceHistory, price)
	if len(s.priceHistory) > maxPricesRSIMom {
		s.priceHistory = s.priceHistory[len(s.priceHistory)-maxPricesRSIMom:]
	}
}

// OnTrade processes a trade event.
func (s *RSIMomentumStrategy) OnTrade(trade *bitso.Trade) (*Signal, error) {
	price := trade.Price.Float64()
	book := trade.Book.String()
	ts := trade.CreatedAt.Time()
	s.addPrice(price)

	if len(s.priceHistory) < s.rsiPeriod+s.momentumLookback+1 {
		return NewSignal(SignalHold, book, price, 0), nil
	}
	if time.Since(s.lastSignalTime) < s.minInterval {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	rsi := indicators.RSI(s.priceHistory, s.rsiPeriod)
	momentum := indicators.Momentum(s.priceHistory, s.momentumLookback)
	state := indicators.RSIStateFromLevels(rsi, s.rsiOversold, s.rsiOverbought)

	if state == indicators.RSIOversold && momentum >= -0.005 {
		s.lastSignalTime = ts
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("RSI oversold %.1f, momentum %.2f%%", rsi, momentum*100)).
			WithMetadata("rsi", rsi).WithMetadata("momentum", momentum), nil
	}
	if state == indicators.RSIOverbought && momentum <= 0.005 {
		s.lastSignalTime = ts
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("RSI overbought %.1f, momentum %.2f%%", rsi, momentum*100)).
			WithMetadata("rsi", rsi).WithMetadata("momentum", momentum), nil
	}
	return NewSignal(SignalHold, book, price, 0).WithMetadata("rsi", rsi).WithMetadata("momentum", momentum), nil
}

// OnTicker processes a ticker event.
func (s *RSIMomentumStrategy) OnTicker(ticker *bitso.Ticker) (*Signal, error) {
	price := ticker.Last.Float64()
	book := ticker.Book.String()
	ts := ticker.CreatedAt.Time()
	s.addPrice(price)

	if len(s.priceHistory) < s.rsiPeriod+s.momentumLookback+1 {
		return NewSignal(SignalHold, book, price, 0), nil
	}
	if time.Since(s.lastSignalTime) < s.minInterval {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	rsi := indicators.RSI(s.priceHistory, s.rsiPeriod)
	momentum := indicators.Momentum(s.priceHistory, s.momentumLookback)
	state := indicators.RSIStateFromLevels(rsi, s.rsiOversold, s.rsiOverbought)

	if state == indicators.RSIOversold && momentum >= -0.005 {
		s.lastSignalTime = ts
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("RSI oversold %.1f, momentum %.2f%%", rsi, momentum*100)), nil
	}
	if state == indicators.RSIOverbought && momentum <= 0.005 {
		s.lastSignalTime = ts
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("RSI overbought %.1f, momentum %.2f%%", rsi, momentum*100)), nil
	}
	return NewSignal(SignalHold, book, price, 0), nil
}

// OnOrderBook does not generate signals.
func (s *RSIMomentumStrategy) OnOrderBook(orderBook interface{}) (*Signal, error) {
	return NewSignal(SignalHold, "", 0, 0), nil
}

// GetName returns the strategy name.
func (s *RSIMomentumStrategy) GetName() string { return s.name }

// Reset resets the strategy state.
func (s *RSIMomentumStrategy) Reset() error {
	s.priceHistory = make([]float64, 0, maxPricesRSIMom)
	s.lastSignalTime = time.Time{}
	return nil
}
