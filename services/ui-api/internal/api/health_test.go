package api

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/ui-api/internal/datahealth"
	"bitso-trading-platform/ui-api/internal/objstore"
	"bitso-trading-platform/ui-api/internal/store"
)

// healthLedger copies the test ledger into a temp dir with one run log.
func healthLedger(t *testing.T, runLog string) string {
	t.Helper()
	dir := t.TempDir()
	b, err := os.ReadFile("testdata/ledger.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "ledger.jsonl"), string(b))
	if runLog != "" {
		writeFile(t, filepath.Join(dir, "run-20261002T010000Z.log"), runLog)
		old := time.Date(2026, 10, 2, 1, 5, 0, 0, time.UTC)
		_ = os.Chtimes(filepath.Join(dir, "run-20261002T010000Z.log"), old, old)
	}
	return filepath.Join(dir, "ledger.jsonl")
}

func check(t *testing.T, resp DataHealthResponse, id string) HealthCheck {
	t.Helper()
	for _, c := range resp.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check %s in %+v", id, resp.Checks)
	return HealthCheck{}
}

func TestDataHealthArchiveOff(t *testing.T) {
	ledger := healthLedger(t, "daily-executor abc | as of x | mode stage\n[btc_mxn] 01:00:00Z ledger: recorded\nexit=0\n")
	ts := newTestServer(t, fixedNow, func(s *Server) { s.Store = store.New(ledger, "") })
	resp := get[DataHealthResponse](t, ts, "/api/ui/health/data", 200)
	if resp.Ledger != "stage" || resp.Archive.Status != datahealth.Off || resp.Collector.Status != datahealth.Off {
		t.Fatalf("archive off: %+v", resp)
	}
	if c := check(t, resp, "archive"); c.Status != datahealth.Off {
		t.Fatalf("archive check %+v", c)
	}
	e := resp.Executor
	if !e.LedgerFound || e.Records == 0 || e.LastRecordedAt != "2026-10-02T20:06:30Z" || len(e.Books) != 2 {
		t.Fatalf("executor %+v", e)
	}
	if e.LastRun == nil || e.LastRun.Status != datahealth.OK || e.Upload.Status != datahealth.Off {
		t.Fatalf("last run %+v upload %+v", e.LastRun, e.Upload)
	}
	// Off checks do not count: everything else is fine at fixedNow.
	if resp.Status != datahealth.OK || e.Status != datahealth.OK {
		t.Fatalf("status %s / %s: %+v", resp.Status, e.Status, resp.Checks)
	}
}

func TestDataHealthWithArchiveAndMissedDays(t *testing.T) {
	late := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC) // 2026-10-02..04 missing
	ledger := healthLedger(t, "daily-executor abc | mode stage\nupload=failed s3://b/daily-executor/stage\nexit=1\n")
	m := &objstore.Mem{Name: "s3://archive"}
	for i := 0; i < 25; i++ {
		at := late.Add(-time.Duration(i) * time.Hour)
		day := at.Format("year=2006/month=01/day=02")
		for _, b := range []string{"btc_mxn", "btc_usd"} {
			m.Put("trades/book="+b+"/"+day+"/trades-"+at.Format("150405")+".parquet", []byte("x"), at)
		}
	}
	m.Put("trades_compacted/book=btc_mxn/year=2026/month=10/day=04/_manifest.json", []byte(`{"created_at":"2026-10-05T01:00:00Z","source_rows":5,"compacted_rows":5}`), late)
	ts := newTestServer(t, late, func(s *Server) { s.Store = store.New(ledger, ""); s.Archive = m })
	resp := get[DataHealthResponse](t, ts, "/api/ui/health/data", 200)
	if resp.Archive.Source != "s3://archive" || resp.Collector.Status != datahealth.OK {
		t.Fatalf("collector %+v archive %+v", resp.Collector, resp.Archive)
	}
	if c := check(t, resp, "compaction.btc_mxn"); c.Status != datahealth.OK {
		t.Fatalf("btc_mxn compaction %+v", c)
	}
	if c := check(t, resp, "compaction.btc_usd"); c.Status != datahealth.Fail {
		t.Fatalf("btc_usd compaction %+v", c)
	}
	if c := check(t, resp, "executor.ledger.btc_usd"); c.Status != datahealth.Fail || !strings.Contains(c.Message, "3 closed day(s) not recorded: 2026-10-02") {
		t.Fatalf("coverage %+v", c)
	}
	if c := check(t, resp, "executor.last_run"); c.Status != datahealth.Fail {
		t.Fatalf("last run %+v", c)
	}
	if c := check(t, resp, "executor.upload"); c.Status != datahealth.Fail {
		t.Fatalf("upload %+v", c)
	}
	if resp.Status != datahealth.Fail || resp.Executor.Status != datahealth.Fail {
		t.Fatalf("status %s", resp.Status)
	}

	// Cached for a minute: an outage now does not show until the cache expires.
	m.Err = errors.New("AccessDenied")
	again := get[DataHealthResponse](t, ts, "/api/ui/health/data", 200)
	if again.Archive.Error != "" || again.Archive.CheckedAt != resp.Archive.CheckedAt {
		t.Fatalf("not cached: %+v", again.Archive)
	}
}

func TestDataHealthArchiveUnreachable(t *testing.T) {
	m := &objstore.Mem{Err: errors.New("no credentials")}
	ts := newTestServer(t, fixedNow, func(s *Server) { s.Archive = m })
	resp := get[DataHealthResponse](t, ts, "/api/ui/health/data", 200)
	if c := check(t, resp, "archive"); c.Status != datahealth.Unknown || !strings.Contains(c.Message, "no credentials") {
		t.Fatalf("archive check %+v", c)
	}
	if resp.Collector.Status != datahealth.Unknown {
		t.Fatalf("collector %+v", resp.Collector)
	}
	// No run logs next to testdata/ledger.jsonl.
	if c := check(t, resp, "executor.last_run"); c.Status != datahealth.Unknown {
		t.Fatalf("last run %+v", c)
	}
	get[errorBody](t, ts, "/api/ui/health/data?ledger=nope", 400)
}
