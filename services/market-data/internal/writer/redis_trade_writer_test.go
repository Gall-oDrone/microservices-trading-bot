package writer

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"bitso-trading-platform/market-data/internal/cache"
	"bitso-trading-platform/market-data/internal/historical"
	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
)

// mockCache implements cache.Cache for tests; only SetTrade is used by the writer
type mockCache struct {
	setTradeErr error
	mu          sync.Mutex
	trades      []*models.TradeEvent
}

func (m *mockCache) SetTrade(ctx context.Context, book string, trade *models.TradeEvent) error {
	m.mu.Lock()
	m.trades = append(m.trades, trade)
	m.mu.Unlock()
	return m.setTradeErr
}
func (m *mockCache) GetTrade(ctx context.Context, book string, tradeID uint64) (*models.TradeEvent, error) {
	return nil, nil
}
func (m *mockCache) GetRecentTrades(ctx context.Context, book string, limit int) ([]*models.TradeEvent, error) {
	return nil, nil
}
func (m *mockCache) GetTradesByTimeRange(ctx context.Context, book string, start, end time.Time) ([]*models.TradeEvent, error) {
	return nil, nil
}
func (m *mockCache) SetOrderBook(ctx context.Context, book string, orderBook *bitso.OrderBook) error {
	return nil
}
func (m *mockCache) GetOrderBook(ctx context.Context, book string) (*bitso.OrderBook, error) {
	return nil, nil
}
func (m *mockCache) UpdateOrderBook(ctx context.Context, book string, diff *bitso.WebSocketDiffOrder) error {
	return nil
}
func (m *mockCache) SetTicker(ctx context.Context, book string, ticker *bitso.Ticker) error {
	return nil
}
func (m *mockCache) GetTicker(ctx context.Context, book string) (*bitso.Ticker, error) {
	return nil, nil
}
func (m *mockCache) SetTradeStats(ctx context.Context, book string, stats *cache.TradeStats) error {
	return nil
}
func (m *mockCache) GetTradeStats(ctx context.Context, book string) (*cache.TradeStats, error) {
	return nil, nil
}
func (m *mockCache) Clear(ctx context.Context, pattern string) error {
	return nil
}
func (m *mockCache) Exists(ctx context.Context, key string) (bool, error) {
	return false, nil
}
func (m *mockCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return nil
}
func (m *mockCache) Close() error {
	return nil
}

// mockStorage implements historical.Storage for tests; only StoreTrade is used
type mockStorage struct {
	storeTradeErr error
	mu            sync.Mutex
	trades        []*models.TradeEvent
}

func (m *mockStorage) StoreTrade(ctx context.Context, trade *models.TradeEvent) error {
	m.mu.Lock()
	m.trades = append(m.trades, trade)
	m.mu.Unlock()
	return m.storeTradeErr
}
func (m *mockStorage) GetTradesByTimeRange(ctx context.Context, book string, start, end time.Time) ([]*models.TradeEvent, error) {
	return nil, nil
}
func (m *mockStorage) GetTradesByBook(ctx context.Context, book string, limit int) ([]*models.TradeEvent, error) {
	return nil, nil
}
func (m *mockStorage) GetTradeByID(ctx context.Context, book string, tradeID uint64) (*models.TradeEvent, error) {
	return nil, nil
}
func (m *mockStorage) StoreOrderBook(ctx context.Context, book string, orderBook *bitso.OrderBook) error {
	return nil
}
func (m *mockStorage) GetOrderBookHistory(ctx context.Context, book string, start, end time.Time) ([]*historical.OrderBookSnapshot, error) {
	return nil, nil
}
func (m *mockStorage) StoreTicker(ctx context.Context, book string, ticker *bitso.Ticker) error {
	return nil
}
func (m *mockStorage) GetTickerHistory(ctx context.Context, book string, start, end time.Time) ([]*bitso.Ticker, error) {
	return nil, nil
}
func (m *mockStorage) GetTradeStatistics(ctx context.Context, book string, start, end time.Time) (*historical.TradeStatistics, error) {
	return nil, nil
}
func (m *mockStorage) GetVolumeStatistics(ctx context.Context, book string, start, end time.Time) (*historical.VolumeStatistics, error) {
	return nil, nil
}
func (m *mockStorage) CleanupOldData(ctx context.Context, olderThan time.Time) error {
	return nil
}
func (m *mockStorage) GetStorageStats(ctx context.Context) (*historical.StorageStats, error) {
	return nil, nil
}
func (m *mockStorage) Close() error {
	return nil
}

func makeTradeEvent(book string, id uint64, price float64) *models.TradeEvent {
	now := time.Now()
	return &models.TradeEvent{
		Book:            book,
		ID:              id,
		Price:           price,
		Amount:          0.01,
		Value:           price * 0.01,
		MakerSide:       "buy",
		Side:            "sell",
		Timestamp:       now,
		ReceivedAt:      now,
		CreatedAtMillis: now.UnixMilli(),
	}
}

func TestWriter_StartStop(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-WRITER] ", log.LstdFlags)
	tradesInput := make(chan *models.TradeEvent, 10)
	mockCache := &mockCache{}
	mockStorage := &mockStorage{}

	config := &WriterConfig{
		Logger:       logger,
		Cache:        mockCache,
		Storage:      mockStorage,
		TradesInput:  tradesInput,
		OutputBuffer: 5,
	}
	w := NewWriter(config)
	if w == nil {
		t.Fatal("NewWriter returned nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	stats := w.GetStatistics()
	if stats == nil {
		t.Fatal("GetStatistics returned nil")
	}
	if stats.TradesWritten != 0 {
		t.Errorf("expected 0 trades written, got %d", stats.TradesWritten)
	}

	if err := w.Stop(); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestWriter_WriteAndForward(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-WRITER] ", log.LstdFlags)
	tradesInput := make(chan *models.TradeEvent, 10)
	mockCache := &mockCache{}
	mockStorage := &mockStorage{}

	config := &WriterConfig{
		Logger:       logger,
		Cache:        mockCache,
		Storage:      mockStorage,
		TradesInput:  tradesInput,
		OutputBuffer: 5,
	}
	w := NewWriter(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	out := w.GetOutputStream()
	trade := makeTradeEvent("btc_mxn", 100, 50000.0)
	tradesInput <- trade

	select {
	case got := <-out:
		if got == nil {
			t.Fatal("received nil trade")
		}
		if got.Book != trade.Book || got.ID != trade.ID || got.Price != trade.Price {
			t.Errorf("got trade %+v, want Book=btc_mxn ID=100 Price=50000", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for output trade")
	}

	mockCache.mu.Lock()
	cacheCount := len(mockCache.trades)
	mockCache.mu.Unlock()
	mockStorage.mu.Lock()
	storageCount := len(mockStorage.trades)
	mockStorage.mu.Unlock()

	if cacheCount != 1 {
		t.Errorf("cache should have 1 trade, got %d", cacheCount)
	}
	if storageCount != 1 {
		t.Errorf("storage should have 1 trade, got %d", storageCount)
	}

	stats := w.GetStatistics()
	if stats.TradesWritten != 1 {
		t.Errorf("expected 1 trade written, got %d", stats.TradesWritten)
	}
}

func TestWriter_NilTradeSkipped(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-WRITER] ", log.LstdFlags)
	tradesInput := make(chan *models.TradeEvent, 10)
	mockCache := &mockCache{}
	mockStorage := &mockStorage{}

	config := &WriterConfig{
		Logger:       logger,
		Cache:        mockCache,
		Storage:      mockStorage,
		TradesInput:  tradesInput,
		OutputBuffer: 5,
	}
	w := NewWriter(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	tradesInput <- nil
	time.Sleep(100 * time.Millisecond)

	stats := w.GetStatistics()
	if stats.TradesWritten != 0 {
		t.Errorf("nil trade should not be written, got TradesWritten=%d", stats.TradesWritten)
	}
}

func TestWriter_CacheErrorIncrementsStats(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-WRITER] ", log.LstdFlags)
	tradesInput := make(chan *models.TradeEvent, 10)
	mockCache := &mockCache{setTradeErr: errors.New("cache error")}
	mockStorage := &mockStorage{}

	config := &WriterConfig{
		Logger:       logger,
		Cache:        mockCache,
		Storage:      mockStorage,
		TradesInput:  tradesInput,
		OutputBuffer: 5,
	}
	w := NewWriter(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	out := w.GetOutputStream()
	trade := makeTradeEvent("btc_mxn", 200, 51000.0)
	tradesInput <- trade

	select {
	case <-out:
		// trade still forwarded
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for output despite cache error")
	}

	stats := w.GetStatistics()
	if stats.CacheErrors != 1 {
		t.Errorf("expected 1 cache error, got %d", stats.CacheErrors)
	}
}

func TestWriter_StorageErrorIncrementsStats(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-WRITER] ", log.LstdFlags)
	tradesInput := make(chan *models.TradeEvent, 10)
	mockCache := &mockCache{}
	mockStorage := &mockStorage{storeTradeErr: errors.New("storage error")}

	config := &WriterConfig{
		Logger:       logger,
		Cache:        mockCache,
		Storage:      mockStorage,
		TradesInput:  tradesInput,
		OutputBuffer: 5,
	}
	w := NewWriter(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	out := w.GetOutputStream()
	trade := makeTradeEvent("btc_mxn", 300, 52000.0)
	tradesInput <- trade

	select {
	case <-out:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for output despite storage error")
	}

	stats := w.GetStatistics()
	if stats.StorageErrors != 1 {
		t.Errorf("expected 1 storage error, got %d", stats.StorageErrors)
	}
}

func TestWriter_MultipleTrades(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-WRITER] ", log.LstdFlags)
	tradesInput := make(chan *models.TradeEvent, 20)
	mockCache := &mockCache{}
	mockStorage := &mockStorage{}

	config := &WriterConfig{
		Logger:       logger,
		Cache:        mockCache,
		Storage:      mockStorage,
		TradesInput:  tradesInput,
		OutputBuffer: 20,
	}
	w := NewWriter(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	const n = 5
	for i := 0; i < n; i++ {
		tradesInput <- makeTradeEvent("btc_mxn", uint64(400+i), 50000.0+float64(i)*100)
	}

	out := w.GetOutputStream()
	for i := 0; i < n; i++ {
		select {
		case got := <-out:
			if got == nil {
				t.Fatalf("trade %d: nil", i)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for trade %d", i)
		}
	}

	mockCache.mu.Lock()
	cacheCount := len(mockCache.trades)
	mockCache.mu.Unlock()
	mockStorage.mu.Lock()
	storageCount := len(mockStorage.trades)
	mockStorage.mu.Unlock()

	if cacheCount != n {
		t.Errorf("cache should have %d trades, got %d", n, cacheCount)
	}
	if storageCount != n {
		t.Errorf("storage should have %d trades, got %d", n, storageCount)
	}

	stats := w.GetStatistics()
	if stats.TradesWritten != int64(n) {
		t.Errorf("expected %d trades written, got %d", n, stats.TradesWritten)
	}
}
