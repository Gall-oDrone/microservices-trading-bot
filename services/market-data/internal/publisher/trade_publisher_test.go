package publisher

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/kafka"
	"bitso-trading-platform/shared/pkg/models"
)

// MockProducer is a mock implementation of kafka.Producer for testing
type MockProducer struct {
	producedMessages []kafka.Message
	produceError     error
	mu               sync.RWMutex
}

func NewMockProducer() *MockProducer {
	return &MockProducer{
		producedMessages: make([]kafka.Message, 0),
	}
}

func (m *MockProducer) Produce(ctx context.Context, key, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.produceError != nil {
		return m.produceError
	}

	m.producedMessages = append(m.producedMessages, kafka.Message{
		Key:   key,
		Value: value,
		Time:  time.Now(),
	})
	return nil
}

func (m *MockProducer) ProduceMessage(ctx context.Context, msg kafka.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.produceError != nil {
		return m.produceError
	}

	m.producedMessages = append(m.producedMessages, msg)
	return nil
}

func (m *MockProducer) ProduceMessages(ctx context.Context, messages ...kafka.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.produceError != nil {
		return m.produceError
	}

	m.producedMessages = append(m.producedMessages, messages...)
	return nil
}

func (m *MockProducer) Close() error {
	return nil
}

func (m *MockProducer) Stats() kafka.WriterStats {
	return kafka.WriterStats{}
}

func (m *MockProducer) GetProducedMessages() []kafka.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	messages := make([]kafka.Message, len(m.producedMessages))
	copy(messages, m.producedMessages)
	return messages
}

func (m *MockProducer) SetProduceError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.produceError = err
}

func (m *MockProducer) ClearProduceError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.produceError = nil
}

// TestPublisher tests the trade publisher functionality
func TestPublisher(t *testing.T) {
	// Create test logger
	logger := log.New(os.Stdout, "[TEST-PUBLISHER] ", log.LstdFlags)

	// Create mock producer
	mockProducer := NewMockProducer()

	// Create input channel
	tradesInput := make(chan *models.TradeEvent, 10)

	// Create publisher
	config := &PublisherConfig{
		Logger:      logger,
		Producer:    mockProducer,
		Topic:       "test-trades",
		TradesInput: tradesInput,
	}
	publisher := NewPublisher(config)

	// Test publisher creation
	if publisher == nil {
		t.Fatal("Failed to create publisher")
	}

	// Test initial statistics
	stats := publisher.GetStatistics()
	if stats == nil {
		t.Fatal("Failed to get initial statistics")
	}
	if stats.MessagesPublished != 0 {
		t.Errorf("Expected 0 messages published, got %d", stats.MessagesPublished)
	}

	// Start publisher
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := publisher.Start(ctx); err != nil {
		t.Fatalf("Failed to start publisher: %v", err)
	}

	// Test publishing a valid trade
	testTrade := createTestTradeEvent()
	tradesInput <- testTrade

	// Wait for publishing
	time.Sleep(100 * time.Millisecond)

	// Check statistics
	stats = publisher.GetStatistics()
	if stats.MessagesPublished != 1 {
		t.Errorf("Expected 1 message published, got %d", stats.MessagesPublished)
	}
	if stats.BytesPublished == 0 {
		t.Error("Expected bytes published to be greater than 0")
	}

	// Check mock producer
	messages := mockProducer.GetProducedMessages()
	if len(messages) != 1 {
		t.Errorf("Expected 1 message in mock producer, got %d", len(messages))
	}

	// Verify message content
	message := messages[0]
	expectedKey := "btc_mxn:12345"
	if string(message.Key) != expectedKey {
		t.Errorf("Expected key %s, got %s", expectedKey, string(message.Key))
	}

	// Verify message value is valid JSON
	var tradeEvent models.TradeEvent
	if err := json.Unmarshal(message.Value, &tradeEvent); err != nil {
		t.Errorf("Failed to unmarshal message value: %v", err)
	}
	if tradeEvent.Book != "btc_mxn" {
		t.Errorf("Expected book btc_mxn, got %s", tradeEvent.Book)
	}
	if tradeEvent.ID != 12345 {
		t.Errorf("Expected ID 12345, got %d", tradeEvent.ID)
	}

	// Stop publisher
	if err := publisher.Stop(); err != nil {
		t.Errorf("Failed to stop publisher: %v", err)
	}
}

// TestPublisherErrorHandling tests error handling
func TestPublisherErrorHandling(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-ERRORS] ", log.LstdFlags)
	mockProducer := NewMockProducer()
	tradesInput := make(chan *models.TradeEvent, 10)

	config := &PublisherConfig{
		Logger:      logger,
		Producer:    mockProducer,
		Topic:       "test-trades",
		TradesInput: tradesInput,
	}
	publisher := NewPublisher(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := publisher.Start(ctx); err != nil {
		t.Fatalf("Failed to start publisher: %v", err)
	}

	// Test publishing nil trade
	tradesInput <- nil

	time.Sleep(100 * time.Millisecond)

	// Should not crash and should increment failed counter
	stats := publisher.GetStatistics()
	if stats.MessagesFailed == 0 {
		t.Error("Expected failed counter to be incremented for nil trade")
	}

	// Test producer error
	mockProducer.SetProduceError(&testError{message: "test error"})
	testTrade := createTestTradeEvent()
	tradesInput <- testTrade

	time.Sleep(100 * time.Millisecond)

	// Should increment failed counter
	stats = publisher.GetStatistics()
	if stats.MessagesFailed == 0 {
		t.Error("Expected failed counter to be incremented for producer error")
	}
	if stats.LastError == "" {
		t.Error("Expected last error to be set")
	}

	publisher.Stop()
}

// TestPublisherConcurrency tests concurrent publishing
func TestPublisherConcurrency(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-CONCURRENCY] ", log.LstdFlags)
	mockProducer := NewMockProducer()
	tradesInput := make(chan *models.TradeEvent, 100)

	config := &PublisherConfig{
		Logger:      logger,
		Producer:    mockProducer,
		Topic:       "test-trades",
		TradesInput: tradesInput,
	}
	publisher := NewPublisher(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := publisher.Start(ctx); err != nil {
		t.Fatalf("Failed to start publisher: %v", err)
	}

	// Send multiple trades concurrently
	var wg sync.WaitGroup
	numTrades := 10

	for i := 0; i < numTrades; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			trade := createTestTradeEvent()
			trade.ID = uint64(10000 + id)
			trade.Book = "eth_mxn"
			tradesInput <- trade
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	// Check statistics
	stats := publisher.GetStatistics()
	if stats.MessagesPublished != int64(numTrades) {
		t.Errorf("Expected %d messages published, got %d", numTrades, stats.MessagesPublished)
	}

	// Check mock producer
	messages := mockProducer.GetProducedMessages()
	if len(messages) != numTrades {
		t.Errorf("Expected %d messages in mock producer, got %d", numTrades, len(messages))
	}

	publisher.Stop()
}

// TestPublisherStatistics tests statistics tracking
func TestPublisherStatistics(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-STATS] ", log.LstdFlags)
	mockProducer := NewMockProducer()
	tradesInput := make(chan *models.TradeEvent, 10)

	config := &PublisherConfig{
		Logger:      logger,
		TradesInput: tradesInput,
		Producer:    mockProducer,
		Topic:       "test-trades",
	}
	publisher := NewPublisher(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := publisher.Start(ctx); err != nil {
		t.Fatalf("Failed to start publisher: %v", err)
	}

	// Send multiple trades
	for i := 0; i < 5; i++ {
		trade := createTestTradeEvent()
		trade.ID = uint64(1000 + i)
		tradesInput <- trade
	}

	time.Sleep(100 * time.Millisecond)

	stats := publisher.GetStatistics()
	if stats.MessagesPublished != 5 {
		t.Errorf("Expected 5 messages published, got %d", stats.MessagesPublished)
	}
	if stats.BytesPublished == 0 {
		t.Error("Expected bytes published to be greater than 0")
	}
	if stats.AveragePublishTimeMs == 0 {
		t.Error("Expected average publish time to be greater than 0")
	}

	publisher.Stop()
}

// TestPublisherPerformance tests publishing performance
func TestPublisherPerformance(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-PERFORMANCE] ", log.LstdFlags)
	mockProducer := NewMockProducer()
	tradesInput := make(chan *models.TradeEvent, 1000)

	config := &PublisherConfig{
		Logger:      logger,
		Producer:    mockProducer,
		Topic:       "test-trades",
		TradesInput: tradesInput,
	}
	publisher := NewPublisher(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := publisher.Start(ctx); err != nil {
		t.Fatalf("Failed to start publisher: %v", err)
	}

	// Measure publishing time
	start := time.Now()
	numTrades := 100

	for i := 0; i < numTrades; i++ {
		trade := createTestTradeEvent()
		trade.ID = uint64(10000 + i)
		tradesInput <- trade
	}

	// Wait for all trades to be processed
	time.Sleep(500 * time.Millisecond)

	duration := time.Since(start)
	stats := publisher.GetStatistics()

	t.Logf("Published %d trades in %v", stats.MessagesPublished, duration)
	t.Logf("Average publish time: %.2fms", stats.AveragePublishTimeMs)
	t.Logf("Throughput: %.2f trades/second", float64(stats.MessagesPublished)/duration.Seconds())

	if stats.MessagesPublished != int64(numTrades) {
		t.Errorf("Expected %d messages published, got %d", numTrades, stats.MessagesPublished)
	}

	publisher.Stop()
}

// BenchmarkPublisher benchmarks the publisher performance
func BenchmarkPublisher(b *testing.B) {
	logger := log.New(os.Stdout, "[BENCH-PUBLISHER] ", log.LstdFlags)
	mockProducer := NewMockProducer()
	tradesInput := make(chan *models.TradeEvent, 1000)

	config := &PublisherConfig{
		Logger:      logger,
		Producer:    mockProducer,
		Topic:       "test-trades",
		TradesInput: tradesInput,
	}
	publisher := NewPublisher(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	publisher.Start(ctx)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			trade := createTestTradeEvent()
			trade.ID = uint64(time.Now().UnixNano())
			select {
			case tradesInput <- trade:
			default:
				// Channel full, skip
			}
		}
	})

	publisher.Stop()
}

// Helper function to create a test trade event
func createTestTradeEvent() *models.TradeEvent {
	return &models.TradeEvent{
		ID:           12345,
		Book:         "btc_mxn",
		Price:        50000.0,
		Amount:       0.001,
		Value:        50.0,
		MakerOrderID: "maker-order-123",
		TakerOrderID: "taker-order-456",
		MakerSide:    "buy",
		Timestamp:    time.Now(),
		CreatedAt:    uint64(time.Now().UnixMilli()),
		ReceivedAt:   time.Now(),
		Source:       "bitso_websocket",
		Metadata: map[string]interface{}{
			"test": true,
		},
	}
}

// testError is a simple error type for testing
type testError struct {
	message string
}

func (e *testError) Error() string {
	return e.message
}
