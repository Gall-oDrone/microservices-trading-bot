package models

import (
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// Trade is a normalized trade record for persistence.
type Trade struct {
	Book          string    `json:"book" parquet:"name=book, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY"`
	TID           int64     `json:"tid" parquet:"name=tid, type=INT64"`
	Price         float64   `json:"price" parquet:"name=price, type=DOUBLE"`
	Amount        float64   `json:"amount" parquet:"name=amount, type=DOUBLE"`
	MakerSide     string    `json:"maker_side" parquet:"name=maker_side, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY"`
	ExchangeTS    time.Time `json:"exchange_ts" parquet:"name=exchange_ts, type=INT64, convertedtype=TIMESTAMP_MILLIS"`
	ReceivedAt    time.Time `json:"received_at" parquet:"name=received_at, type=INT64, convertedtype=TIMESTAMP_MILLIS"`
}

// GapRecord captures a WebSocket outage window for later query.
type GapRecord struct {
	Book      string    `json:"book"`
	Start     time.Time `json:"gap_start"`
	End       time.Time `json:"gap_end"`
	Duration  time.Duration `json:"duration"`
	CreatedAt time.Time `json:"created_at"`
}

// FromBitsoWebSocketTrade expands a Bitso WS trade message into one Trade per payload item.
func FromBitsoWebSocketTrade(msg *bitso.WebSocketTrade, receivedAt time.Time) []Trade {
	if msg == nil {
		return nil
	}
	book := msg.Book.String()
	out := make([]Trade, 0, len(msg.Payload))
	for _, p := range msg.Payload {
		out = append(out, Trade{
			Book:       book,
			TID:        int64(p.TID),
			Price:      p.Price.Float64(),
			Amount:     p.Amount.Float64(),
			MakerSide:  normalizeMakerSide(p.MakerSide),
			ExchangeTS: time.UnixMilli(int64(p.CreationTimestamp)).UTC(),
			ReceivedAt: receivedAt.UTC(),
		})
	}
	return out
}

func normalizeMakerSide(raw string) string {
	switch strings.TrimSpace(raw) {
	case "0", "buy":
		return "buy"
	case "1", "sell":
		return "sell"
	default:
		return raw
	}
}
