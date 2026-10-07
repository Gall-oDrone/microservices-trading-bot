package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/objstore"
)

const (
	ledgerA = `{"book":"btc_mxn","mode":"stage","recorded_at":"2026-10-02T12:00:00Z","decision":{"bar_date":"2026-10-01"}}` + "\n"
	ledgerB = ledgerA + `{"book":"btc_mxn","mode":"stage","recorded_at":"2026-10-03T12:00:00Z","decision":{"bar_date":"2026-10-02"}}` + "\n"
	csvOld  = "date,open,high,low,close,volume\n2026-09-30,1,2,0.5,1.5,10\n"
	csvNew  = "date,open,high,low,close,volume,trade_count\n2026-09-30,1,2,0.5,1.5,10,3\n2026-10-01,1.5,3,1,2.5,20,4\n"
	haltOn  = `{"halted":true,"reason":"exchange incident","by":"diego","at":"2026-10-06T01:00:00Z"}`
)

// countingMem counts List calls.
type countingMem struct {
	*objstore.Mem
	lists atomic.Int32
}

func (c *countingMem) List(ctx context.Context, prefix string) ([]objstore.Object, error) {
	c.lists.Add(1)
	return c.Mem.List(ctx, prefix)
}

func remoteFixture(t *testing.T) (*Store, *countingMem, *time.Time) {
	t.Helper()
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	m := &countingMem{Mem: &objstore.Mem{Name: "s3://bucket"}}
	p := "daily-executor/stage/"
	m.Put(p+"ledger.jsonl", []byte(ledgerA), at)
	m.Put(p+"candles/btc_mxn_daily_2026-09-30.csv", []byte(csvOld), at)
	m.Put(p+"candles/btc_mxn_daily_2026-10-01.csv", []byte(csvNew), at)
	m.Put(p+"run-20261003T120000Z.log", []byte("exit=0\n"), at)
	m.Put(p+"notes.txt", []byte("x"), at)
	m.Put("daily-executor/stage-other/ledger.jsonl", []byte(ledgerB), at) // a sibling prefix
	st := NewRemote(m, "/"+p)
	now := at
	st.fs.(*remoteFS).now = func() time.Time { return now }
	return st, m, &now
}

func TestRemoteStoreReadsTheUploadedCopy(t *testing.T) {
	st, m, now := remoteFixture(t)
	if !st.Remote() || st.LedgerPath != "daily-executor/stage/ledger.jsonl" || st.CandlesDir != "daily-executor/stage/candles" {
		t.Fatalf("paths %q %q", st.LedgerPath, st.CandlesDir)
	}
	if w := st.Where(st.LedgerPath); w != "s3://bucket/daily-executor/stage/ledger.jsonl" {
		t.Fatalf("where %q", w)
	}
	recs, err := st.Records()
	if err != nil || len(recs) != 1 || recs[0].Decision.BarDate != "2026-10-01" {
		t.Fatalf("records %+v %v", recs, err)
	}
	info, found, err := st.LedgerInfo()
	if err != nil || !found || !info.ModTime.Equal(*now) {
		t.Fatalf("ledger info %+v %v %v", info, found, err)
	}
	rows, path, err := st.Candles("btc_mxn")
	if err != nil || len(rows) != 2 || rows[1].TradeCount != 4 || !strings.HasSuffix(path, "btc_mxn_daily_2026-10-01.csv") {
		t.Fatalf("candles %+v %q %v", rows, path, err)
	}
	if _, _, err := st.Candles("btc_usd"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing candles: %v", err)
	}
	runs, err := st.Files("run-", ".log")
	if err != nil || len(runs) != 1 || runs[0].Name != "run-20261003T120000Z.log" {
		t.Fatalf("run logs %+v %v", runs, err)
	}
	if b, err := st.ReadFile(runs[0].Path); err != nil || string(b) != "exit=0\n" {
		t.Fatalf("read run log %q %v", b, err)
	}
	if _, found, err := st.Halt(); found || err != nil {
		t.Fatalf("no halt file: found=%v err=%v", found, err)
	}

	// One listing serves every read until it is ListTTL old.
	if n := m.lists.Load(); n != 1 {
		t.Fatalf("%d listings, want 1", n)
	}

	// The next upload: a new ledger line and a halt. Not seen until the
	// listing expires, then seen without restarting.
	later := now.Add(time.Minute)
	m.Put("daily-executor/stage/ledger.jsonl", []byte(ledgerB), later)
	m.Put("daily-executor/stage/risk-state.json", []byte(haltOn), later)
	if recs, _ := st.Records(); len(recs) != 1 {
		t.Fatalf("cached listing: %d records", len(recs))
	}
	*now = now.Add(ListTTL)
	if recs, _ := st.Records(); len(recs) != 2 {
		t.Fatalf("after ListTTL: %d records, want 2", len(recs))
	}
	h, found, err := st.Halt()
	if err != nil || !found || !h.Halted || h.By != "diego" {
		t.Fatalf("halt %+v %v %v", h, found, err)
	}

	// An invalid halt file is an error (the executor refuses to run on it).
	m.Put("daily-executor/stage/risk-state.json", []byte(`{"halted":true}`), later.Add(time.Second))
	*now = now.Add(ListTTL)
	if _, found, err := st.Halt(); !found || err == nil || !strings.Contains(err.Error(), "s3://bucket/daily-executor/stage/risk-state.json") {
		t.Fatalf("invalid halt: found=%v err=%v", found, err)
	}

	// A removed ledger is an empty one, as on disk.
	m.Delete("daily-executor/stage/ledger.jsonl")
	*now = now.Add(ListTTL)
	if recs, err := st.Records(); err != nil || len(recs) != 0 {
		t.Fatalf("deleted ledger: %d %v", len(recs), err)
	}
}

func TestRemoteStoreOutage(t *testing.T) {
	st, m, now := remoteFixture(t)
	m.Err = errors.New("AccessDenied")
	if _, err := st.Records(); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("outage must surface: %v", err)
	}
	// Retried sooner than ListTTL after a failure.
	m.Err = nil
	*now = now.Add(listErrTTL)
	if recs, err := st.Records(); err != nil || len(recs) != 1 {
		t.Fatalf("after recovery: %d %v", len(recs), err)
	}
}

func TestRemoteStoreLedgerKey(t *testing.T) {
	m := &objstore.Mem{Name: "s3://b"}
	if st := NewRemote(m, "x/y/custom.jsonl"); st.LedgerPath != "x/y/custom.jsonl" || st.CandlesDir != "x/y/candles" {
		t.Fatalf("%q %q", st.LedgerPath, st.CandlesDir)
	}
	if st := NewRemote(m, ""); st.LedgerPath != "ledger.jsonl" || st.CandlesDir != "candles" || st.HaltPath() != "risk-state.json" {
		t.Fatalf("bucket root: %q %q %q", st.LedgerPath, st.CandlesDir, st.HaltPath())
	}
}

// The local store must behave as before the FS split.
func TestLocalStore(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st := New(filepath.Join(dir, "ledger.jsonl"), "")
	if st.Remote() || st.CandlesDir != filepath.Join(dir, "candles") || st.Where(st.LedgerPath) != st.LedgerPath {
		t.Fatalf("local paths %+v", st)
	}
	if recs, err := st.Records(); err != nil || recs != nil {
		t.Fatalf("missing ledger: %v %v", recs, err)
	}
	if _, found, err := st.LedgerInfo(); found || err != nil {
		t.Fatalf("missing ledger info: %v %v", found, err)
	}
	write("ledger.jsonl", ledgerA)
	write("candles/btc_mxn_daily_2026-09-30.csv", csvOld)
	write("candles/btc_mxn_daily_2026-10-01.csv", csvNew)
	write("run-20261003T120000Z.log", "exit=0\n")
	write(risk.HaltFileName, haltOn)
	if recs, err := st.Records(); err != nil || len(recs) != 1 {
		t.Fatalf("records %d %v", len(recs), err)
	}
	write("ledger.jsonl", ledgerB) // size changes: re-read
	if recs, _ := st.Records(); len(recs) != 2 {
		t.Fatalf("re-read: %d", len(recs))
	}
	if rows, path, err := st.Candles("btc_mxn"); err != nil || len(rows) != 2 || filepath.Base(path) != "btc_mxn_daily_2026-10-01.csv" {
		t.Fatalf("candles %d %q %v", len(rows), path, err)
	}
	if runs, err := st.Files("run-", ".log"); err != nil || len(runs) != 1 {
		t.Fatalf("runs %+v %v", runs, err)
	}
	if h, found, err := st.Halt(); err != nil || !found || !h.Halted {
		t.Fatalf("halt %+v %v %v", h, found, err)
	}
}
