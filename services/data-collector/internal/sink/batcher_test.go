package sink_test

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/data-collector/internal/clock"
	"bitso-trading-platform/data-collector/internal/models"
	"bitso-trading-platform/data-collector/internal/sink"
)

func sampleTrade(ts time.Time) models.Trade {
	return models.Trade{
		Book:       "btc_mxn",
		TID:        1,
		Price:      1000000,
		Amount:     0.01,
		MakerSide:  "buy",
		ExchangeTS: ts,
		ReceivedAt: ts,
	}
}

// batchSizes decodes every stored object and returns row counts in key
// (i.e. chronological) order.
func batchSizes(t *testing.T, mem *sink.MemObjectSink) []int {
	t.Helper()
	keys := make([]string, 0, len(mem.Objects))
	for k := range mem.Objects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sizes := make([]int, 0, len(keys))
	for _, k := range keys {
		rows, err := sink.DecodeParquet(mem.Objects[k])
		if err != nil {
			t.Fatalf("decode %s: %v", k, err)
		}
		sizes = append(sizes, len(rows))
	}
	return sizes
}

func TestParquetBatcherFlushesOnRowCount(t *testing.T) {
	mem := sink.NewMemObjectSink()
	clk := &clock.FakeClock{T: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)}
	b := sink.NewParquetBatcher(mem, "trades", time.Hour, 3, clk, nil)

	ctx := context.Background()
	ts := clk.Now()
	_ = b.Add(ctx, []models.Trade{sampleTrade(ts)})
	_ = b.Add(ctx, []models.Trade{sampleTrade(ts)})
	if mem.CallCount() != 0 {
		t.Fatalf("unexpected flush before threshold")
	}
	if b.BufferLen() != 2 {
		t.Fatalf("buffer=%d", b.BufferLen())
	}

	if err := b.Add(ctx, []models.Trade{sampleTrade(ts)}); err != nil {
		t.Fatal(err)
	}
	if mem.CallCount() != 1 {
		t.Fatalf("expected 1 flush, got %d", mem.CallCount())
	}
	if b.BufferLen() != 0 {
		t.Fatalf("buffer should be empty after flush")
	}

	var key string
	for k := range mem.Objects {
		key = k
	}
	if !strings.HasPrefix(key, "trades/book=btc_mxn/year=2026/month=07/day=25/") {
		t.Fatalf("partition key=%s", key)
	}
	if !strings.HasSuffix(key, ".parquet") {
		t.Fatalf("key=%s", key)
	}
	if got := batchSizes(t, mem); len(got) != 1 || got[0] != 3 {
		t.Fatalf("batch sizes=%v, want [3]", got)
	}
}

func TestParquetBatcherFlushesOnStop(t *testing.T) {
	mem := sink.NewMemObjectSink()
	clk := &clock.FakeClock{T: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)}
	b := sink.NewParquetBatcher(mem, "trades", time.Hour, 1000, clk, nil)
	ctx := context.Background()
	_ = b.Add(ctx, []models.Trade{sampleTrade(clk.Now()), sampleTrade(clk.Now())})
	if err := b.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if got := batchSizes(t, mem); len(got) != 1 || got[0] != 2 {
		t.Fatalf("expected buffered rows flushed on stop, sizes=%v", got)
	}
}

func TestParquetBatcherStopRetriesFailedFlush(t *testing.T) {
	mem := sink.NewMemObjectSink()
	mem.Fails = 1
	clk := &clock.FakeClock{T: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)}
	b := sink.NewParquetBatcher(mem, "trades", time.Hour, 1000, clk, nil)
	_ = b.Add(context.Background(), []models.Trade{sampleTrade(clk.Now())})

	if err := b.Stop(context.Background()); err != nil {
		t.Fatalf("stop should succeed after retry: %v", err)
	}
	if mem.CallCount() != 2 || len(mem.Objects) != 1 {
		t.Fatalf("calls=%d objects=%d", mem.CallCount(), len(mem.Objects))
	}
}

func TestParquetBatcherFlushIfDueUsesOldestRowAge(t *testing.T) {
	mem := sink.NewMemObjectSink()
	clk := &clock.FakeClock{T: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)}
	b := sink.NewParquetBatcher(mem, "trades", time.Hour, 1000, clk, nil)
	ctx := context.Background()

	_ = b.FlushIfDue(ctx)
	if mem.CallCount() != 0 {
		t.Fatal("empty buffer must not flush")
	}

	_ = b.Add(ctx, []models.Trade{sampleTrade(clk.Now())})
	clk.Advance(30 * time.Minute)
	_ = b.Add(ctx, []models.Trade{sampleTrade(clk.Now())})
	clk.Advance(29 * time.Minute)
	_ = b.FlushIfDue(ctx)
	if mem.CallCount() != 0 {
		t.Fatal("flushed before oldest row reached max age")
	}

	clk.Advance(time.Minute)
	if err := b.FlushIfDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := batchSizes(t, mem); len(got) != 1 || got[0] != 2 {
		t.Fatalf("batch sizes=%v, want [2]", got)
	}
}

func TestParquetBatcherStartLoopFlushesByAge(t *testing.T) {
	mem := sink.NewMemObjectSink()
	b := sink.NewParquetBatcher(mem, "trades", 50*time.Millisecond, 1000, clock.RealClock{}, nil)
	b.Start()
	defer func() { _ = b.Stop(context.Background()) }()

	_ = b.Add(context.Background(), []models.Trade{sampleTrade(time.Now().UTC())})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mem.CallCount() >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected time-based flush, calls=%d", mem.CallCount())
}

func TestParquetBatcherRequeuesOnFailure(t *testing.T) {
	mem := sink.NewMemObjectSink()
	mem.Fails = 1
	var failCount int
	clk := &clock.FakeClock{T: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)}
	b := sink.NewParquetBatcher(mem, "trades", time.Hour, 1, clk, func(error) { failCount++ })

	if err := b.Add(context.Background(), []models.Trade{sampleTrade(clk.Now())}); err == nil {
		t.Fatal("expected error")
	}
	if failCount != 1 {
		t.Fatalf("failCount=%d", failCount)
	}
	if b.BufferLen() != 1 {
		t.Fatalf("expected requeue, buffer=%d", b.BufferLen())
	}

	// A requeued batch keeps its original age, so it is retried on the next check.
	clk.Advance(time.Hour)
	if err := b.FlushIfDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mem.CallCount() != 2 {
		t.Fatalf("calls=%d", mem.CallCount())
	}
}

func TestParquetBatcherSplitsBatchAcrossMidnight(t *testing.T) {
	mem := sink.NewMemObjectSink()
	clk := &clock.FakeClock{T: time.Date(2026, 7, 26, 0, 30, 0, 0, time.UTC)}
	b := sink.NewParquetBatcher(mem, "trades", time.Hour, 1000, clk, nil)
	ctx := context.Background()

	late := sampleTrade(time.Date(2026, 7, 25, 23, 59, 0, 0, time.UTC))
	late.TID = 10
	early := sampleTrade(time.Date(2026, 7, 26, 0, 1, 0, 0, time.UTC))
	early.TID = 11
	_ = b.Add(ctx, []models.Trade{late, early})
	if err := b.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	var days []string
	for k := range mem.Objects {
		days = append(days, k[:strings.LastIndex(k, "/")])
	}
	sort.Strings(days)
	want := []string{
		"trades/book=btc_mxn/year=2026/month=07/day=25",
		"trades/book=btc_mxn/year=2026/month=07/day=26",
	}
	if len(days) != 2 || days[0] != want[0] || days[1] != want[1] {
		t.Fatalf("partitions=%v want %v", days, want)
	}
}

// feed simulates the collector: for each trade the clock advances by gap,
// the age check runs (as the Start loop would), then the trade is added.
func feed(b *sink.ParquetBatcher, clk *clock.FakeClock, n int, gap time.Duration, nextTID *int64) {
	ctx := context.Background()
	for i := 0; i < n; i++ {
		clk.Advance(gap)
		_ = b.FlushIfDue(ctx)
		tr := sampleTrade(clk.Now())
		tr.TID = *nextTID
		*nextTID++
		_ = b.Add(ctx, []models.Trade{tr})
	}
}

func TestParquetBatcherSyntheticStreamBatchSizes(t *testing.T) {
	const (
		maxAge  = time.Hour
		maxRows = 5000
	)

	t.Run("steady low rate is bounded by max age", func(t *testing.T) {
		// 2 trades/minute (typical btc_mxn) for 6 hours.
		mem := sink.NewMemObjectSink()
		clk := &clock.FakeClock{T: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
		b := sink.NewParquetBatcher(mem, "trades", maxAge, maxRows, clk, nil)
		var tid int64 = 1
		feed(b, clk, 6*60*2, 30*time.Second, &tid)
		if err := b.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}

		got := batchSizes(t, mem)
		want := []int{120, 120, 120, 120, 120, 120}
		if len(got) != len(want) {
			t.Fatalf("batch sizes=%v want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("batch sizes=%v want %v", got, want)
			}
		}
	})

	t.Run("burst is bounded by max rows", func(t *testing.T) {
		// 10 trades/second for 20 minutes = 12,000 trades inside one max-age window.
		mem := sink.NewMemObjectSink()
		clk := &clock.FakeClock{T: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
		b := sink.NewParquetBatcher(mem, "trades", maxAge, maxRows, clk, nil)
		var tid int64 = 1
		feed(b, clk, 12000, 100*time.Millisecond, &tid)
		if err := b.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}

		got := batchSizes(t, mem)
		want := []int{5000, 5000, 2000}
		if len(got) != len(want) {
			t.Fatalf("batch sizes=%v want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("batch sizes=%v want %v", got, want)
			}
		}
	})
}
