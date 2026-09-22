package loader

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGXScanner is the live RowScanner backed by the collector's Postgres hot
// store. Schema reference: services/data-collector/internal/sink/postgres.go
// (migrate) on the feat/intraday-data-collector branch.
type PGXScanner struct {
	pool *pgxpool.Pool
}

// NewPGXScanner connects to the hot store.
//
// The RDS instance is not publicly accessible, so this expects a DSN that
// resolves from inside the VPC or through a port-forward/tunnel.
func NewPGXScanner(ctx context.Context, dsn string) (*PGXScanner, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	return &PGXScanner{pool: pool}, nil
}

// QueryTrades implements RowScanner.
func (s *PGXScanner) QueryTrades(ctx context.Context, book string, from, to time.Time) ([]ArchiveTrade, error) {
	rows, err := s.pool.Query(ctx, `
SELECT book, tid, price, amount, maker_side, exchange_ts, received_at
FROM trades
WHERE book = $1 AND exchange_ts >= $2 AND exchange_ts <= $3
ORDER BY exchange_ts ASC, tid ASC
`, book, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ArchiveTrade
	for rows.Next() {
		var t ArchiveTrade
		if err := rows.Scan(&t.Book, &t.TID, &t.Price, &t.Amount, &t.MakerSide, &t.ExchangeTS, &t.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Close implements RowScanner.
func (s *PGXScanner) Close() error {
	s.pool.Close()
	return nil
}

// WSGap is one row of the collector's ws_gaps table — a WebSocket outage
// window recorded when the collector reconnected.
type WSGap struct {
	ID        int64
	Book      string
	Start     time.Time
	End       time.Time
	Duration  time.Duration
	CreatedAt time.Time
}

// QueryWSGaps returns every recorded reconnect gap, longest first.
//
// This backs the gap-duration audit: a gap that overlaps a high-activity window
// means the archive is missing data exactly where it matters most, which would
// undermine any regime-coverage claim drawn from that period.
func (s *PGXScanner) QueryWSGaps(ctx context.Context, book string) ([]WSGap, error) {
	q := `
SELECT id, book, gap_start, gap_end, duration_ms, created_at
FROM ws_gaps
`
	args := []any{}
	if book != "" {
		q += " WHERE book = $1"
		args = append(args, book)
	}
	q += " ORDER BY duration_ms DESC"

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WSGap
	for rows.Next() {
		var g WSGap
		var ms int64
		if err := rows.Scan(&g.ID, &g.Book, &g.Start, &g.End, &ms, &g.CreatedAt); err != nil {
			return nil, err
		}
		g.Duration = time.Duration(ms) * time.Millisecond
		out = append(out, g)
	}
	return out, rows.Err()
}
