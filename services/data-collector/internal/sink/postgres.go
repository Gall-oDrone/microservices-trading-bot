package sink

import (
	"context"
	"fmt"
	"time"

	"bitso-trading-platform/data-collector/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is a hot store for recent trades and gap records.
type PostgresStore struct {
	pool           *pgxpool.Pool
	retentionDays  int
}

// NewPostgresStore connects and ensures schema.
func NewPostgresStore(ctx context.Context, dsn string, retentionDays int) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	s := &PostgresStore{pool: pool, retentionDays: retentionDays}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *PostgresStore) migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS trades (
    book TEXT NOT NULL,
    tid BIGINT NOT NULL,
    price DOUBLE PRECISION NOT NULL,
    amount DOUBLE PRECISION NOT NULL,
    maker_side TEXT NOT NULL,
    exchange_ts TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (book, tid)
);
CREATE INDEX IF NOT EXISTS trades_exchange_ts_idx ON trades (exchange_ts DESC);
CREATE INDEX IF NOT EXISTS trades_received_at_idx ON trades (received_at DESC);

CREATE TABLE IF NOT EXISTS ws_gaps (
    id BIGSERIAL PRIMARY KEY,
    book TEXT NOT NULL,
    gap_start TIMESTAMPTZ NOT NULL,
    gap_end TIMESTAMPTZ NOT NULL,
    duration_ms BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS ws_gaps_book_start_idx ON ws_gaps (book, gap_start DESC);
`)
	return err
}

// WriteTrades upserts trades and prunes rows older than retention.
func (s *PostgresStore) WriteTrades(ctx context.Context, trades []models.Trade) error {
	if len(trades) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, t := range trades {
		batch.Queue(`
INSERT INTO trades (book, tid, price, amount, maker_side, exchange_ts, received_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (book, tid) DO NOTHING
`, t.Book, t.TID, t.Price, t.Amount, t.MakerSide, t.ExchangeTS, t.ReceivedAt)
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range trades {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("postgres insert trade: %w", err)
		}
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -s.retentionDays)
	if _, err := s.pool.Exec(ctx, `DELETE FROM trades WHERE received_at < $1`, cutoff); err != nil {
		return fmt.Errorf("postgres prune: %w", err)
	}
	return nil
}

// WriteGap persists a WebSocket gap record.
func (s *PostgresStore) WriteGap(ctx context.Context, gap models.GapRecord) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO ws_gaps (book, gap_start, gap_end, duration_ms, created_at)
VALUES ($1, $2, $3, $4, $5)
`, gap.Book, gap.Start, gap.End, gap.Duration.Milliseconds(), gap.CreatedAt)
	if err != nil {
		return fmt.Errorf("postgres insert gap: %w", err)
	}
	return nil
}

func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}

// NopTradeWriter discards writes (used when Postgres is disabled).
type NopTradeWriter struct{}

func (NopTradeWriter) WriteTrades(context.Context, []models.Trade) error { return nil }
func (NopTradeWriter) WriteGap(context.Context, models.GapRecord) error   { return nil }
func (NopTradeWriter) Close() error                                      { return nil }
