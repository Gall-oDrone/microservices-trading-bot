package etoroledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Intent is written (fsync) before a demo order is sent and never changed
// afterwards. A re-run after a crash finds it and resends with the SAME
// ClientRef, so eToro's duplicate-reference rejection resolves the order the
// first attempt placed (broker.OpenRequest.IntentAt bounds that search).
// Intents are kept as an audit trail; cmd/etoro-reconcile reports any whose
// ClientRef never reached the ledger.
type Intent struct {
	ClientRef  string    `json:"client_ref"`
	RequestID  string    `json:"request_id"`
	Kind       string    `json:"kind"` // ActionOpen | ActionClose
	Book       string    `json:"book"`
	Instrument int64     `json:"instrument_id"`
	BarDate    string    `json:"bar_date"`
	FillDate   string    `json:"fill_date"`
	Amount     float64   `json:"amount,omitempty"`      // opens
	PositionID string    `json:"position_id,omitempty"` // closes
	IntentAt   time.Time `json:"intent_at"`
}

// IntentDir is <ledger dir>/intents.
func IntentDir(ledgerPath string) string {
	return filepath.Join(filepath.Dir(ledgerPath), "intents")
}

func intentFile(dir, ref string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, ref)
	return filepath.Join(dir, safe+".json")
}

// LoadIntent returns the intent recorded for ref, if any.
func LoadIntent(dir, ref string) (Intent, bool, error) {
	b, err := os.ReadFile(intentFile(dir, ref))
	if errors.Is(err, os.ErrNotExist) {
		return Intent{}, false, nil
	}
	if err != nil {
		return Intent{}, false, err
	}
	var in Intent
	if err := json.Unmarshal(b, &in); err != nil {
		return Intent{}, false, fmt.Errorf("intent %s: %w", ref, err)
	}
	if in.ClientRef != ref {
		return Intent{}, false, fmt.Errorf("intent file for %s holds %q", ref, in.ClientRef)
	}
	return in, true, nil
}

// RecordIntent writes in unless an intent for its ClientRef exists; either
// way it returns the intent on disk (an existing one wins, unchanged), and
// whether it already existed.
func RecordIntent(dir string, in Intent) (Intent, bool, error) {
	if in.ClientRef == "" || in.IntentAt.IsZero() {
		return Intent{}, false, fmt.Errorf("intent needs client_ref and intent_at")
	}
	if old, ok, err := LoadIntent(dir, in.ClientRef); err != nil || ok {
		return old, ok, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Intent{}, false, err
	}
	b, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return Intent{}, false, err
	}
	path := intentFile(dir, in.ClientRef)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return Intent{}, false, err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return Intent{}, false, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return Intent{}, false, err
	}
	return in, false, f.Close()
}

// Intents lists every intent in dir, sorted by IntentAt.
func Intents(dir string) ([]Intent, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Intent
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var in Intent
		if err := json.Unmarshal(b, &in); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IntentAt.Before(out[j].IntentAt) })
	return out, nil
}

// ClientRef is the executor's idempotency key for a book's leg on a fill
// date, e.g. "sma50-nsdq100-2026-10-13-open".
func ClientRef(book, fillDate, kind string) string {
	return "sma50-" + book + "-" + fillDate + "-" + kind
}
