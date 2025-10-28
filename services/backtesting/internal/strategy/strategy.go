package strategy

import (
	"fmt"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// SignalType represents the type of trading signal
type SignalType string

const (
	SignalNone SignalType = "NONE"
	SignalBuy  SignalType = "BUY"
	SignalSell SignalType = "SELL"
	SignalHold SignalType = "HOLD"
)

// Signal represents a trading signal
type Signal struct {
	Type       SignalType             `json:"type"`
	Book       string                 `json:"book"`
	Price      float64                `json:"price"`
	Amount     float64                `json:"amount"`
	Confidence float64                `json:"confidence"` // 0.0 to 1.0
	Reason     string                 `json:"reason"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
}

// Strategy defines the interface for trading strategies
// Adapted from strategy-executor for synchronous backtesting
type Strategy interface {
	// Initialize initializes the strategy with parameters
	Initialize(params map[string]interface{}) error
	
	// OnTrade processes a trade event and returns a signal
	OnTrade(trade *bitso.Trade) (*Signal, error)
	
	// OnTicker processes a ticker event and returns a signal
	OnTicker(ticker *bitso.Ticker) (*Signal, error)
	
	// OnOrderBook processes an order book event and returns a signal
	OnOrderBook(orderBook interface{}) (*Signal, error)
	
	// GetName returns the strategy name
	GetName() string
	
	// Reset resets the strategy state
	Reset() error
}

// NewSignal creates a new signal
func NewSignal(signalType SignalType, book string, price, amount float64) *Signal {
	return &Signal{
		Type:       signalType,
		Book:       book,
		Price:      price,
		Amount:     amount,
		Confidence: 1.0,
		Timestamp:  time.Now(),
		Metadata:   make(map[string]interface{}),
	}
}

// WithReason sets the signal reason
func (s *Signal) WithReason(reason string) *Signal {
	s.Reason = reason
	return s
}

// WithConfidence sets the signal confidence
func (s *Signal) WithConfidence(confidence float64) *Signal {
	s.Confidence = confidence
	return s
}

// WithMetadata adds metadata to the signal
func (s *Signal) WithMetadata(key string, value interface{}) *Signal {
	s.Metadata[key] = value
	return s
}

// IsBuySignal returns true if this is a buy signal
func (s *Signal) IsBuySignal() bool {
	return s.Type == SignalBuy
}

// IsSellSignal returns true if this is a sell signal
func (s *Signal) IsSellSignal() bool {
	return s.Type == SignalSell
}

// IsActionableSignal returns true if this signal requires action
func (s *Signal) IsActionableSignal() bool {
	return s.Type == SignalBuy || s.Type == SignalSell
}

// Validate validates the signal
func (s *Signal) Validate() error {
	if s.Type == "" {
		return fmt.Errorf("signal type is required")
	}
	
	if s.Book == "" {
		return fmt.Errorf("book is required")
	}
	
	if s.IsActionableSignal() {
		if s.Price <= 0 {
			return fmt.Errorf("price must be positive for actionable signals")
		}
		if s.Amount <= 0 {
			return fmt.Errorf("amount must be positive for actionable signals")
		}
	}
	
	if s.Confidence < 0 || s.Confidence > 1.0 {
		return fmt.Errorf("confidence must be between 0 and 1, got: %f", s.Confidence)
	}
	
	return nil
}

