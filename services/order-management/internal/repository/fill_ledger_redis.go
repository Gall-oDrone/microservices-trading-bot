package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"bitso-trading-platform/order-management/internal/models"
)

const fillLedgerKeyPrefix = "om:ledger:fills:"

// RedisFillLedger appends JSON lines to a per-day Redis list (RPUSH).
type RedisFillLedger struct {
	client *redis.Client
}

// NewRedisFillLedger creates a Redis-backed fill ledger.
func NewRedisFillLedger(client *redis.Client) *RedisFillLedger {
	return &RedisFillLedger{client: client}
}

func ledgerDayKey(t time.Time) string {
	day := t.UTC().Format("2006-01-02")
	return fillLedgerKeyPrefix + day
}

// Append serializes the entry and RPUSHes to the UTC date list.
func (l *RedisFillLedger) Append(ctx context.Context, e *models.FillLedgerEntry) error {
	if e == nil {
		return nil
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("ledger marshal: %w", err)
	}
	ts := e.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	key := ledgerDayKey(ts)
	return l.client.RPush(ctx, key, data).Err()
}
