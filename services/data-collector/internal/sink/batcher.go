package sink

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"bitso-trading-platform/data-collector/internal/clock"
	"bitso-trading-platform/data-collector/internal/models"

	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/writer"
)

// ObjectSink stores opaque objects (e.g. Parquet files) by key.
type ObjectSink interface {
	Put(ctx context.Context, key string, body []byte) error
}

// TradeWriter persists trades to a hot store (e.g. Postgres).
type TradeWriter interface {
	WriteTrades(ctx context.Context, trades []models.Trade) error
	WriteGap(ctx context.Context, gap models.GapRecord) error
	Close() error
}

// MemObjectSink is an in-memory ObjectSink for tests.
type MemObjectSink struct {
	mu      sync.Mutex
	Objects map[string][]byte
	Fails   int
	calls   int
}

func NewMemObjectSink() *MemObjectSink {
	return &MemObjectSink{Objects: make(map[string][]byte)}
}

func (m *MemObjectSink) Put(ctx context.Context, key string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.Fails > 0 {
		m.Fails--
		return fmt.Errorf("simulated put failure")
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	m.Objects[key] = cp
	return nil
}

func (m *MemObjectSink) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// ParquetBatcher batches trades and flushes Parquet files to an ObjectSink.
type ParquetBatcher struct {
	sink         ObjectSink
	prefix       string
	flushEvery   time.Duration
	maxRows      int
	clock        clock.Clock
	onFlushFail  func(error)
	onFlushOK    func(key string, n int)

	mu      sync.Mutex
	buffer  []models.Trade
	stopCh  chan struct{}
	wg      sync.WaitGroup
	started bool
}

// NewParquetBatcher creates a batcher. flushEvery and maxRows trigger flushes.
func NewParquetBatcher(
	sink ObjectSink,
	prefix string,
	flushEvery time.Duration,
	maxRows int,
	clk clock.Clock,
	onFlushFail func(error),
) *ParquetBatcher {
	if clk == nil {
		clk = clock.RealClock{}
	}
	if maxRows < 1 {
		maxRows = 1
	}
	return &ParquetBatcher{
		sink:        sink,
		prefix:      prefix,
		flushEvery:  flushEvery,
		maxRows:     maxRows,
		clock:       clk,
		onFlushFail: onFlushFail,
		stopCh:      make(chan struct{}),
	}
}

// Start begins the time-based flush loop.
func (b *ParquetBatcher) Start() {
	b.mu.Lock()
	if b.started {
		b.mu.Unlock()
		return
	}
	b.started = true
	b.mu.Unlock()

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ticker := time.NewTicker(b.flushEvery)
		defer ticker.Stop()
		for {
			select {
			case <-b.stopCh:
				return
			case <-ticker.C:
				_ = b.Flush(context.Background())
			}
		}
	}()
}

// Stop flushes remaining data and stops the timer loop.
func (b *ParquetBatcher) Stop(ctx context.Context) error {
	b.mu.Lock()
	if b.started {
		close(b.stopCh)
		b.started = false
	}
	b.mu.Unlock()
	b.wg.Wait()
	return b.Flush(ctx)
}

// Add appends trades and may flush if the row threshold is hit.
func (b *ParquetBatcher) Add(ctx context.Context, trades []models.Trade) error {
	if len(trades) == 0 {
		return nil
	}
	b.mu.Lock()
	b.buffer = append(b.buffer, trades...)
	shouldFlush := len(b.buffer) >= b.maxRows
	b.mu.Unlock()
	if shouldFlush {
		return b.Flush(ctx)
	}
	return nil
}

// BufferLen returns the current in-memory row count (for tests).
func (b *ParquetBatcher) BufferLen() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buffer)
}

// Flush writes the current buffer as a Parquet object.
func (b *ParquetBatcher) Flush(ctx context.Context) error {
	b.mu.Lock()
	if len(b.buffer) == 0 {
		b.mu.Unlock()
		return nil
	}
	batch := b.buffer
	b.buffer = nil
	b.mu.Unlock()

	data, key, err := encodeParquet(batch, b.prefix, b.clock.Now())
	if err != nil {
		b.requeue(batch)
		if b.onFlushFail != nil {
			b.onFlushFail(err)
		}
		return err
	}

	if err := b.sink.Put(ctx, key, data); err != nil {
		b.requeue(batch)
		if b.onFlushFail != nil {
			b.onFlushFail(err)
		}
		return err
	}
	if b.onFlushOK != nil {
		b.onFlushOK(key, len(batch))
	}
	return nil
}

func (b *ParquetBatcher) requeue(batch []models.Trade) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buffer = append(batch, b.buffer...)
}

func encodeParquet(trades []models.Trade, prefix string, now time.Time) ([]byte, string, error) {
	if len(trades) == 0 {
		return nil, "", fmt.Errorf("empty batch")
	}

	// Partition by the first trade's exchange day (stable within a flush).
	t0 := trades[0].ExchangeTS.UTC()
	key := fmt.Sprintf("%s/book=%s/year=%04d/month=%02d/day=%02d/trades-%s.parquet",
		prefix,
		trades[0].Book,
		t0.Year(), int(t0.Month()), t0.Day(),
		now.UTC().Format("20060102T150405.000"),
	)

	buf := new(bytes.Buffer)
	pw, err := writer.NewParquetWriterFromWriter(buf, new(models.Trade), 4)
	if err != nil {
		return nil, "", fmt.Errorf("parquet writer: %w", err)
	}
	pw.CompressionType = parquet.CompressionCodec_SNAPPY

	for i := range trades {
		if err := pw.Write(trades[i]); err != nil {
			_ = pw.WriteStop()
			return nil, "", fmt.Errorf("parquet write row: %w", err)
		}
	}
	if err := pw.WriteStop(); err != nil {
		return nil, "", fmt.Errorf("parquet write stop: %w", err)
	}
	return buf.Bytes(), key, nil
}
