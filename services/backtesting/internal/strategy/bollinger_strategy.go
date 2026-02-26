package strategy

import (
	"fmt"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/indicators"
)

// BollingerStrategy implements mean reversion using Bollinger Bands.
// Buy when price at or below lower band, sell when at or above upper band.
const maxPricesBollinger = 100

// BollingerStrategy for backtesting.
type BollingerStrategy struct {
	name string

	period     int
	numStdDev  float64
	minInterval time.Duration

	priceHistory   []float64
	lastSignalTime time.Time
}

// NewBollingerStrategy creates a new Bollinger Bands strategy.
func NewBollingerStrategy(params map[string]interface{}) (Strategy, error) {
	s := &BollingerStrategy{
		name:           "bollinger",
		period:         20,
		numStdDev:       indicators.DefaultBollingerStdDev,
		minInterval:    1 * time.Minute,
		priceHistory:   make([]float64, 0, maxPricesBollinger),
	}
	if err := s.Initialize(params); err != nil {
		return nil, err
	}
	return s, nil
}

// Initialize initializes the strategy with parameters.
func (s *BollingerStrategy) Initialize(params map[string]interface{}) error {
	if v, ok := params["period"]; ok {
		switch x := v.(type) {
		case float64:
			s.period = int(x)
		case int:
			s.period = x
		}
	}
	if v, ok := params["num_std_dev"]; ok {
		switch x := v.(type) {
		case float64:
			s.numStdDev = x
		case int:
			s.numStdDev = float64(x)
		}
	}
	if s.period < 5 {
		s.period = 5
	}
	return nil
}

func (s *BollingerStrategy) addPrice(price float64) {
	s.priceHistory = append(s.priceHistory, price)
	if len(s.priceHistory) > maxPricesBollinger {
		s.priceHistory = s.priceHistory[len(s.priceHistory)-maxPricesBollinger:]
	}
}

// OnTrade processes a trade event.
func (s *BollingerStrategy) OnTrade(trade *bitso.Trade) (*Signal, error) {
	price := trade.Price.Float64()
	book := trade.Book.String()
	ts := trade.CreatedAt.Time()
	s.addPrice(price)

	if len(s.priceHistory) < s.period {
		return NewSignal(SignalHold, book, price, 0), nil
	}
	if time.Since(s.lastSignalTime) < s.minInterval {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	bb := indicators.BollingerBands(s.priceHistory, s.period, s.numStdDev)
	// Band width as fraction of price for "at band" tolerance
	tolerance := (bb.Upper - bb.Lower) * 0.05

	if price <= bb.Lower+tolerance {
		s.lastSignalTime = ts
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("Price at lower Bollinger band (%.2f)", bb.Lower)).
			WithMetadata("lower", bb.Lower).WithMetadata("upper", bb.Upper).WithMetadata("middle", bb.Middle), nil
	}
	if price >= bb.Upper-tolerance {
		s.lastSignalTime = ts
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("Price at upper Bollinger band (%.2f)", bb.Upper)).
			WithMetadata("lower", bb.Lower).WithMetadata("upper", bb.Upper).WithMetadata("middle", bb.Middle), nil
	}
	return NewSignal(SignalHold, book, price, 0).
		WithMetadata("lower", bb.Lower).WithMetadata("upper", bb.Upper).WithMetadata("middle", bb.Middle), nil
}

// OnTicker processes a ticker event.
func (s *BollingerStrategy) OnTicker(ticker *bitso.Ticker) (*Signal, error) {
	price := ticker.Last.Float64()
	book := ticker.Book.String()
	ts := ticker.CreatedAt.Time()
	s.addPrice(price)

	if len(s.priceHistory) < s.period {
		return NewSignal(SignalHold, book, price, 0), nil
	}
	if time.Since(s.lastSignalTime) < s.minInterval {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	bb := indicators.BollingerBands(s.priceHistory, s.period, s.numStdDev)
	tolerance := (bb.Upper - bb.Lower) * 0.05

	if price <= bb.Lower+tolerance {
		s.lastSignalTime = ts
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("Price at lower Bollinger band")), nil
	}
	if price >= bb.Upper-tolerance {
		s.lastSignalTime = ts
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("Price at upper Bollinger band")), nil
	}
	return NewSignal(SignalHold, book, price, 0), nil
}

// OnOrderBook does not generate signals.
func (s *BollingerStrategy) OnOrderBook(orderBook interface{}) (*Signal, error) {
	return NewSignal(SignalHold, "", 0, 0), nil
}

// GetName returns the strategy name.
func (s *BollingerStrategy) GetName() string { return s.name }

// Reset resets the strategy state.
func (s *BollingerStrategy) Reset() error {
	s.priceHistory = make([]float64, 0, maxPricesBollinger)
	s.lastSignalTime = time.Time{}
	return nil
}
