package models

import (
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// TradeEvent represents a normalized trade event for publishing
type TradeEvent struct {
	// Trade identification
	ID   uint64 `json:"id"`
	Book string `json:"book"`

	// Trade details
	Price  float64 `json:"price"`
	Amount float64 `json:"amount"`
	Value  float64 `json:"value"`

	// Order information
	MakerOrderID string `json:"maker_order_id"`
	TakerOrderID string `json:"taker_order_id"`
	MakerSide    string `json:"maker_side"` // "buy" or "sell"

	// Timestamps
	Timestamp  time.Time `json:"timestamp"`
	CreatedAt  uint64    `json:"created_at"`  // Original creation timestamp (ms)
	ReceivedAt time.Time `json:"received_at"` // When we received it

	// Metadata
	Source   string                 `json:"source"` // "bitso_websocket"
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// FromBitsoWebSocketTrade converts Bitso WebSocket trade to TradeEvent
func FromBitsoWebSocketTrade(wsTrade *bitso.WebSocketTrade) *TradeEvent {
	if wsTrade == nil || len(wsTrade.Payload) == 0 {
		return nil
	}

	// Take the first payload (typically there's only one)
	payload := wsTrade.Payload[0]

	// Determine taker side (opposite of maker side)
	// takerSide := "sell"
	// if payload.MakerSide == "0" { // Maker was buyer
	// 	takerSide = "buy"
	// }

	return &TradeEvent{
		ID:           payload.TID,
		Book:         wsTrade.Book.String(),
		Price:        payload.Price.Float64(),
		Amount:       payload.Amount.Float64(),
		Value:        payload.Value.Float64(),
		MakerOrderID: payload.MakerOrderID,
		TakerOrderID: payload.TakerOrderID,
		MakerSide:    getMakerSideString(payload.MakerSide),
		Timestamp:    time.Now(),
		CreatedAt:    payload.CreationTimestamp,
		ReceivedAt:   time.Now(),
		Source:       "bitso_websocket",
		Metadata: map[string]interface{}{
			"sent_timestamp": wsTrade.Sent,
		},
	}
}

// getMakerSideString converts maker side code to string
func getMakerSideString(side string) string {
	switch side {
	case "0":
		return "buy"
	case "1":
		return "sell"
	default:
		return "unknown"
	}
}

// GetTakerSide returns the taker side (opposite of maker)
func (t *TradeEvent) GetTakerSide() string {
	if t.MakerSide == "buy" {
		return "sell"
	}
	return "buy"
}

// GetLatencyMs calculates latency from creation to reception
func (t *TradeEvent) GetLatencyMs() int64 {
	if t.CreatedAt == 0 {
		return 0
	}

	createdAtTime := time.Unix(0, int64(t.CreatedAt)*int64(time.Millisecond))
	return t.ReceivedAt.Sub(createdAtTime).Milliseconds()
}
