package processor

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/strategy-executor/internal/consumer"
	"bitso-trading-platform/strategy-executor/internal/logger"
	"bitso-trading-platform/strategy-executor/internal/metrics"
)

func TestNewProcessor(t *testing.T) {
	tradeEvents := make(chan *models.TradeEvent, 1)
	tickerEvents := make(chan *consumer.TickerEvent, 1)
	orderBookEvents := make(chan *consumer.OrderBookEvent, 1)
	logger := logger.NewDefault()
	metrics := metrics.New("test")

	processor := NewProcessor(tradeEvents, tickerEvents, orderBookEvents, logger, metrics)

	if processor == nil {
		t.Fatal("NewProcessor() returned nil")
	}

	if processor.tradeEvents != tradeEvents {
		t.Error("Expected tradeEvents channel to match input")
	}

	if processor.tickerEvents != tickerEvents {
		t.Error("Expected tickerEvents channel to match input")
	}

	if processor.orderBookEvents != orderBookEvents {
		t.Error("Expected orderBookEvents channel to match input")
	}

	if processor.logger != logger {
		t.Error("Expected logger to match input")
	}

	if processor.metrics != metrics {
		t.Error("Expected metrics to match input")
	}
}

func TestProcessor_ProcessTradeEvent(t *testing.T) {
	tradeEvents := make(chan *models.TradeEvent, 1)
	tickerEvents := make(chan *consumer.TickerEvent, 1)
	orderBookEvents := make(chan *consumer.OrderBookEvent, 1)
	logger := logger.NewDefault()
	metrics := metrics.New("test")

	processor := NewProcessor(tradeEvents, tickerEvents, orderBookEvents, logger, metrics)

	// Create a test trade event
	tradeEvent := &models.TradeEvent{
		ID:           12345,
		Book:         "btc_mxn",
		Price:        1000.0,
		Amount:       0.5,
		Value:        500.0,
		MakerOrderID: "maker-123",
		TakerOrderID: "taker-456",
		MakerSide:    "buy",
		Timestamp:    time.Now(),
		CreatedAt:    uint64(time.Now().UnixMilli()),
		ReceivedAt:   time.Now(),
		Source:       "test",
		Metadata:     map[string]interface{}{"test": true},
	}

	// Process the event
	err := processor.ProcessTradeEvent(tradeEvent)
	if err != nil {
		t.Errorf("ProcessTradeEvent() unexpected error: %v", err)
	}

	// Check if event was processed
	select {
	case processedEvent := <-processor.GetProcessedEvents():
		if processedEvent.Type != EventTypeTrade {
			t.Errorf("Expected event type %s, got %s", EventTypeTrade, processedEvent.Type)
		}
		if processedEvent.Book != "btc_mxn" {
			t.Errorf("Expected book 'btc_mxn', got '%s'", processedEvent.Book)
		}
		if processedEvent.Data != tradeEvent {
			t.Error("Expected data to match original trade event")
		}
	case <-time.After(1 * time.Second):
		t.Error("Expected processed event, but none received")
	}
}

func TestProcessor_ProcessTickerEvent(t *testing.T) {
	tradeEvents := make(chan *models.TradeEvent, 1)
	tickerEvents := make(chan *consumer.TickerEvent, 1)
	orderBookEvents := make(chan *consumer.OrderBookEvent, 1)
	logger := logger.NewDefault()
	metrics := metrics.New("test")

	processor := NewProcessor(tradeEvents, tickerEvents, orderBookEvents, logger, metrics)

	// Create a test ticker event
	tickerEvent := &consumer.TickerEvent{
		Book:      "btc_mxn",
		Bid:       1000.0,
		Ask:       1001.0,
		Last:      1000.5,
		Volume:    10.5,
		Timestamp: time.Now(),
	}

	// Process the event
	err := processor.ProcessTickerEvent(tickerEvent)
	if err != nil {
		t.Errorf("ProcessTickerEvent() unexpected error: %v", err)
	}

	// Check if event was processed
	select {
	case processedEvent := <-processor.GetProcessedEvents():
		if processedEvent.Type != EventTypeTicker {
			t.Errorf("Expected event type %s, got %s", EventTypeTicker, processedEvent.Type)
		}
		if processedEvent.Book != "btc_mxn" {
			t.Errorf("Expected book 'btc_mxn', got '%s'", processedEvent.Book)
		}
		if processedEvent.Data != tickerEvent {
			t.Error("Expected data to match original ticker event")
		}
	case <-time.After(1 * time.Second):
		t.Error("Expected processed event, but none received")
	}
}

func TestProcessor_ProcessOrderBookEvent(t *testing.T) {
	tradeEvents := make(chan *models.TradeEvent, 1)
	tickerEvents := make(chan *consumer.TickerEvent, 1)
	orderBookEvents := make(chan *consumer.OrderBookEvent, 1)
	logger := logger.NewDefault()
	metrics := metrics.New("test")

	processor := NewProcessor(tradeEvents, tickerEvents, orderBookEvents, logger, metrics)

	// Create a test order book event
	orderBookEvent := &consumer.OrderBookEvent{
		Book: "btc_mxn",
		Bids: []consumer.OrderBookEntry{
			{Price: 1000.0, Amount: 1.0},
			{Price: 999.0, Amount: 2.0},
		},
		Asks: []consumer.OrderBookEntry{
			{Price: 1001.0, Amount: 1.5},
			{Price: 1002.0, Amount: 2.5},
		},
		Timestamp: time.Now(),
		Metadata:  map[string]interface{}{"source": "test"},
	}

	// Process the event
	err := processor.ProcessOrderBookEvent(orderBookEvent)
	if err != nil {
		t.Errorf("ProcessOrderBookEvent() unexpected error: %v", err)
	}

	// Check if event was processed
	select {
	case processedEvent := <-processor.GetProcessedEvents():
		if processedEvent.Type != EventTypeOrderBook {
			t.Errorf("Expected event type %s, got %s", EventTypeOrderBook, processedEvent.Type)
		}
		if processedEvent.Book != "btc_mxn" {
			t.Errorf("Expected book 'btc_mxn', got '%s'", processedEvent.Book)
		}
		if processedEvent.Data != orderBookEvent {
			t.Error("Expected data to match original order book event")
		}
	case <-time.After(1 * time.Second):
		t.Error("Expected processed event, but none received")
	}
}

func TestProcessor_GetStatistics(t *testing.T) {
	tradeEvents := make(chan *models.TradeEvent, 1)
	tickerEvents := make(chan *consumer.TickerEvent, 1)
	orderBookEvents := make(chan *consumer.OrderBookEvent, 1)
	logger := logger.NewDefault()
	metrics := metrics.New("test")

	processor := NewProcessor(tradeEvents, tickerEvents, orderBookEvents, logger, metrics)

	// Process some events to generate statistics
	tradeEvent := &models.TradeEvent{
		ID:        12345,
		Book:      "btc_mxn",
		Price:     1000.0,
		Amount:    0.5,
		Value:     500.0,
		Timestamp: time.Now(),
		Source:    "test",
	}

	processor.ProcessTradeEvent(tradeEvent)

	// Get statistics
	stats := processor.GetStatistics()

	if stats == nil {
		t.Fatal("GetStatistics() returned nil")
	}

	if stats.EventsProcessed != 1 {
		t.Errorf("Expected EventsProcessed 1, got %d", stats.EventsProcessed)
	}

	if stats.EventsByType[EventTypeTrade] != 1 {
		t.Errorf("Expected EventsByType[EventTypeTrade] 1, got %d", stats.EventsByType[EventTypeTrade])
	}

	if stats.EventsByBook["btc_mxn"] != 1 {
		t.Errorf("Expected EventsByBook['btc_mxn'] 1, got %d", stats.EventsByBook["btc_mxn"])
	}
}

func TestProcessor_Stop(t *testing.T) {
	tradeEvents := make(chan *models.TradeEvent, 1)
	tickerEvents := make(chan *consumer.TickerEvent, 1)
	orderBookEvents := make(chan *consumer.OrderBookEvent, 1)
	logger := logger.NewDefault()
	metrics := metrics.New("test")

	processor := NewProcessor(tradeEvents, tickerEvents, orderBookEvents, logger, metrics)

	// Test stopping the processor
	err := processor.Stop()
	if err != nil {
		t.Errorf("Stop() unexpected error: %v", err)
	}

	// Check if stop channel is closed
	select {
	case <-processor.stopChan:
		// Channel is closed, which is expected
	default:
		t.Error("Expected stop channel to be closed")
	}
}

func TestProcessedEvent(t *testing.T) {
	event := &ProcessedEvent{
		Type:      EventTypeTrade,
		Book:      "btc_mxn",
		Data:      "test data",
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"test": "value",
		},
	}

	if event.Type != EventTypeTrade {
		t.Errorf("Expected Type %s, got %s", EventTypeTrade, event.Type)
	}

	if event.Book != "btc_mxn" {
		t.Errorf("Expected Book 'btc_mxn', got '%s'", event.Book)
	}

	if event.Data != "test data" {
		t.Errorf("Expected Data 'test data', got %v", event.Data)
	}

	if event.Metadata["test"] != "value" {
		t.Errorf("Expected Metadata['test'] 'value', got %v", event.Metadata["test"])
	}
}

func TestEventType(t *testing.T) {
	if EventTypeTrade != "trade" {
		t.Errorf("Expected EventTypeTrade 'trade', got '%s'", EventTypeTrade)
	}

	if EventTypeTicker != "ticker" {
		t.Errorf("Expected EventTypeTicker 'ticker', got '%s'", EventTypeTicker)
	}

	if EventTypeOrderBook != "orderbook" {
		t.Errorf("Expected EventTypeOrderBook 'orderbook', got '%s'", EventTypeOrderBook)
	}
}

func TestProcessorStatistics(t *testing.T) {
	stats := &ProcessorStatistics{
		StartTime:        time.Now(),
		EventsProcessed:  1000,
		EventsFiltered:   100,
		EventsFailed:     50,
		LastEventTime:    time.Now(),
		LastEventType:    EventTypeTrade,
		AverageLatencyMs: 5.5,
		EventsByType: map[EventType]int64{
			EventTypeTrade:     500,
			EventTypeTicker:    300,
			EventTypeOrderBook: 200,
		},
		EventsByBook: map[string]int64{
			"btc_mxn": 600,
			"eth_mxn": 300,
			"ltc_mxn": 100,
		},
	}

	if stats.EventsProcessed != 1000 {
		t.Errorf("Expected EventsProcessed 1000, got %d", stats.EventsProcessed)
	}

	if stats.EventsFiltered != 100 {
		t.Errorf("Expected EventsFiltered 100, got %d", stats.EventsFiltered)
	}

	if stats.EventsFailed != 50 {
		t.Errorf("Expected EventsFailed 50, got %d", stats.EventsFailed)
	}

	if stats.LastEventType != EventTypeTrade {
		t.Errorf("Expected LastEventType %s, got %s", EventTypeTrade, stats.LastEventType)
	}

	if len(stats.EventsByType) != 3 {
		t.Errorf("Expected EventsByType length 3, got %d", len(stats.EventsByType))
	}

	if len(stats.EventsByBook) != 3 {
		t.Errorf("Expected EventsByBook length 3, got %d", len(stats.EventsByBook))
	}
}
