package strategy

import (
	"fmt"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/indicators"
)

// BasicStrategy implements a simple RSI-based trading strategy
// Adapted from strategy-executor for synchronous backtesting
type BasicStrategy struct {
	name string

	// Parameters
	rsiPeriod     int
	rsiOversold   float64
	rsiOverbought float64

	// State
	priceHistory   []float64
	rsiValues      []float64
	lastSignalTime time.Time
	minInterval    time.Duration
}

// NewBasicStrategy creates a new basic strategy
func NewBasicStrategy(params map[string]interface{}) (Strategy, error) {
	strategy := &BasicStrategy{
		name:          "basic",
		rsiPeriod:     14, // Default
		rsiOversold:   35, // Default (slightly relaxed so synthetic/backtest data produces trades)
		rsiOverbought: 65, // Default
		priceHistory:  make([]float64, 0),
		rsiValues:     make([]float64, 0),
		minInterval:   1 * time.Second, // Allow signals close together in backtest (was 5*time.Minute)
	}

	if err := strategy.Initialize(params); err != nil {
		return nil, err
	}

	return strategy, nil
}

// Initialize initializes the strategy with parameters
func (s *BasicStrategy) Initialize(params map[string]interface{}) error {
	// Parse RSI period
	if period, ok := params["rsi_period"]; ok {
		switch v := period.(type) {
		case float64:
			s.rsiPeriod = int(v)
		case int:
			s.rsiPeriod = v
		}
	}

	// Parse RSI oversold level
	if oversold, ok := params["rsi_oversold"]; ok {
		switch v := oversold.(type) {
		case float64:
			s.rsiOversold = v
		case int:
			s.rsiOversold = float64(v)
		}
	}

	// Parse RSI overbought level
	if overbought, ok := params["rsi_overbought"]; ok {
		switch v := overbought.(type) {
		case float64:
			s.rsiOverbought = v
		case int:
			s.rsiOverbought = float64(v)
		}
	}

	// Validate parameters
	if s.rsiPeriod < 2 || s.rsiPeriod > 100 {
		return fmt.Errorf("rsi_period must be between 2 and 100, got: %d", s.rsiPeriod)
	}

	if s.rsiOversold >= s.rsiOverbought {
		return fmt.Errorf("rsi_oversold must be less than rsi_overbought")
	}

	return nil
}

// OnTrade processes a trade event
func (s *BasicStrategy) OnTrade(trade *bitso.Trade) (*Signal, error) {
	price := trade.Price.Float64()
	book := trade.Book.String()
	timestamp := trade.CreatedAt.Time()

	// Add price to history
	s.priceHistory = append(s.priceHistory, price)

	// Calculate RSI if we have enough data (using shared indicator)
	if len(s.priceHistory) >= s.rsiPeriod+1 {
		rsi := indicators.RSI(s.priceHistory, s.rsiPeriod)
		s.rsiValues = append(s.rsiValues, rsi)

		// Generate signal based on RSI
		return s.generateSignalFromRSI(rsi, book, price, timestamp)
	}

	// Not enough data yet
	return NewSignal(SignalHold, book, price, 0), nil
}

// OnTicker processes a ticker event
func (s *BasicStrategy) OnTicker(ticker *bitso.Ticker) (*Signal, error) {
	price := ticker.Last.Float64()
	book := ticker.Book.String()
	timestamp := ticker.CreatedAt.Time()

	// Add price to history
	s.priceHistory = append(s.priceHistory, price)

	// Calculate RSI if we have enough data (using shared indicator)
	if len(s.priceHistory) >= s.rsiPeriod+1 {
		rsi := indicators.RSI(s.priceHistory, s.rsiPeriod)
		s.rsiValues = append(s.rsiValues, rsi)

		// Generate signal based on RSI
		return s.generateSignalFromRSI(rsi, book, price, timestamp)
	}

	// Not enough data yet
	return NewSignal(SignalHold, book, price, 0), nil
}

// OnOrderBook processes an order book event
func (s *BasicStrategy) OnOrderBook(orderBook interface{}) (*Signal, error) {
	// Basic strategy doesn't use order book
	return NewSignal(SignalHold, "", 0, 0), nil
}

// GetName returns the strategy name
func (s *BasicStrategy) GetName() string {
	return s.name
}

// Reset resets the strategy state
func (s *BasicStrategy) Reset() error {
	s.priceHistory = make([]float64, 0)
	s.rsiValues = make([]float64, 0)
	s.lastSignalTime = time.Time{}
	return nil
}

// generateSignalFromRSI generates a trading signal based on RSI
func (s *BasicStrategy) generateSignalFromRSI(rsi float64, book string, price float64, timestamp time.Time) (*Signal, error) {
	// Check minimum interval between signals
	if time.Since(s.lastSignalTime) < s.minInterval {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	// Generate signal based on RSI levels
	if rsi < s.rsiOversold {
		// Oversold - BUY signal
		s.lastSignalTime = timestamp
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("RSI oversold: %.2f < %.2f", rsi, s.rsiOversold)).
			WithMetadata("rsi", rsi).
			WithConfidence(calculateConfidence(rsi, s.rsiOversold, 0)), nil

	} else if rsi > s.rsiOverbought {
		// Overbought - SELL signal
		s.lastSignalTime = timestamp
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("RSI overbought: %.2f > %.2f", rsi, s.rsiOverbought)).
			WithMetadata("rsi", rsi).
			WithConfidence(calculateConfidence(rsi, s.rsiOverbought, 100)), nil
	}

	// No signal - HOLD
	return NewSignal(SignalHold, book, price, 0).
		WithMetadata("rsi", rsi), nil
}

// calculateConfidence calculates signal confidence based on how extreme the RSI is
func calculateConfidence(rsi, threshold, extreme float64) float64 {
	distance := abs(rsi - threshold)
	maxDistance := abs(extreme - threshold)

	if maxDistance == 0 {
		return 1.0
	}

	confidence := 0.5 + (distance / maxDistance * 0.5)
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// abs returns the absolute value
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
