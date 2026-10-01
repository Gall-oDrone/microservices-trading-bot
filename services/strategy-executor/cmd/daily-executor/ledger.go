package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// record is one line of the append-only ledger: one book, one decision day.
type record struct {
	RecordedAt  string      `json:"recorded_at"`
	CodeVersion string      `json:"code_version"`
	Mode        string      `json:"mode"` // "dry-run" (phase 1); "stage" once orders are placed
	Book        string      `json:"book"`
	Prereg      string      `json:"prereg"`
	Decision    decision    `json:"decision"`
	Paper       paperResult `json:"paper"`
	Candles     candleInfo  `json:"candles"`
}

type candleInfo struct {
	Source      string `json:"source"`
	First       string `json:"first"`
	Last        string `json:"last"`
	Bars        int    `json:"bars"`
	RecentGaps  string `json:"recent_gaps,omitempty"` // missing days in the last 60 bars
	SHA256Short string `json:"sha256_prefix"`         // of the fetched CSV, for the data-integrity check
}

// ledger is keyed by (book, bar_date). Re-running on the same day must never
// write a second line for the same decision.
type ledger struct {
	path    string
	entries map[string]record
}

func key(book, barDate string) string { return book + "|" + barDate }

func openLedger(path string) (*ledger, error) {
	l := &ledger{path: path, entries: map[string]record{}}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var r record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		l.entries[key(r.Book, r.Decision.BarDate)] = r
	}
	return l, sc.Err()
}

func (l *ledger) get(book, barDate string) (record, bool) {
	r, ok := l.entries[key(book, barDate)]
	return r, ok
}

func (l *ledger) append(r record) error {
	if _, dup := l.get(r.Book, r.Decision.BarDate); dup {
		return fmt.Errorf("ledger already has %s %s", r.Book, r.Decision.BarDate)
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	l.entries[key(r.Book, r.Decision.BarDate)] = r
	return f.Close()
}
