// Package store reads the daily-executor's files (ledger, candle CSVs, halt
// file, run logs) from disk or from the S3 copy that
// scripts/daily-executor-run.sh uploads, caching each by size and mtime (or
// ETag). It never writes.
package store

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"sync"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/objstore"
)

// Store gives cached, read-only access to one ledger and its candles dir.
// LedgerPath and CandlesDir are paths on disk, or keys in the bucket for a
// store made by NewRemote; use Where for display.
type Store struct {
	LedgerPath string
	CandlesDir string

	fs      FS
	mu      sync.Mutex
	ledTag  string
	ledRecs []dailyledger.Record
	candles map[string]candleCache
}

type candleCache struct {
	path string
	tag  string
	rows []Candle
}

// New returns a store on local disk. candlesDir defaults to
// <ledger dir>/candles, as in the executor.
func New(ledgerPath, candlesDir string) *Store {
	return newStore(osFS{}, ledgerPath, candlesDir)
}

// NewRemote returns a store on the S3 copy under prefix (as uploaded by
// scripts/daily-executor-run.sh: <prefix>/ledger.jsonl, risk-state.json,
// candles/*.csv, run-*.log). A prefix ending in .jsonl names the ledger key
// itself.
func NewRemote(obj objstore.Store, prefix string) *Store {
	prefix = strings.Trim(prefix, "/")
	ledger := prefix + "/ledger.jsonl"
	if prefix == "" {
		ledger = "ledger.jsonl"
	}
	if strings.HasSuffix(prefix, ".jsonl") {
		ledger = prefix
	}
	rfs := newRemoteFS(obj, rfsRoot(ledger))
	return newStore(rfs, ledger, "")
}

func rfsRoot(ledgerKey string) string {
	if i := strings.LastIndex(ledgerKey, "/"); i >= 0 {
		return ledgerKey[:i]
	}
	return ""
}

func newStore(f FS, ledgerPath, candlesDir string) *Store {
	if candlesDir == "" {
		candlesDir = f.Join(f.Dir(ledgerPath), "candles")
	}
	return &Store{LedgerPath: ledgerPath, CandlesDir: candlesDir, fs: f, candles: map[string]candleCache{}}
}

func (s *Store) files() FS {
	if s.fs == nil { // a zero Store reads the local disk
		return osFS{}
	}
	return s.fs
}

// Where is p (a path or key of this store) for display: the path on disk or
// s3://bucket/key.
func (s *Store) Where(p string) string { return s.files().URI(p) }

// Remote reports whether the store reads S3.
func (s *Store) Remote() bool {
	_, ok := s.files().(*remoteFS)
	return ok
}

// LedgerInfo stats the ledger. found is false (and err nil) when it does not
// exist yet.
func (s *Store) LedgerInfo() (info Info, found bool, err error) {
	info, err = s.files().Stat(s.LedgerPath)
	if errors.Is(err, fs.ErrNotExist) {
		return Info{}, false, nil
	}
	if err != nil {
		return Info{}, false, err
	}
	return info, true, nil
}

// Records returns every ledger record, sorted by (book, bar_date). The file is
// re-read only when it changes (size and mtime, or ETag). A missing ledger is
// empty.
func (s *Store) Records() ([]dailyledger.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fi, found, err := s.LedgerInfo()
	if err != nil {
		return nil, err
	}
	if !found {
		s.ledRecs, s.ledTag = nil, ""
		return nil, nil
	}
	if s.ledRecs != nil && fi.Tag == s.ledTag {
		return s.ledRecs, nil
	}
	b, err := s.files().ReadFile(s.LedgerPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	recs, err := dailyledger.Read(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.Where(s.LedgerPath), err)
	}
	s.ledRecs, s.ledTag = recs, fi.Tag
	return recs, nil
}

// HaltPath is the operator halt file next to the ledger (risk.HaltFileName).
func (s *Store) HaltPath() string {
	f := s.files()
	return f.Join(f.Dir(s.LedgerPath), risk.HaltFileName)
}

// Halt reads the halt file with the executor's rules (risk.ParseHaltState).
// A missing file is found=false and no error; an invalid one is an error,
// on which the executor refuses to run.
func (s *Store) Halt() (h risk.HaltState, found bool, err error) {
	p := s.HaltPath()
	b, err := s.files().ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return risk.HaltState{}, false, nil
	}
	if err != nil {
		return risk.HaltState{}, true, fmt.Errorf("halt file %s: %w", s.Where(p), err)
	}
	if h, err = risk.ParseHaltState(b); err != nil {
		return risk.HaltState{}, true, fmt.Errorf("halt file %s: %w", s.Where(p), err)
	}
	return h, true, nil
}

// Files lists the files next to the ledger whose name has prefix and suffix
// (e.g. the run-*.log files), sorted by name.
func (s *Store) Files(prefix, suffix string) ([]Info, error) {
	return s.files().Files(s.files().Dir(s.LedgerPath), prefix, suffix)
}

// ReadFile reads a path returned by Files.
func (s *Store) ReadFile(p string) ([]byte, error) { return s.files().ReadFile(p) }

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
	fi, err := s.latestCandles(book)
	return fi.Path, err
}

func (s *Store) latestCandles(book string) (Info, error) {
	matches, err := s.files().Files(s.CandlesDir, book+"_daily_", ".csv")
	if err != nil {
		return Info{}, err
	}
	if len(matches) == 0 {
		return Info{}, fs.ErrNotExist
	}
	return matches[len(matches)-1], nil // ISO dates sort lexically
}

// Candles returns the full history from the latest CSV for book.
func (s *Store) Candles(book string) ([]Candle, string, error) {
	fi, err := s.latestCandles(book)
	if err != nil {
		return nil, "", err
	}
	path := fi.Path
	s.mu.Lock()
	c, ok := s.candles[book]
	s.mu.Unlock()
	if ok && c.path == path && c.tag == fi.Tag {
		return c.rows, path, nil
	}
	b, err := s.files().ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	rows, err := ParseCandles(bytes.NewReader(b))
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", s.Where(path), err)
	}
	s.mu.Lock()
	s.candles[book] = candleCache{path: path, tag: fi.Tag, rows: rows}
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
