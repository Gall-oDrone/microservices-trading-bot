package writer

import (
	"context"
	"log"
	"sync"
	"time"

	"bitso-trading-platform/market-data/internal/cache"
	"bitso-trading-platform/market-data/internal/historical"
	"bitso-trading-platform/shared/pkg/models"
)

// RedisTradeWriter persists processed trades to Redis (cache + historical storage)
// and forwards them to an output channel for downstream consumers (e.g. Kafka publisher).
type RedisTradeWriter interface {
	Start(ctx context.Context) error
	Stop() error
	GetOutputStream() <-chan *models.TradeEvent
	GetStatistics() *WriterStatistics
}

// Writer implements RedisTradeWriter
type Writer struct {
	logger *log.Logger

	cache   cache.Cache
	storage historical.Storage

	// Input from trade processor
	tradesInput <-chan *models.TradeEvent
	// Output for Kafka publisher (or other consumers)
	tradesOutput chan *models.TradeEvent

	indicatorRecorder IndicatorRecorder

	// Statistics
	stats      *WriterStatistics
	statsMutex sync.RWMutex

	stopChan chan struct{}
	wg       sync.WaitGroup
}

// WriterStatistics tracks write performance
type WriterStatistics struct {
	StartTime        time.Time
	TradesWritten    int64
	TradesDropped    int64
	CacheErrors      int64
	StorageErrors    int64
	LastTradeTime    time.Time
	LastTradeID      uint64
	AverageWriteMs   float64
}

// IndicatorRecorder is an optional callback to record indicator metrics per trade.
type IndicatorRecorder interface {
	RecordTrade(trade *models.TradeEvent)
}

// WriterConfig holds configuration for the Redis trade writer
type WriterConfig struct {
	Logger            *log.Logger
	Cache             cache.Cache
	Storage           historical.Storage
	TradesInput       <-chan *models.TradeEvent
	OutputBuffer      int
	IndicatorRecorder IndicatorRecorder // optional: record financial indicators for Prometheus
}

// NewWriter creates a new Redis trade writer
func NewWriter(config *WriterConfig) *Writer {
	logger := config.Logger
	if logger == nil {
		logger = log.New(log.Writer(), "[REDIS-TRADE-WRITER] ", log.LstdFlags|log.Lshortfile)
	}
	outputBuffer := config.OutputBuffer
	if outputBuffer == 0 {
		outputBuffer = 100
	}
	return &Writer{
		logger:            logger,
		cache:             config.Cache,
		storage:           config.Storage,
		tradesInput:      config.TradesInput,
		tradesOutput:     make(chan *models.TradeEvent, outputBuffer),
		indicatorRecorder: config.IndicatorRecorder,
		stats: &WriterStatistics{
			StartTime: time.Now(),
		},
		stopChan: make(chan struct{}),
	}
}

// Start begins consuming trades, writing to Redis and forwarding to output
func (w *Writer) Start(ctx context.Context) error {
	w.logger.Println("Starting Redis trade writer...")
	w.wg.Add(1)
	go w.writeLoop(ctx)
	w.logger.Println("✓ Redis trade writer started")
	return nil
}

// Stop gracefully stops the writer and closes the output channel
func (w *Writer) Stop() error {
	w.logger.Println("Stopping Redis trade writer...")
	close(w.stopChan)
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(w.tradesOutput)
		close(done)
	}()
	select {
	case <-done:
		w.logger.Println("Redis trade writer stopped")
	case <-time.After(5 * time.Second):
		w.logger.Println("Warning: Redis trade writer stop timeout")
	}
	return nil
}

// GetOutputStream returns the channel for downstream consumers (e.g. Kafka publisher)
func (w *Writer) GetOutputStream() <-chan *models.TradeEvent {
	return w.tradesOutput
}

// GetStatistics returns current writer statistics
func (w *Writer) GetStatistics() *WriterStatistics {
	w.statsMutex.RLock()
	defer w.statsMutex.RUnlock()
	cp := *w.stats
	return &cp
}

// writeLoop reads from processor, writes to Redis, forwards to output
func (w *Writer) writeLoop(ctx context.Context) {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopChan:
			return
		case <-ctx.Done():
			return
		case trade, ok := <-w.tradesInput:
			if !ok {
				w.logger.Println("Input channel closed")
				return
			}
			w.writeAndForward(ctx, trade)
		}
	}
}

func (w *Writer) writeAndForward(ctx context.Context, trade *models.TradeEvent) {
	if trade == nil {
		return
	}
	if w.indicatorRecorder != nil {
		w.indicatorRecorder.RecordTrade(trade)
	}
	start := time.Now()

	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := w.cache.SetTrade(writeCtx, trade.Book, trade); err != nil {
		w.logger.Printf("Cache SetTrade error for book=%s id=%d: %v", trade.Book, trade.ID, err)
		w.incCacheErrors()
	}
	if err := w.storage.StoreTrade(writeCtx, trade); err != nil {
		w.logger.Printf("Storage StoreTrade error for book=%s id=%d: %v", trade.Book, trade.ID, err)
		w.incStorageErrors()
	}

	w.updateStats(trade, time.Since(start))

	select {
	case w.tradesOutput <- trade:
	case <-time.After(1 * time.Second):
		w.logger.Println("Warning: output channel full, dropping trade")
		w.incDropped()
	}
}

func (w *Writer) updateStats(trade *models.TradeEvent, elapsed time.Duration) {
	w.statsMutex.Lock()
	defer w.statsMutex.Unlock()
	w.stats.TradesWritten++
	w.stats.LastTradeTime = trade.Timestamp
	w.stats.LastTradeID = trade.ID
	ms := elapsed.Seconds() * 1000
	if w.stats.AverageWriteMs == 0 {
		w.stats.AverageWriteMs = ms
	} else {
		w.stats.AverageWriteMs = 0.9*w.stats.AverageWriteMs + 0.1*ms
	}
}

func (w *Writer) incCacheErrors() {
	w.statsMutex.Lock()
	defer w.statsMutex.Unlock()
	w.stats.CacheErrors++
}

func (w *Writer) incStorageErrors() {
	w.statsMutex.Lock()
	defer w.statsMutex.Unlock()
	w.stats.StorageErrors++
}

func (w *Writer) incDropped() {
	w.statsMutex.Lock()
	defer w.statsMutex.Unlock()
	w.stats.TradesDropped++
}
