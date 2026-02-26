package strategy

import (
	"fmt"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/indicators"
)

// VolatilityBreakoutStrategy enters when volatility is high and price breaks recent range (SMA ± multiple of volatility).
const maxPricesVol = 150

// VolatilityBreakoutStrategy for backtesting.
type VolatilityBreakoutStrategy struct {
	name string

	volatilityPeriod int
	volatilityMult   float64 // e.g. 2.0 = break when price > SMA + 2*vol
	smaPeriod        int
	minInterval      time.Duration

	priceHistory   []float64
	lastSignalTime time.Time
}

// NewVolatilityBreakoutStrategy creates a new volatility breakout strategy.
func NewVolatilityBreakoutStrategy(params map[string]interface{}) (Strategy, error) {
	s := &VolatilityBreakoutStrategy{
		name:              "volatility_breakout",
		volatilityPeriod:   20,
		volatilityMult:     2.0,
		smaPeriod:          20,
		minInterval:         2 * time.Minute,
		priceHistory:       make([]float64, 0, maxPricesVol),
	}
	if err := s.Initialize(params); err != nil {
		return nil, err
	}
	return s, nil
}

// Initialize initializes the strategy with parameters.
func (s *VolatilityBreakoutStrategy) Initialize(params map[string]interface{}) error {
	if v, ok := params["volatility_period"]; ok {
		switch x := v.(type) {
		case float64:
			s.volatilityPeriod = int(x)
		case int:
			s.volatilityPeriod = x
		}
	}
	if v, ok := params["volatility_mult"]; ok {
		switch x := v.(type) {
		case float64:
			s.volatilityMult = x
		case int:
			s.volatilityMult = float64(x)
		}
	}
	if v, ok := params["sma_period"]; ok {
		switch x := v.(type) {
		case float64:
			s.smaPeriod = int(x)
		case int:
			s.smaPeriod = x
		}
	}
	return nil
}

func (s *VolatilityBreakoutStrategy) addPrice(price float64) {
	s.priceHistory = append(s.priceHistory, price)
	if len(s.priceHistory) > maxPricesVol {
		s.priceHistory = s.priceHistory[len(s.priceHistory)-maxPricesVol:]
	}
}

// OnTrade processes a trade event.
func (s *VolatilityBreakoutStrategy) OnTrade(trade *bitso.Trade) (*Signal, error) {
	price := trade.Price.Float64()
	book := trade.Book.String()
	ts := trade.CreatedAt.Time()
	s.addPrice(price)

	n := s.volatilityPeriod
	if n < s.smaPeriod {
		n = s.smaPeriod
	}
	if len(s.priceHistory) < n+1 {
		return NewSignal(SignalHold, book, price, 0), nil
	}
	if time.Since(s.lastSignalTime) < s.minInterval {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	vol := indicators.Volatility(s.priceHistory, s.volatilityPeriod)
	sma := indicators.SMA(s.priceHistory, s.smaPeriod)
	upper := sma + s.volatilityMult*vol*sma
	lower := sma - s.volatilityMult*vol*sma
	if lower <= 0 {
		lower = sma * 0.99
	}

	if vol <= 0 {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	if price >= upper {
		s.lastSignalTime = ts
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("Breakout above SMA+%.1f*vol (price=%.2f, upper=%.2f)", s.volatilityMult, price, upper)).
			WithMetadata("volatility", vol).WithMetadata("sma", sma), nil
	}
	if price <= lower {
		s.lastSignalTime = ts
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("Breakout below SMA-%.1f*vol (price=%.2f, lower=%.2f)", s.volatilityMult, price, lower)).
			WithMetadata("volatility", vol).WithMetadata("sma", sma), nil
	}
	return NewSignal(SignalHold, book, price, 0), nil
}

// OnTicker processes a ticker event.
func (s *VolatilityBreakoutStrategy) OnTicker(ticker *bitso.Ticker) (*Signal, error) {
	price := ticker.Last.Float64()
	book := ticker.Book.String()
	ts := ticker.CreatedAt.Time()
	s.addPrice(price)

	n := s.volatilityPeriod
	if n < s.smaPeriod {
		n = s.smaPeriod
	}
	if len(s.priceHistory) < n+1 {
		return NewSignal(SignalHold, book, price, 0), nil
	}
	if time.Since(s.lastSignalTime) < s.minInterval {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	vol := indicators.Volatility(s.priceHistory, s.volatilityPeriod)
	sma := indicators.SMA(s.priceHistory, s.smaPeriod)
	upper := sma + s.volatilityMult*vol*sma
	lower := sma - s.volatilityMult*vol*sma
	if lower <= 0 {
		lower = sma * 0.99
	}
	if vol <= 0 {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	if price >= upper {
		s.lastSignalTime = ts
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("Breakout above SMA+%.1f*vol", s.volatilityMult)), nil
	}
	if price <= lower {
		s.lastSignalTime = ts
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("Breakout below SMA-%.1f*vol", s.volatilityMult)), nil
	}
	return NewSignal(SignalHold, book, price, 0), nil
}

// OnOrderBook does not generate signals.
func (s *VolatilityBreakoutStrategy) OnOrderBook(orderBook interface{}) (*Signal, error) {
	return NewSignal(SignalHold, "", 0, 0), nil
}

// GetName returns the strategy name.
func (s *VolatilityBreakoutStrategy) GetName() string { return s.name }

// Reset resets the strategy state.
func (s *VolatilityBreakoutStrategy) Reset() error {
	s.priceHistory = make([]float64, 0, maxPricesVol)
	s.lastSignalTime = time.Time{}
	return nil
}
