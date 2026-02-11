package models

import (
	"encoding/json"
	"fmt"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// MarketEventType represents the type of market event
type MarketEventType string

const (
	EventTypeTrade     MarketEventType = "trade"
	EventTypeTicker    MarketEventType = "ticker"
	EventTypeOrderBook MarketEventType = "orderbook"
)

// MarketEvent represents a market data event
type MarketEvent struct {
	EventType MarketEventType `json:"event_type"`
	Timestamp time.Time       `json:"timestamp"`
	Book      string          `json:"book"`
	Data      interface{}     `json:"data"` // *bitso.Trade, *bitso.Ticker, or OrderBook data
}

// NewTradeEvent creates a market event from a trade
func NewTradeEvent(trade *bitso.Trade) *MarketEvent {
	return &MarketEvent{
		EventType: EventTypeTrade,
		Timestamp: trade.CreatedAt.Time(),
		Book:      trade.Book.String(),
		Data:      trade,
	}
}

// NewTickerEvent creates a market event from a ticker
func NewTickerEvent(ticker *bitso.Ticker) *MarketEvent {
	return &MarketEvent{
		EventType: EventTypeTicker,
		Timestamp: ticker.CreatedAt.Time(),
		Book:      ticker.Book.String(),
		Data:      ticker,
	}
}

// NewOrderBookEvent creates a market event from order book data
func NewOrderBookEvent(book string, timestamp time.Time, data interface{}) *MarketEvent {
	return &MarketEvent{
		EventType: EventTypeOrderBook,
		Timestamp: timestamp,
		Book:      book,
		Data:      data,
	}
}

// GetTrade returns the trade data if this is a trade event.
// Handles Data as *bitso.Trade (in-memory) or map from JSON cache (round-trip via json).
func (e *MarketEvent) GetTrade() (*bitso.Trade, error) {
	if e.EventType != EventTypeTrade {
		return nil, fmt.Errorf("event is not a trade event")
	}

	if trade, ok := e.Data.(*bitso.Trade); ok {
		return trade, nil
	}
	// Cache round-trip: JSON unmarshals Data as map[string]interface{}
	if m, ok := e.Data.(map[string]interface{}); ok {
		js, err := json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal cached trade: %w", err)
		}
		var t bitso.Trade
		if err := json.Unmarshal(js, &t); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cached trade: %w", err)
		}
		return &t, nil
	}

	return nil, fmt.Errorf("failed to cast event data to Trade")
}

// GetTicker returns the ticker data if this is a ticker event
func (e *MarketEvent) GetTicker() (*bitso.Ticker, error) {
	if e.EventType != EventTypeTicker {
		return nil, fmt.Errorf("event is not a ticker event")
	}

	ticker, ok := e.Data.(*bitso.Ticker)
	if !ok {
		return nil, fmt.Errorf("failed to cast event data to Ticker")
	}

	return ticker, nil
}

// GetOrderBook returns the order book data if this is an order book event
func (e *MarketEvent) GetOrderBook() (interface{}, error) {
	if e.EventType != EventTypeOrderBook {
		return nil, fmt.Errorf("event is not an order book event")
	}

	return e.Data, nil
}

// IsTradeEvent returns true if this is a trade event
func (e *MarketEvent) IsTradeEvent() bool {
	return e.EventType == EventTypeTrade
}

// IsTickerEvent returns true if this is a ticker event
func (e *MarketEvent) IsTickerEvent() bool {
	return e.EventType == EventTypeTicker
}

// IsOrderBookEvent returns true if this is an order book event
func (e *MarketEvent) IsOrderBookEvent() bool {
	return e.EventType == EventTypeOrderBook
}

// GetPrice returns the price from the event
func (e *MarketEvent) GetPrice() (float64, error) {
	switch e.EventType {
	case EventTypeTrade:
		trade, err := e.GetTrade()
		if err != nil {
			return 0, err
		}
		return trade.Price.Float64(), nil

	case EventTypeTicker:
		ticker, err := e.GetTicker()
		if err != nil {
			return 0, err
		}
		// Use last traded price
		return ticker.Last.Float64(), nil

	default:
		return 0, fmt.Errorf("cannot get price from event type: %s", e.EventType)
	}
}

// GetAmount returns the amount from the event (for trade events)
func (e *MarketEvent) GetAmount() (float64, error) {
	if e.EventType != EventTypeTrade {
		return 0, fmt.Errorf("only trade events have amount")
	}

	trade, err := e.GetTrade()
	if err != nil {
		return 0, err
	}

	return trade.Amount.Float64(), nil
}

// GetSide returns the side from the event (for trade events)
func (e *MarketEvent) GetSide() (string, error) {
	if e.EventType != EventTypeTrade {
		return "", fmt.Errorf("only trade events have side")
	}

	trade, err := e.GetTrade()
	if err != nil {
		return "", err
	}

	return trade.MakerSide.String(), nil
}

// Validate validates the market event
func (e *MarketEvent) Validate() error {
	if e.Book == "" {
		return fmt.Errorf("book is required")
	}

	if e.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is required")
	}

	validTypes := map[MarketEventType]bool{
		EventTypeTrade:     true,
		EventTypeTicker:    true,
		EventTypeOrderBook: true,
	}

	if !validTypes[e.EventType] {
		return fmt.Errorf("invalid event type: %s", e.EventType)
	}

	if e.Data == nil {
		return fmt.Errorf("event data is required")
	}

	return nil
}

// String returns a string representation of the event
func (e *MarketEvent) String() string {
	return fmt.Sprintf("MarketEvent{type=%s, book=%s, timestamp=%s}",
		e.EventType, e.Book, e.Timestamp.Format(time.RFC3339))
}

// Compare compares two events by timestamp for sorting
func (e *MarketEvent) Compare(other *MarketEvent) int {
	if e.Timestamp.Before(other.Timestamp) {
		return -1
	} else if e.Timestamp.After(other.Timestamp) {
		return 1
	}
	return 0
}
