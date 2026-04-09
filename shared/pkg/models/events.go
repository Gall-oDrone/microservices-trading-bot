package models

import (
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// TradeEvent represents a trade event
type TradeEvent struct {
	ID              uint64    `json:"id"`
	Book            string    `json:"book"`
	Price           float64   `json:"price"`
	Amount          float64   `json:"amount"`
	Value           float64   `json:"value"`
	Side            string    `json:"side"` // buy or sell
	Timestamp       time.Time `json:"timestamp"`
	MakerSide       string    `json:"maker_side,omitempty"`
	ReceivedAt      time.Time `json:"received_at,omitempty"`
	CreatedAtMillis int64     `json:"created_at_millis,omitempty"`
}

// GetLatencyMs returns the latency in milliseconds between trade creation and reception
func (t *TradeEvent) GetLatencyMs() int64 {
	if t.ReceivedAt.IsZero() || t.CreatedAtMillis == 0 {
		return 0
	}
	return t.ReceivedAt.UnixMilli() - t.CreatedAtMillis
}

// FromBitsoWebSocketTrade converts a Bitso WebSocket trade to a TradeEvent
func FromBitsoWebSocketTrade(wsTrade *bitso.WebSocketTrade) *TradeEvent {
	if wsTrade == nil || len(wsTrade.Payload) == 0 {
		return nil
	}

	// Get the first trade from the payload
	payload := wsTrade.Payload[0]

	// Determine side from maker side (0 = buy, 1 = sell)
	side := "buy"
	makerSide := "buy"
	if payload.MakerSide == 1 {
		side = "sell"
		makerSide = "sell"
	}

	return &TradeEvent{
		ID:              payload.TID,
		Book:            wsTrade.Book.String(),
		Price:           payload.Price.Float64(),
		Amount:          payload.Amount.Float64(),
		Value:           payload.Value.Float64(),
		Side:            side,
		MakerSide:       makerSide,
		Timestamp:       time.UnixMilli(int64(payload.CreationTimestamp)),
		ReceivedAt:      time.Now(),
		CreatedAtMillis: int64(payload.CreationTimestamp),
	}
}

type TradeSignalEvent struct {
	EventID   string                 `json:"event_id"`
	Timestamp int64                  `json:"timestamp"`
	Book      string                 `json:"book"`
	Strategy  string                 `json:"strategy"`
	Signal    string                 `json:"signal"` // BUY, SELL, HOLD
	Price     float64                `json:"price"`
	Amount    float64                `json:"amount"`
	Metadata  map[string]interface{} `json:"metadata"`
}

type OrderEvent struct {
	EventID   string  `json:"event_id"`
	OrderID   string  `json:"order_id"`
	Timestamp int64   `json:"timestamp"`
	Book      string  `json:"book"`
	Side      string  `json:"side"`
	Type      string  `json:"type"`
	Status    string  `json:"status"`
	Price     float64 `json:"price"`
	Amount    float64 `json:"amount"`
}

// OrderFillEvent is published when an order reaches fully filled status (e.g. after Bitso sync).
// strategy-executor correlates EventID with TradeSignalEvent.event_id / signal metadata event_id.
type OrderFillEvent struct {
	EventID      string  `json:"event_id"`
	OrderID      string  `json:"order_id"`
	TimestampMs  int64   `json:"timestamp_ms"`
	Book         string  `json:"book"`
	Side         string  `json:"side"`
	AveragePrice float64 `json:"average_price"`
	FilledAmount float64 `json:"filled_amount"`
	Strategy     string  `json:"strategy"`
	Liquidity    string  `json:"liquidity,omitempty"`
}
