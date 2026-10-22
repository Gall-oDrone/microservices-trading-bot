package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/etorodaily"
)

// candleBooks maps the instruments the canary may hold to the executor's
// book names and symbols, so the files it writes are the ones the console
// (ui-api etoro.go: the newest <book>_daily_<date>.csv) and the stop
// profiles (ATR from the same file) already read.
var candleBooks = map[int64]struct{ Book, Symbol string }{
	28: {"nsdq100", "NSDQ100"},
	27: {"spx500", "SPX500"},
}

// candlePath is the file the newest closed bar lands in, named like the
// executor's (<book>_daily_<last date>.csv), so the console picks the
// newest by date.
func candlePath(dir, book string, rows []bitsodaily.Row) (string, error) {
	if len(rows) == 0 {
		return "", errors.New("no closed bars returned")
	}
	return filepath.Join(dir, fmt.Sprintf("%s_daily_%s.csv", book, rows[len(rows)-1].Date)), nil
}

// writeCandles reads the instrument's daily candles (one GET, the same
// route the executor reads: etoro.MaxCandles bars, NYSE trading days only)
// and writes them in the executor's CSV format. With dry it reads and
// reports but writes nothing. Returns a one-line summary.
func writeCandles(ctx context.Context, c *etoro.Client, dir string, instrument int64, now time.Time, dry bool) (string, error) {
	b, ok := candleBooks[instrument]
	if !ok {
		return "", fmt.Errorf("instrument %d has no book name here (known: 28 nsdq100, 27 spx500)", instrument)
	}
	rows, rep, err := etorodaily.Fetch(ctx, c, instrument, etoro.MaxCandles, now, etorodaily.Options{})
	if err != nil {
		return "", err
	}
	path, err := candlePath(dir, b.Book, rows)
	if err != nil {
		return "", err
	}
	if dry {
		return fmt.Sprintf("-dry: would write %d bars (%s..%s) to %s", len(rows), rep.First, rep.Last, path), nil
	}
	if err := bitsodaily.WriteCSV(path, b.Symbol, rows); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d daily bars (%s..%s, %d missing trading day(s)) to %s", len(rows), rep.First, rep.Last, rep.MissingTrading, path), nil
}
