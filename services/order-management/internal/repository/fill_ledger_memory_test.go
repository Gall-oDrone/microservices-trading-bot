package repository

import (
	"context"
	"testing"
	"time"

	"bitso-trading-platform/order-management/internal/models"
)

func TestInMemoryFillLedger_Append(t *testing.T) {
	l := NewInMemoryFillLedger()
	e := &models.FillLedgerEntry{
		ID:             "1",
		Timestamp:      time.Now().UTC(),
		OrderID:        "ord-1",
		Book:           "btc_mxn",
		Strategy:       "basic",
		Side:           "sell",
		Amount:         0.01,
		AvgPrice:       1e6,
		RealizedPnLMXN: 42.5,
	}
	if err := l.Append(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 1 || l.Entries[0].RealizedPnLMXN != 42.5 {
		t.Fatalf("ledger: %+v", l.Entries)
	}
}
