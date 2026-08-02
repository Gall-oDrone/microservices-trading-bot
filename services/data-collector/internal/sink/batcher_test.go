package sink_test

import (
	"context"
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

	err := b.Add(ctx, []models.Trade{sampleTrade(ts)})
	if err != nil {
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
	if !strings.Contains(key, "book=btc_mxn/year=2026/month=07/day=25/") {
		t.Fatalf("partition key=%s", key)
	}
	if !strings.HasSuffix(key, ".parquet") {
		t.Fatalf("key=%s", key)
	}
	if len(mem.Objects[key]) == 0 {
		t.Fatal("empty parquet body")
	}
}

func TestParquetBatcherFlushesOnStop(t *testing.T) {
	mem := sink.NewMemObjectSink()
	clk := &clock.FakeClock{T: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)}
	// Large max rows so only Stop/Flush triggers
	b := sink.NewParquetBatcher(mem, "trades", time.Hour, 1000, clk, nil)
	ctx := context.Background()
	_ = b.Add(ctx, []models.Trade{sampleTrade(clk.Now())})
	if err := b.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if mem.CallCount() != 1 {
		t.Fatalf("expected flush on stop, calls=%d", mem.CallCount())
	}
}

func TestParquetBatcherTimeBasedFlush(t *testing.T) {
	mem := sink.NewMemObjectSink()
	clk := &clock.FakeClock{T: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)}
	b := sink.NewParquetBatcher(mem, "trades", 50*time.Millisecond, 1000, clk, nil)
	b.Start()
	defer func() { _ = b.Stop(context.Background()) }()

	_ = b.Add(context.Background(), []models.Trade{sampleTrade(clk.Now())})
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

	err := b.Add(context.Background(), []models.Trade{sampleTrade(clk.Now())})
	if err == nil {
		t.Fatal("expected error")
	}
	if failCount != 1 {
		t.Fatalf("failCount=%d", failCount)
	}
	if b.BufferLen() != 1 {
		t.Fatalf("expected requeue, buffer=%d", b.BufferLen())
	}

	if err := b.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mem.CallCount() != 2 {
		t.Fatalf("calls=%d", mem.CallCount())
	}
}
