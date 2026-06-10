package models

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

func TestFromBitsoRESTTrade(t *testing.T) {
	created := bitso.Time(time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC))
	trade := &bitso.Trade{
		Book:      *bitso.NewBook(bitso.BTC, bitso.MXN),
		CreatedAt: created,
		Amount:    bitso.Monetary("0.001"),
		MakerSide: bitso.OrderSideBuy,
		Price:     bitso.Monetary("1075000"),
		TID:       12345,
	}

	event := FromBitsoRESTTrade(trade)
	if event == nil {
		t.Fatal("expected trade event")
	}
	if event.ID != 12345 {
		t.Fatalf("id=%d", event.ID)
	}
	if event.Book != "btc_mxn" {
		t.Fatalf("book=%s", event.Book)
	}
	if event.Price != 1075000 {
		t.Fatalf("price=%v", event.Price)
	}
	if event.MakerSide != "buy" {
		t.Fatalf("maker_side=%s", event.MakerSide)
	}
}
