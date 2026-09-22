package loader

import (
	"context"
	"fmt"
	"time"
)

// PostgresRetentionDays documents the hot store's rolling window.
//
// services/data-collector/internal/sink/postgres.go prunes on every write with
//
//	DELETE FROM trades WHERE received_at < now() - retentionDays
//
// so anything older than this is simply gone. This constant exists so the
// limitation is enforced in code rather than remembered in a doc.
const PostgresRetentionDays = 7

// RowScanner is the minimum pgx surface the Postgres source needs. Keeping it
// this narrow is what allows the unit tests to run with no database.
type RowScanner interface {
	// QueryTrades returns archived rows for a book in [from, to].
	QueryTrades(ctx context.Context, book string, from, to time.Time) ([]ArchiveTrade, error)
	// Close releases the underlying pool.
	Close() error
}

// PostgresSource is a LIMITED-RANGE trade source for quick recent-data checks.
//
// It must not be used as the primary source for any multi-week analysis. The
// hot store keeps only the last PostgresRetentionDays days; a backtest run
// against it will silently cover a far shorter window than requested and will
// look like a valid result. LoadTrades therefore refuses out-of-retention
// windows outright rather than returning a quietly truncated slice.
type PostgresSource struct {
	scanner       RowScanner
	retentionDays int
	// now is injectable so the retention guard is testable.
	now func() time.Time
}

// NewPostgresSource wraps a RowScanner as a limited-range TradeSource.
// Pass retentionDays <= 0 to use PostgresRetentionDays.
func NewPostgresSource(scanner RowScanner, retentionDays int) *PostgresSource {
	if retentionDays <= 0 {
		retentionDays = PostgresRetentionDays
	}
	return &PostgresSource{
		scanner:       scanner,
		retentionDays: retentionDays,
		now:           func() time.Time { return time.Now().UTC() },
	}
}

// Describe implements TradeSource.
func (p *PostgresSource) Describe() string {
	return fmt.Sprintf("postgres hot store (LIMITED: last %d days only)", p.retentionDays)
}

// RetentionCutoff returns the oldest timestamp the hot store can be trusted for.
func (p *PostgresSource) RetentionCutoff() time.Time {
	return p.now().UTC().AddDate(0, 0, -p.retentionDays)
}

// ErrOutsideRetention is returned when the requested window predates the hot
// store's retention cutoff. Callers should fall back to the S3 archive.
type ErrOutsideRetention struct {
	RequestedFrom time.Time
	Cutoff        time.Time
	RetentionDays int
}

func (e *ErrOutsideRetention) Error() string {
	return fmt.Sprintf(
		"requested from=%s predates the Postgres hot-store retention cutoff %s (%d-day window); "+
			"use the S3 archive source instead — Postgres cannot answer this range",
		e.RequestedFrom.UTC().Format(time.RFC3339),
		e.Cutoff.UTC().Format(time.RFC3339),
		e.RetentionDays,
	)
}

// LoadTrades implements TradeSource, refusing windows the hot store cannot
// honestly serve.
func (p *PostgresSource) LoadTrades(ctx context.Context, book string, from, to time.Time) ([]ArchiveTrade, Stats, error) {
	st := Stats{Source: p.Describe(), Book: book, From: from, To: to}

	cutoff := p.RetentionCutoff()
	if from.Before(cutoff) {
		return nil, st, &ErrOutsideRetention{
			RequestedFrom: from,
			Cutoff:        cutoff,
			RetentionDays: p.retentionDays,
		}
	}

	rows, err := p.scanner.QueryTrades(ctx, book, from, to)
	if err != nil {
		return nil, st, fmt.Errorf("query trades: %w", err)
	}
	st.RowsDecoded = len(rows)

	return Normalize(rows, book, from, to, &st), st, nil
}

// Close releases the underlying scanner.
func (p *PostgresSource) Close() error { return p.scanner.Close() }
