package repository

import (
	"context"

	"bitso-trading-platform/order-management/internal/models"
)

// FillLedger is an append-only store of completed fills for audit (Phase 4).
// Implementations must be safe for concurrent use.
type FillLedger interface {
	Append(ctx context.Context, e *models.FillLedgerEntry) error
}
