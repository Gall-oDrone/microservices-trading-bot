package consumer

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/strategy-executor/internal/logger"
	"bitso-trading-platform/strategy-executor/internal/metrics"
)

func TestNewConsumer(t *testing.T) {
	tests := []struct {
		name    string
		config  *ConsumerConfig
		logger  *logger.Logger
		metrics *metrics.Metrics
		wantErr bool
	}{
		{
			name:    "valid configuration",
			config:  &ConsumerConfig{Brokers: []string{"localhost:9092"}, ConsumerGroup: "test-group"},
			logger:  logger.NewDefault(),
			metrics: metrics.New("test"),
			wantErr: false,
		},
		{
			name:    "nil config",
			config:  nil,
			logger:  logger.NewDefault(),
			metrics: metrics.New("test"),
			wantErr: true,
		},
		{
			name:    "nil logger",
			config:  &ConsumerConfig{Brokers: []string{"localhost:9092"}, ConsumerGroup: "test-group"},
			logger:  nil,
			metrics: metrics.New("test"),
			wantErr: true,
		},
		{
			name:    "nil metrics",
			config:  &ConsumerConfig{Brokers: []string{"localhost:9092"}, ConsumerGroup: "test-group"},
			logger:  logger.NewDefault(),
			metrics: nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consumer, err := NewConsumer(tt.config, tt.logger, tt.metrics)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewConsumer() expected error, got nil")
				}
				if consumer != nil {
					t.Errorf("NewConsumer() expected nil consumer on error, got %v", consumer)
				}
			} else {
				if err != nil {
					t.Errorf("NewConsumer() unexpected error: %v", err)
				}
				if consumer == nil {
					t.Errorf("NewConsumer() expected consumer, got nil")
				}
			}
		})
	}
}

func TestConsumer_SubscribeToBook(t *testing.T) {
	consumer := &Consumer{
		logger:          logger.NewDefault(),
		subscribedBooks: make(map[string]bool),
		stats: &ConsumerStatistics{
			ConsumerLag: make(map[string]int64),
		},
	}

	// Test subscribing to a book
	err := consumer.SubscribeToBook("btc_mxn")
	if err != nil {
		t.Errorf("SubscribeToBook() unexpected error: %v", err)
	}

	// Check if book is subscribed
	if !consumer.subscribedBooks["btc_mxn"] {
		t.Error("Expected btc_mxn to be subscribed")
	}

	// Test subscribing to multiple books
	err = consumer.SubscribeToBook("eth_mxn")
	if err != nil {
		t.Errorf("SubscribeToBook() unexpected error: %v", err)
	}

	if len(consumer.subscribedBooks) != 2 {
		t.Errorf("Expected 2 subscribed books, got %d", len(consumer.subscribedBooks))
	}
}

func TestConsumer_UnsubscribeFromBook(t *testing.T) {
	consumer := &Consumer{
		logger: logger.NewDefault(),
		subscribedBooks: map[string]bool{
			"btc_mxn": true,
			"eth_mxn": true,
		},
		stats: &ConsumerStatistics{
			ConsumerLag: make(map[string]int64),
		},
	}

	// Test unsubscribing from a book
	err := consumer.UnsubscribeFromBook("btc_mxn")
	if err != nil {
		t.Errorf("UnsubscribeFromBook() unexpected error: %v", err)
	}

	// Check if book is unsubscribed
	if consumer.subscribedBooks["btc_mxn"] {
		t.Error("Expected btc_mxn to be unsubscribed")
	}

	// Check if other book is still subscribed
	if !consumer.subscribedBooks["eth_mxn"] {
		t.Error("Expected eth_mxn to still be subscribed")
	}

	// Test unsubscribing from non-existent book
	err = consumer.UnsubscribeFromBook("non_existent")
	if err != nil {
		t.Errorf("UnsubscribeFromBook() unexpected error: %v", err)
	}
}

func TestConsumer_GetStatistics(t *testing.T) {
	consumer := &Consumer{
		stats: &ConsumerStatistics{
			StartTime:         time.Now(),
			MessagesConsumed:  100,
			MessagesProcessed: 95,
			MessagesFailed:    5,
			BooksSubscribed:   2,
			ConsumerLag: map[string]int64{
				"btc_mxn": 10,
				"eth_mxn": 5,
			},
		},
	}

	stats := consumer.GetStatistics()

	if stats == nil {
		t.Fatal("GetStatistics() returned nil")
	}

	if stats.MessagesConsumed != 100 {
		t.Errorf("Expected MessagesConsumed 100, got %d", stats.MessagesConsumed)
	}

	if stats.MessagesProcessed != 95 {
		t.Errorf("Expected MessagesProcessed 95, got %d", stats.MessagesProcessed)
	}

	if stats.MessagesFailed != 5 {
		t.Errorf("Expected MessagesFailed 5, got %d", stats.MessagesFailed)
	}

	if stats.BooksSubscribed != 2 {
		t.Errorf("Expected BooksSubscribed 2, got %d", stats.BooksSubscribed)
	}

	if len(stats.ConsumerLag) != 2 {
		t.Errorf("Expected ConsumerLag length 2, got %d", len(stats.ConsumerLag))
	}
}

func TestConsumer_GetConsumedMessages(t *testing.T) {
	consumer := &Consumer{
		tradeEvents: make(chan *models.TradeEvent, 1),
	}

	// Test that we get the same channel
	ch1 := consumer.GetConsumedMessages()
	ch2 := consumer.GetConsumedMessages()

	if ch1 != ch2 {
		t.Error("GetConsumedMessages() should return the same channel")
	}
}

func TestConsumer_Stop(t *testing.T) {
	consumer := &Consumer{
		logger:          logger.NewDefault(),
		stopChan:        make(chan struct{}),
		tradeEvents:     make(chan *models.TradeEvent, 1),
		tickerEvents:    make(chan *TickerEvent, 1),
		orderBookEvents: make(chan *OrderBookEvent, 1),
	}

	// Test stopping the consumer
	err := consumer.Stop()
	if err != nil {
		t.Errorf("Stop() unexpected error: %v", err)
	}

	// Check if stop channel is closed
	select {
	case <-consumer.stopChan:
		// Channel is closed, which is expected
	default:
		t.Error("Expected stop channel to be closed")
	}
}

func TestTickerEvent(t *testing.T) {
	event := &TickerEvent{
		Book:      "btc_mxn",
		Bid:       1000.0,
		Ask:       1001.0,
		Last:      1000.5,
		Volume:    10.5,
		Timestamp: time.Now(),
	}

	if event.Book != "btc_mxn" {
		t.Errorf("Expected Book 'btc_mxn', got '%s'", event.Book)
	}

	if event.Bid != 1000.0 {
		t.Errorf("Expected Bid 1000.0, got %f", event.Bid)
	}

	if event.Ask != 1001.0 {
		t.Errorf("Expected Ask 1001.0, got %f", event.Ask)
	}

	if event.Last != 1000.5 {
		t.Errorf("Expected Last 1000.5, got %f", event.Last)
	}

	if event.Volume != 10.5 {
		t.Errorf("Expected Volume 10.5, got %f", event.Volume)
	}
}

func TestOrderBookEvent(t *testing.T) {
	event := &OrderBookEvent{
		Book: "btc_mxn",
		Bids: []OrderBookEntry{
			{Price: 1000.0, Amount: 1.0},
			{Price: 999.0, Amount: 2.0},
		},
		Asks: []OrderBookEntry{
			{Price: 1001.0, Amount: 1.5},
			{Price: 1002.0, Amount: 2.5},
		},
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"source": "test",
		},
	}

	if event.Book != "btc_mxn" {
		t.Errorf("Expected Book 'btc_mxn', got '%s'", event.Book)
	}

	if len(event.Bids) != 2 {
		t.Errorf("Expected 2 bids, got %d", len(event.Bids))
	}

	if len(event.Asks) != 2 {
		t.Errorf("Expected 2 asks, got %d", len(event.Asks))
	}

	if event.Bids[0].Price != 1000.0 {
		t.Errorf("Expected first bid price 1000.0, got %f", event.Bids[0].Price)
	}

	if event.Asks[0].Price != 1001.0 {
		t.Errorf("Expected first ask price 1001.0, got %f", event.Asks[0].Price)
	}
}

func TestOrderBookEntry(t *testing.T) {
	entry := OrderBookEntry{
		Price:  1000.0,
		Amount: 1.5,
	}

	if entry.Price != 1000.0 {
		t.Errorf("Expected Price 1000.0, got %f", entry.Price)
	}

	if entry.Amount != 1.5 {
		t.Errorf("Expected Amount 1.5, got %f", entry.Amount)
	}
}

func TestConsumerStatistics(t *testing.T) {
	stats := &ConsumerStatistics{
		StartTime:         time.Now(),
		MessagesConsumed:  1000,
		MessagesProcessed: 950,
		MessagesFailed:    50,
		LastMessageTime:   time.Now(),
		LastMessageID:     "msg-123",
		AverageLatencyMs:  5.5,
		BooksSubscribed:   3,
		ConsumerLag: map[string]int64{
			"btc_mxn": 100,
			"eth_mxn": 50,
			"ltc_mxn": 25,
		},
	}

	if stats.MessagesConsumed != 1000 {
		t.Errorf("Expected MessagesConsumed 1000, got %d", stats.MessagesConsumed)
	}

	if stats.MessagesProcessed != 950 {
		t.Errorf("Expected MessagesProcessed 950, got %d", stats.MessagesProcessed)
	}

	if stats.MessagesFailed != 50 {
		t.Errorf("Expected MessagesFailed 50, got %d", stats.MessagesFailed)
	}

	if stats.BooksSubscribed != 3 {
		t.Errorf("Expected BooksSubscribed 3, got %d", stats.BooksSubscribed)
	}

	if len(stats.ConsumerLag) != 3 {
		t.Errorf("Expected ConsumerLag length 3, got %d", len(stats.ConsumerLag))
	}

	if stats.ConsumerLag["btc_mxn"] != 100 {
		t.Errorf("Expected btc_mxn lag 100, got %d", stats.ConsumerLag["btc_mxn"])
	}
}
