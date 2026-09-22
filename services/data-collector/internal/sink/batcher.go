package sink

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"bitso-trading-platform/data-collector/internal/clock"
	"bitso-trading-platform/data-collector/internal/models"
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

const (
	minAgeCheckInterval = 10 * time.Millisecond
	maxAgeCheckInterval = 30 * time.Second
	stopFlushAttempts   = 3
)

// ParquetBatcher buffers trades and writes one Parquet object per day
// partition when either the buffer reaches maxRows or its oldest row has
// waited maxAge, whichever happens first.
type ParquetBatcher struct {
	sink        ObjectSink
	prefix      string
	maxAge      time.Duration
	maxRows     int
	clock       clock.Clock
	onFlushFail func(error)
	onFlushOK   func(key string, rows int)

	mu      sync.Mutex
	buffer  []models.Trade
	firstAt time.Time // when the oldest buffered row was added
	stopCh  chan struct{}
	wg      sync.WaitGroup
	started bool
}

// NewParquetBatcher creates a batcher. maxAge is the longest a buffered row
// may wait before a time-based flush; maxRows triggers a size-based flush.
func NewParquetBatcher(
	sink ObjectSink,
	prefix string,
	maxAge time.Duration,
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
		maxAge:      maxAge,
		maxRows:     maxRows,
		clock:       clk,
		onFlushFail: onFlushFail,
		stopCh:      make(chan struct{}),
	}
}

// OnFlush registers a callback invoked after each object is written.
func (b *ParquetBatcher) OnFlush(fn func(key string, rows int)) {
	b.onFlushOK = fn
}

// Start begins the loop that enforces the maxAge trigger.
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
		ticker := time.NewTicker(ageCheckInterval(b.maxAge))
		defer ticker.Stop()
		for {
			select {
			case <-b.stopCh:
				return
			case <-ticker.C:
				_ = b.FlushIfDue(context.Background())
			}
		}
	}()
}

// ageCheckInterval bounds how late a time-based flush can fire relative to
// maxAge (at most 10%, never more than 30s).
func ageCheckInterval(maxAge time.Duration) time.Duration {
	d := maxAge / 10
	if d < minAgeCheckInterval {
		return minAgeCheckInterval
	}
	if d > maxAgeCheckInterval {
		return maxAgeCheckInterval
	}
	return d
}

// Stop halts the age loop and flushes everything still buffered, retrying a
// few times so a transient S3 error on shutdown does not drop the batch.
func (b *ParquetBatcher) Stop(ctx context.Context) error {
	b.mu.Lock()
	if b.started {
		close(b.stopCh)
		b.started = false
	}
	b.mu.Unlock()
	b.wg.Wait()

	var err error
	for attempt := 1; attempt <= stopFlushAttempts; attempt++ {
		if err = b.Flush(ctx); err == nil {
			return nil
		}
		if attempt == stopFlushAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("final flush: %w (context: %v)", err, ctx.Err())
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return fmt.Errorf("final flush after %d attempts: %w", stopFlushAttempts, err)
}

// Add appends trades and flushes if the row threshold is reached.
func (b *ParquetBatcher) Add(ctx context.Context, trades []models.Trade) error {
	if len(trades) == 0 {
		return nil
	}
	b.mu.Lock()
	if len(b.buffer) == 0 {
		b.firstAt = b.clock.Now()
	}
	b.buffer = append(b.buffer, trades...)
	shouldFlush := len(b.buffer) >= b.maxRows
	b.mu.Unlock()
	if shouldFlush {
		return b.Flush(ctx)
	}
	return nil
}

// FlushIfDue flushes when the oldest buffered row has waited at least maxAge.
func (b *ParquetBatcher) FlushIfDue(ctx context.Context) error {
	b.mu.Lock()
	due := len(b.buffer) > 0 && b.clock.Now().Sub(b.firstAt) >= b.maxAge
	b.mu.Unlock()
	if !due {
		return nil
	}
	return b.Flush(ctx)
}

// BufferLen returns the current in-memory row count.
func (b *ParquetBatcher) BufferLen() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buffer)
}

// Flush writes the buffer as one Parquet object per book/day partition.
// Rows from partitions that fail to write are requeued.
func (b *ParquetBatcher) Flush(ctx context.Context) error {
	b.mu.Lock()
	if len(b.buffer) == 0 {
		b.mu.Unlock()
		return nil
	}
	batch := b.buffer
	firstAt := b.firstAt
	b.buffer = nil
	b.firstAt = time.Time{}
	b.mu.Unlock()

	stamp := b.clock.Now().UTC().Format("20060102T150405.000")
	var failed []models.Trade
	var firstErr error
	for _, g := range groupByPartition(batch, b.prefix) {
		key := fmt.Sprintf("%s/trades-%s-%d.parquet", g.dir, stamp, g.trades[0].TID)
		err := b.put(ctx, key, g.trades)
		if err == nil {
			if b.onFlushOK != nil {
				b.onFlushOK(key, len(g.trades))
			}
			continue
		}
		failed = append(failed, g.trades...)
		if firstErr == nil {
			firstErr = err
		}
		if b.onFlushFail != nil {
			b.onFlushFail(err)
		}
	}
	if len(failed) > 0 {
		b.requeue(failed, firstAt)
	}
	return firstErr
}

func (b *ParquetBatcher) put(ctx context.Context, key string, trades []models.Trade) error {
	data, err := EncodeParquet(trades)
	if err != nil {
		return err
	}
	return b.sink.Put(ctx, key, data)
}

func (b *ParquetBatcher) requeue(batch []models.Trade, firstAt time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buffer) == 0 || firstAt.Before(b.firstAt) {
		b.firstAt = firstAt
	}
	b.buffer = append(batch, b.buffer...)
}

// PartitionDir returns the Hive-style partition directory for a book and day,
// e.g. trades/book=btc_mxn/year=2026/month=07/day=25.
func PartitionDir(prefix, book string, day time.Time) string {
	d := day.UTC()
	return fmt.Sprintf("%s/book=%s/year=%04d/month=%02d/day=%02d",
		prefix, book, d.Year(), int(d.Month()), d.Day())
}

type partitionGroup struct {
	dir    string
	trades []models.Trade
}

// groupByPartition splits trades by book and exchange-timestamp UTC day so a
// batch spanning midnight never lands in the wrong day partition.
func groupByPartition(trades []models.Trade, prefix string) []partitionGroup {
	idx := make(map[string]int)
	var groups []partitionGroup
	for _, t := range trades {
		dir := PartitionDir(prefix, t.Book, t.ExchangeTS)
		i, ok := idx[dir]
		if !ok {
			i = len(groups)
			idx[dir] = i
			groups = append(groups, partitionGroup{dir: dir})
		}
		groups[i].trades = append(groups[i].trades, t)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].dir < groups[j].dir })
	return groups
}
