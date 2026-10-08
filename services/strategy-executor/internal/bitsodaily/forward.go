// Package bitsodaily forwards to bitso-trading-platform/shared/pkg/bitsodaily,
// where the Bitso daily candle fetcher and CSV format moved on 2026-10-08 so
// order-management's VaR volatility estimate reads the same bars as
// cmd/daily-executor. The aliases keep every caller in this module
// unchanged; there is no second implementation.
package bitsodaily

import (
	"net/http"
	"time"

	shared "bitso-trading-platform/shared/pkg/bitsodaily"
)

// Candle is one bucket as returned by /api/v3/ohlc.
type Candle = shared.Candle

// Row is one closed daily candle.
type Row = shared.Row

// Day is one calendar day.
const Day = shared.Day

// DefaultBaseURL is Bitso production.
const DefaultBaseURL = shared.DefaultBaseURL

// Mexico is the time zone Bitso uses to bucket daily candles.
var Mexico = shared.Mexico

// FetchRange walks [start, end) in chunks.
func FetchRange(c *http.Client, base, book string, start, end time.Time, chunk time.Duration) ([]Candle, error) {
	return shared.FetchRange(c, base, book, start, end, chunk)
}

// ToRows converts candles to closed, de-duplicated rows sorted by date.
func ToRows(cs []Candle, now time.Time) ([]Row, int) { return shared.ToRows(cs, now) }

// WriteCSV writes rows as the evidence CSV.
func WriteCSV(path, book string, rows []Row) error { return shared.WriteCSV(path, book, rows) }

// ReadCSV reads an evidence CSV, sorted by date.
func ReadCSV(path string) ([]Row, error) { return shared.ReadCSV(path) }

// Gaps describes missing calendar days between consecutive rows, or "".
func Gaps(rows []Row) string { return shared.Gaps(rows) }
