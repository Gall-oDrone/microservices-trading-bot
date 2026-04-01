package repository

import (
	"context"
	"sync"

	"bitso-trading-platform/order-management/internal/models"
)

// InMemoryFillLedger keeps ledger entries in memory (tests and local dev).
type InMemoryFillLedger struct {
	mu      sync.Mutex
	Entries []*models.FillLedgerEntry
}

// NewInMemoryFillLedger creates an empty in-memory ledger.
func NewInMemoryFillLedger() *InMemoryFillLedger {
	return &InMemoryFillLedger{Entries: make([]*models.FillLedgerEntry, 0)}
}

// Append records a fill event.
func (l *InMemoryFillLedger) Append(_ context.Context, e *models.FillLedgerEntry) error {
	if e == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Entries = append(l.Entries, e)
	return nil
}
