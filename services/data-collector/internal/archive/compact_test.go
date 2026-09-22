package archive_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/data-collector/internal/archive"
	"bitso-trading-platform/data-collector/internal/models"
	"bitso-trading-platform/data-collector/internal/sink"
)

var now = time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

func opts() archive.Options {
	return archive.Options{
		SourcePrefix: "trades",
		DestPrefix:   "trades_compacted",
		Book:         "btc_mxn",
		Settle:       2 * time.Hour,
		Workers:      4,
		Now:          func() time.Time { return now },
		Logger:       log.New(io.Discard, "", 0),
	}
}

// seedDay writes files small Parquet objects of rowsPerFile trades each into
// the source partition for day and returns the number of rows written.
func seedDay(t *testing.T, st *archive.MemStore, day time.Time, files, rowsPerFile int, tid *int64) int {
	t.Helper()
	dir := sink.PartitionDir("trades", "btc_mxn", day)
	for f := 0; f < files; f++ {
		var rows []models.Trade
		for r := 0; r < rowsPerFile; r++ {
			ts := day.Add(time.Duration(f*rowsPerFile+r) * time.Minute)
			rows = append(rows, models.Trade{
				Book: "btc_mxn", TID: *tid, Price: 1e6 + float64(*tid), Amount: 0.001,
				MakerSide: "buy", ExchangeTS: ts, ReceivedAt: ts.Add(200 * time.Millisecond),
			})
			*tid++
		}
		data, err := sink.EncodeParquet(rows)
		if err != nil {
			t.Fatal(err)
		}
		key := fmt.Sprintf("%s/trades-%s-%03d.parquet", dir, day.Format("20060102"), f)
		_ = st.Put(context.Background(), key, data)
	}
	return files * rowsPerFile
}

func countRows(t *testing.T, st *archive.MemStore, prefix string) (files, rows int) {
	t.Helper()
	objs, _ := st.List(context.Background(), prefix)
	for _, o := range objs {
		if !strings.HasSuffix(o.Key, ".parquet") {
			continue
		}
		data, _ := st.Get(context.Background(), o.Key)
		got, err := sink.DecodeParquet(data)
		if err != nil {
			t.Fatalf("decode %s: %v", o.Key, err)
		}
		files++
		rows += len(got)
	}
	return files, rows
}

func byStatus(sum archive.Summary) map[string]archive.Status {
	m := map[string]archive.Status{}
	for _, p := range sum.Partitions {
		m[p.Partition] = p.Status
	}
	return m
}

func TestCompactValidatesAndLeavesSourceUntouched(t *testing.T) {
	st := archive.NewMemStore()
	var tid int64 = 1
	d1 := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	today := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	want := seedDay(t, st, d1, 40, 3, &tid) + seedDay(t, st, d2, 25, 2, &tid)
	seedDay(t, st, today, 5, 2, &tid)

	sum, err := archive.Compact(context.Background(), st, opts())
	if err != nil {
		t.Fatal(err)
	}
	status := byStatus(sum)
	if status["year=2026/month=08/day=19"] != archive.StatusCompacted ||
		status["year=2026/month=08/day=20"] != archive.StatusCompacted {
		t.Fatalf("statuses=%v", status)
	}
	if status["year=2026/month=09/day=22"] != archive.StatusNotSettled {
		t.Fatalf("today must not be compacted, statuses=%v", status)
	}

	srcFiles, _, srcRows, dstFiles, _, dstRows := sum.Totals()
	if srcFiles != 65 || srcRows != want || dstFiles != 2 || dstRows != want {
		t.Fatalf("totals src files=%d rows=%d dst files=%d rows=%d, want rows=%d",
			srcFiles, srcRows, dstFiles, dstRows, want)
	}

	files, rows := countRows(t, st, "trades_compacted/")
	if files != 2 || rows != want {
		t.Fatalf("compacted objects files=%d rows=%d", files, rows)
	}
	files, _ = countRows(t, st, "trades/")
	if files != 70 {
		t.Fatalf("source files must be untouched, got %d", files)
	}
}

func TestCompactIsIdempotentAndRebuildsChangedPartitions(t *testing.T) {
	st := archive.NewMemStore()
	var tid int64 = 1
	d1 := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	seedDay(t, st, d1, 10, 2, &tid)
	seedDay(t, st, d2, 10, 2, &tid)

	if _, err := archive.Compact(context.Background(), st, opts()); err != nil {
		t.Fatal(err)
	}
	sum, _ := archive.Compact(context.Background(), st, opts())
	for _, p := range sum.Partitions {
		if p.Status != archive.StatusUpToDate {
			t.Fatalf("second run should skip %s, got %s", p.Partition, p.Status)
		}
	}

	// A late file in day 2 invalidates only that partition.
	dir := sink.PartitionDir("trades", "btc_mxn", d2)
	data, _ := sink.EncodeParquet([]models.Trade{{Book: "btc_mxn", TID: 999, ExchangeTS: d2, ReceivedAt: d2}})
	_ = st.Put(context.Background(), dir+"/trades-late.parquet", data)

	sum, _ = archive.Compact(context.Background(), st, opts())
	status := byStatus(sum)
	if status["year=2026/month=08/day=19"] != archive.StatusUpToDate ||
		status["year=2026/month=08/day=20"] != archive.StatusCompacted {
		t.Fatalf("statuses=%v", status)
	}
	_, rows := countRows(t, st, "trades_compacted/book=btc_mxn/year=2026/month=08/day=20/")
	if rows != 21 {
		t.Fatalf("rebuilt partition rows=%d want 21", rows)
	}
}

func TestCompactFailsPartitionOnUnreadableSource(t *testing.T) {
	st := archive.NewMemStore()
	var tid int64 = 1
	d1 := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	seedDay(t, st, d1, 3, 2, &tid)
	_ = st.Put(context.Background(), sink.PartitionDir("trades", "btc_mxn", d1)+"/corrupt.parquet", []byte("not parquet"))

	sum, _ := archive.Compact(context.Background(), st, opts())
	if sum.Failed() != 1 {
		t.Fatalf("expected 1 failed partition, got %v", byStatus(sum))
	}
	if objs, _ := st.List(context.Background(), "trades_compacted/"); len(objs) != 0 {
		t.Fatalf("failed partition must not produce output, got %v", objs)
	}
}

func TestCutoverReplacesSmallFilesWithValidatedOutput(t *testing.T) {
	st := archive.NewMemStore()
	var tid int64 = 1
	d1 := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	want := seedDay(t, st, d1, 30, 2, &tid) + seedDay(t, st, d2, 20, 3, &tid)

	plan, _ := archive.PlanCutover(context.Background(), st, opts())
	if len(plan.Items) != 0 {
		t.Fatal("cutover must require compaction first")
	}

	if _, err := archive.Compact(context.Background(), st, opts()); err != nil {
		t.Fatal(err)
	}
	plan, err := archive.PlanCutover(context.Background(), st, opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 2 || len(plan.Items[0].DeleteKeys) != 30 {
		t.Fatalf("plan=%+v", plan)
	}
	if err := archive.Cutover(context.Background(), st, opts(), plan); err != nil {
		t.Fatal(err)
	}

	files, rows := countRows(t, st, "trades/")
	if files != 2 || rows != want {
		t.Fatalf("after cutover source files=%d rows=%d want 2/%d", files, rows, want)
	}

	plan, _ = archive.PlanCutover(context.Background(), st, opts())
	if len(plan.Items) != 0 || plan.Skipped["year=2026/month=08/day=19"] != "already cut over" {
		t.Fatalf("second plan=%+v", plan)
	}

	// Re-running compaction after cutover rebuilds from the single file with identical rows.
	sum, _ := archive.Compact(context.Background(), st, opts())
	if sum.Failed() != 0 {
		t.Fatalf("post-cutover compaction failed: %v", byStatus(sum))
	}
	if _, _, srcRows, _, _, dstRows := sum.Totals(); srcRows != want || dstRows != want {
		t.Fatalf("post-cutover rows src=%d dst=%d want %d", srcRows, dstRows, want)
	}
}

func TestCutoverResumesAfterPartialCutover(t *testing.T) {
	st := archive.NewMemStore()
	var tid int64 = 1
	d1 := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	want := seedDay(t, st, d1, 10, 2, &tid)
	if _, err := archive.Compact(context.Background(), st, opts()); err != nil {
		t.Fatal(err)
	}

	// Simulate a crash after the cutover object was written but before deletes.
	compacted, _ := st.Get(context.Background(), "trades_compacted/book=btc_mxn/year=2026/month=08/day=19/trades-20260819.parquet")
	cutKey := sink.PartitionDir("trades", "btc_mxn", d1) + "/" + archive.CutoverName(d1)
	_ = st.Put(context.Background(), cutKey, compacted)

	sum, _ := archive.Compact(context.Background(), st, opts())
	if sum.Failed() != 1 {
		t.Fatalf("compaction must refuse a partially cut-over partition, got %v", byStatus(sum))
	}

	plan, _ := archive.PlanCutover(context.Background(), st, opts())
	if len(plan.Items) != 1 {
		t.Fatalf("plan should resume the partial cutover, got %+v", plan)
	}
	for _, k := range plan.Items[0].DeleteKeys {
		if k == cutKey {
			t.Fatal("cutover must never delete its own output")
		}
	}
	if err := archive.Cutover(context.Background(), st, opts(), plan); err != nil {
		t.Fatal(err)
	}
	files, rows := countRows(t, st, "trades/")
	if files != 1 || rows != want {
		t.Fatalf("files=%d rows=%d want 1/%d", files, rows, want)
	}
}
