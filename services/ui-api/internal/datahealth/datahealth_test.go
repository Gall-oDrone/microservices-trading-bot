package datahealth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/ui-api/internal/objstore"
)

var now = time.Date(2026, 10, 7, 21, 0, 0, 0, time.UTC)

// putFlushes writes one raw object per hour for book, from start for n hours.
func putFlushes(m *objstore.Mem, book string, start time.Time, n int) {
	for i := 0; i < n; i++ {
		t := start.Add(time.Duration(i) * time.Hour)
		key := fmt.Sprintf("%s%s", dayPrefix("trades", book, t.Truncate(24*time.Hour)),
			"trades-"+t.Format("20060102T150405")+".parquet")
		m.Put(key, []byte("x"), t.Add(20*time.Second))
	}
}

func putCompacted(m *objstore.Mem, book, day, created string) {
	d, _ := time.Parse("2006-01-02", day)
	p := dayPrefix("trades_compacted", book, d)
	m.Put(p+"trades-"+d.Format("20060102")+".parquet", []byte("pq"), d)
	m.Put(p+"_manifest.json", []byte(`{"version":1,"source_rows":40,"compacted_rows":39,"duplicate_tids":1,"other_day_rows":0,"created_at":"`+created+`"}`), d)
}

func TestArchiveHealthy(t *testing.T) {
	m := &objstore.Mem{Name: "s3://test"}
	putFlushes(m, "btc_mxn", now.Add(-30*time.Hour), 30) // hourly up to 30 min before now
	putCompacted(m, "btc_mxn", "2026-10-05", "2026-10-06T01:00:00Z")
	putCompacted(m, "btc_mxn", "2026-10-06", "2026-10-07T01:00:00Z")
	a := CheckArchive(context.Background(), m, []string{"btc_mxn"}, now)
	if a.Status != OK || a.Source != "s3://test" || len(a.Books) != 1 {
		t.Fatalf("archive %+v", a)
	}
	r, c := a.Books[0].Raw, a.Books[0].Compacted
	if r.Status != OK || r.LatestPartition != "2026-10-07" || r.AgeMinutes != 59.7 || r.Flushes24h != 24 || r.FlushGaps24h != 0 {
		t.Fatalf("raw %+v", r)
	}
	if r.ObjectsToday != 21 || r.MaxFlushGapMinutes != 60 || len(r.Flushes) != 24 || r.Flushes[23] != "2026-10-07T20:00:20Z" {
		t.Fatalf("raw counts %+v", r)
	}
	if c.Status != OK || c.LatestPartition != "2026-10-06" || c.DaysBehind != 0 || c.Partitions != 2 ||
		c.FirstPartition != "2026-10-05" || c.CompactedRows != 39 || c.DuplicateTids != 1 {
		t.Fatalf("compaction %+v", c)
	}
}

func TestArchiveStaleCollectorAndCompaction(t *testing.T) {
	m := &objstore.Mem{}
	// Flushes stopped 4 h ago; compaction stopped on 2026-09-30.
	putFlushes(m, "btc_usd", now.Add(-20*time.Hour), 17)
	putCompacted(m, "btc_usd", "2026-09-30", "2026-10-01T17:48:19.041382207Z")
	a := CheckArchive(context.Background(), m, []string{"btc_usd"}, now)
	r, c := a.Books[0].Raw, a.Books[0].Compacted
	// Gaps: nothing from the window start (21:00) to 01:00, and 4 h since the last flush.
	if r.Status != Fail || !strings.Contains(r.Message, "collector may be down") || r.FlushGaps24h != 2 {
		t.Fatalf("raw %+v", r)
	}
	if c.Status != Fail || c.DaysBehind != 6 || !strings.Contains(c.Message, "6 days ago") {
		t.Fatalf("compaction %+v", c)
	}
	if a.Status != Fail || a.Books[0].Status != Fail {
		t.Fatalf("status %s", a.Status)
	}

	// Late but not down, and two days behind: warnings.
	m2 := &objstore.Mem{}
	putFlushes(m2, "btc_usd", now.Add(-25*time.Hour), 24) // last ~2 h ago
	putCompacted(m2, "btc_usd", "2026-10-04", "2026-10-05T01:00:00Z")
	a = CheckArchive(context.Background(), m2, []string{"btc_usd"}, now)
	if a.Books[0].Raw.Status != Warn || a.Books[0].Compacted.Status != Warn || a.Status != Warn {
		t.Fatalf("want warnings, got %+v", a.Books[0])
	}
}

func TestArchiveGapAndMissing(t *testing.T) {
	m := &objstore.Mem{}
	// Hourly, but nothing between 10:00 and 13:00 today.
	putFlushes(m, "btc_mxn", now.Add(-26*time.Hour), 15) // up to 09:00 (+20 s)
	putFlushes(m, "btc_mxn", time.Date(2026, 10, 7, 13, 0, 0, 0, time.UTC), 8)
	a := CheckArchive(context.Background(), m, []string{"btc_mxn", "eth_mxn"}, now)
	r := a.Books[0].Raw
	if r.Status != Warn || r.FlushGaps24h != 1 || r.MaxFlushGapMinutes != 240 {
		t.Fatalf("gap %+v", r)
	}
	if e := a.Books[1]; e.Raw.Status != Fail || e.Compacted.Status != Fail || e.Compacted.Message != "nothing compacted yet" {
		t.Fatalf("missing book %+v", e)
	}
}

func TestArchiveOffAndError(t *testing.T) {
	if a := CheckArchive(context.Background(), nil, []string{"btc_mxn"}, now); a.Status != Off || len(a.Books) != 0 {
		t.Fatalf("off %+v", a)
	}
	m := &objstore.Mem{Err: errors.New("AccessDenied")}
	a := CheckArchive(context.Background(), m, []string{"btc_mxn"}, now)
	if a.Status != Unknown || a.Error != "AccessDenied" || a.Books[0].Raw.Status != Unknown {
		t.Fatalf("error %+v", a)
	}
}

func TestWorst(t *testing.T) {
	if Worst() != OK || Worst(OK, Off) != Off || Worst(Off, Unknown) != Unknown || Worst(Warn, Unknown) != Warn || Worst(OK, Fail, Warn) != Fail {
		t.Fatal("rank order")
	}
}

const sampleLog = `daily-executor 2e1030017900+dirty | as of 2026-10-05T22:39:28Z (Mexico City 2026-10-05 16:39) | mode stage | ledger ./daily-executor-data/stage/ledger.jsonl | risk policy default-2026-10-03

[btc_mxn] FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md
  candles    : 3414 bars 2017-05-31 .. 2026-10-04 (sha256 e6b7848af23e1b5e) -> daily-executor-data/stage/candles/btc_mxn_daily_2026-10-04.csv
  rule       : 2026-10-04 close 1562180.00 vs SMA50 1364552.20 -> LONG (was long)
  paper next : 2026-10-05 open -> HOLD

[btc_usd] FORWARD-TEST-PREREGISTRATION-SMA50-BTCUSD-2026-09-29.md
  ledger     : 2026-10-04 already recorded at 2026-10-05T22:39:48Z, unchanged; nothing to do
[btc_mxn] 22:39:48Z stage: rule says long, executor holds long (0.00099999 BTC) -> none 0.00000000 BTC
[btc_mxn] 22:39:48Z ledger: recorded
`

func writeLog(t *testing.T, dir, name, body string, mod time.Time) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestReadRuns(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "run-20261005T223928Z.log", sampleLog+"upload=ok s3://b/daily-executor/stage\nexit=0\n", now.Add(-46*time.Hour))
	writeLog(t, dir, "run-20261004T010000Z.log", sampleLog+"[btc_mxn] 01:00:00Z error: candles: refusing a revised history\nexit=1\n", now.Add(-3*24*time.Hour))
	writeLog(t, dir, "run-20261003T010000Z.log", sampleLog, now.Add(-4*24*time.Hour))
	writeLog(t, dir, "run-20261007T204500Z.log", sampleLog, now.Add(-10*time.Minute))
	writeLog(t, dir, "notes.txt", "x", now)

	runs, err := ReadRuns(dir, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 || runs[0].File != "run-20261007T204500Z.log" || runs[2].File != "run-20261004T010000Z.log" {
		t.Fatalf("order %+v", runs)
	}
	if runs[0].Status != Unknown || runs[0].ExitCode != nil {
		t.Fatalf("in progress %+v", runs[0])
	}
	ok := runs[1]
	if ok.Status != OK || *ok.ExitCode != 0 || ok.Version != "2e1030017900+dirty" || ok.Mode != "stage" ||
		ok.StartedAt != "2026-10-05T22:39:28Z" || ok.Upload != "ok" || ok.UploadTarget != "s3://b/daily-executor/stage" {
		t.Fatalf("ok run %+v", ok)
	}
	if len(ok.Books) != 2 || ok.Books[0].Ledger != "recorded" || !strings.HasPrefix(ok.Books[0].Stage, "rule says long") ||
		!strings.HasPrefix(ok.Books[1].Ledger, "2026-10-04 already recorded") {
		t.Fatalf("books %+v", ok.Books)
	}
	bad := runs[2]
	if bad.Status != Fail || *bad.ExitCode != 1 || len(bad.Errors) != 1 || !strings.Contains(bad.Errors[0], "refusing") {
		t.Fatalf("failed run %+v", bad)
	}

	all, _ := ReadRuns(dir, 10, now)
	if len(all) != 4 || all[3].Status != Warn || !strings.Contains(all[3].Message, "no exit code") {
		t.Fatalf("old run without exit line %+v", all[3])
	}
	if none, err := ReadRuns(filepath.Join(dir, "missing"), 5, now); err != nil || len(none) != 0 {
		t.Fatalf("missing dir: %v %v", none, err)
	}
}

func TestRunUploadFailedIsWarn(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "run-20261007T010000Z.log", sampleLog+"upload=failed s3://b/x\nexit=0\n", now.Add(-20*time.Hour))
	r, err := ParseRunLog(filepath.Join(dir, "run-20261007T010000Z.log"), now)
	if err != nil || r.Status != Warn || r.Upload != "failed" {
		t.Fatalf("%+v %v", r, err)
	}
	writeLog(t, dir, "run-20261007T020000Z.log", "daily-executor x | mode stage\nexit=2\n", now)
	r, _ = ParseRunLog(filepath.Join(dir, "run-20261007T020000Z.log"), now)
	if r.Status != Fail || !strings.Contains(r.Message, "refused to run") {
		t.Fatalf("exit 2 %+v", r)
	}
}
