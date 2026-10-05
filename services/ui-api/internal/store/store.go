// Package store reads the daily-executor's files (ledger, candle CSVs) from
// disk, caching each by modification time. It never writes.
package store

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/dailyledger"
)

// Store gives cached, read-only access to one ledger and its candles dir.
type Store struct {
	LedgerPath string
	CandlesDir string

	mu      sync.Mutex
	ledMod  time.Time
	ledSize int64
	ledRecs []dailyledger.Record
	candles map[string]candleCache
}

type candleCache struct {
	path string
	mod  time.Time
	rows []Candle
}

// New returns a store. candlesDir defaults to <ledger dir>/candles, as in the
// executor.
func New(ledgerPath, candlesDir string) *Store {
	if candlesDir == "" {
		candlesDir = filepath.Join(filepath.Dir(ledgerPath), "candles")
	}
	return &Store{LedgerPath: ledgerPath, CandlesDir: candlesDir, candles: map[string]candleCache{}}
}

// Records returns every ledger record, sorted by (book, bar_date). The file is
// re-read only when its size or mtime changes.
func (s *Store) Records() ([]dailyledger.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fi, err := os.Stat(s.LedgerPath)
	if os.IsNotExist(err) {
		s.ledRecs, s.ledMod, s.ledSize = nil, time.Time{}, 0
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if s.ledRecs != nil && fi.ModTime().Equal(s.ledMod) && fi.Size() == s.ledSize {
		return s.ledRecs, nil
	}
	recs, err := dailyledger.ReadFile(s.LedgerPath)
	if err != nil {
		return nil, err
	}
	s.ledRecs, s.ledMod, s.ledSize = recs, fi.ModTime(), fi.Size()
	return recs, nil
}

// Candle is one daily bar from the executor's CSV.
type Candle struct {
	Date       string  `json:"date"`
	Open       float64 `json:"open"`
	High       float64 `json:"high"`
	Low        float64 `json:"low"`
	Close      float64 `json:"close"`
	Volume     float64 `json:"volume"`
	TradeCount int     `json:"trade_count"`
}

// LatestCandlesFile is the newest <book>_daily_<date>.csv in the candles dir.
func (s *Store) LatestCandlesFile(book string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(s.CandlesDir, book+"_daily_*.csv"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", os.ErrNotExist
	}
	sort.Strings(matches) // ISO dates sort lexically
	return matches[len(matches)-1], nil
}

// Candles returns the full history from the latest CSV for book.
func (s *Store) Candles(book string) ([]Candle, string, error) {
	path, err := s.LatestCandlesFile(book)
	if err != nil {
		return nil, "", err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	s.mu.Lock()
	c, ok := s.candles[book]
	s.mu.Unlock()
	if ok && c.path == path && c.mod.Equal(fi.ModTime()) {
		return c.rows, path, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	rows, err := ParseCandles(f)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	s.mu.Lock()
	s.candles[book] = candleCache{path: path, mod: fi.ModTime(), rows: rows}
	s.mu.Unlock()
	return rows, path, nil
}

// ParseCandles reads the bitsodaily CSV format (header row with at least
// date, open, high, low, close, volume; trade_count optional).
func ParseCandles(r io.Reader) ([]Candle, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	head, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.TrimSpace(strings.ToLower(h))] = i
	}
	for _, need := range []string{"date", "open", "high", "low", "close", "volume"} {
		if _, ok := col[need]; !ok {
			return nil, fmt.Errorf("missing column %q", need)
		}
	}
	var out []Candle
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		num := func(name string) (float64, error) {
			i, ok := col[name]
			if !ok || i >= len(rec) {
				return 0, nil
			}
			return strconv.ParseFloat(strings.TrimSpace(rec[i]), 64)
		}
		c := Candle{Date: rec[col["date"]]}
		for _, f := range []struct {
			name string
			dst  *float64
		}{{"open", &c.Open}, {"high", &c.High}, {"low", &c.Low}, {"close", &c.Close}, {"volume", &c.Volume}} {
			v, err := num(f.name)
			if err != nil {
				return nil, fmt.Errorf("line %d %s: %w", line, f.name, err)
			}
			*f.dst = v
		}
		if tc, err := num("trade_count"); err == nil {
			c.TradeCount = int(tc)
		}
		out = append(out, c)
	}
	return out, nil
}
