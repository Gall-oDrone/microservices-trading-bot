package processor

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/strategy-executor/internal/consumer"
	"bitso-trading-platform/strategy-executor/internal/logger"
	"bitso-trading-platform/strategy-executor/internal/metrics"
)

func TestBookFilter(t *testing.T) {
	books := []string{"btc_mxn", "eth_mxn"}
	filter := NewBookFilter("test-book-filter", books)

	if filter.GetName() != "test-book-filter" {
		t.Errorf("Expected name 'test-book-filter', got '%s'", filter.GetName())
	}

	// Test allowed book
	event := &ProcessedEvent{
		Type: EventTypeTrade,
		Book: "btc_mxn",
	}

	if !filter.ShouldProcess(event) {
		t.Error("Expected btc_mxn to be allowed")
	}

	// Test disallowed book
	event.Book = "ltc_mxn"
	if filter.ShouldProcess(event) {
		t.Error("Expected ltc_mxn to be disallowed")
	}

	// Test adding a book
	filter.AddBook("ltc_mxn")
	if !filter.ShouldProcess(event) {
		t.Error("Expected ltc_mxn to be allowed after adding")
	}

	// Test removing a book
	filter.RemoveBook("btc_mxn")
	event.Book = "btc_mxn"
	if filter.ShouldProcess(event) {
		t.Error("Expected btc_mxn to be disallowed after removing")
	}
}

func TestTypeFilter(t *testing.T) {
	types := []EventType{EventTypeTrade, EventTypeTicker}
	filter := NewTypeFilter("test-type-filter", types)

	if filter.GetName() != "test-type-filter" {
		t.Errorf("Expected name 'test-type-filter', got '%s'", filter.GetName())
	}

	// Test allowed type
	event := &ProcessedEvent{
		Type: EventTypeTrade,
		Book: "btc_mxn",
	}

	if !filter.ShouldProcess(event) {
		t.Error("Expected EventTypeTrade to be allowed")
	}

	// Test disallowed type
	event.Type = EventTypeOrderBook
	if filter.ShouldProcess(event) {
		t.Error("Expected EventTypeOrderBook to be disallowed")
	}

	// Test adding a type
	filter.AddType(EventTypeOrderBook)
	if !filter.ShouldProcess(event) {
		t.Error("Expected EventTypeOrderBook to be allowed after adding")
	}

	// Test removing a type
	filter.RemoveType(EventTypeTrade)
	event.Type = EventTypeTrade
	if filter.ShouldProcess(event) {
		t.Error("Expected EventTypeTrade to be disallowed after removing")
	}
}

func TestTimeFilter(t *testing.T) {
	startTime := time.Now().Add(-1 * time.Hour)
	endTime := time.Now().Add(1 * time.Hour)
	filter := NewTimeFilter("test-time-filter", startTime, endTime)

	if filter.GetName() != "test-time-filter" {
		t.Errorf("Expected name 'test-time-filter', got '%s'", filter.GetName())
	}

	// Test event within time range
	event := &ProcessedEvent{
		Type:      EventTypeTrade,
		Book:      "btc_mxn",
		Timestamp: time.Now(),
	}

	if !filter.ShouldProcess(event) {
		t.Error("Expected event within time range to be allowed")
	}

	// Test event before time range
	event.Timestamp = time.Now().Add(-2 * time.Hour)
	if filter.ShouldProcess(event) {
		t.Error("Expected event before time range to be disallowed")
	}

	// Test event after time range
	event.Timestamp = time.Now().Add(2 * time.Hour)
	if filter.ShouldProcess(event) {
		t.Error("Expected event after time range to be disallowed")
	}

	// Test updating time range
	newStartTime := time.Now().Add(-2 * time.Hour)
	newEndTime := time.Now().Add(2 * time.Hour)
	filter.SetTimeRange(newStartTime, newEndTime)

	event.Timestamp = time.Now().Add(-90 * time.Minute)
	if !filter.ShouldProcess(event) {
		t.Error("Expected event within new time range to be allowed")
	}
}

func TestRateLimitFilter(t *testing.T) {
	filter := NewRateLimitFilter("test-rate-limit", 2, 1*time.Minute)

	if filter.GetName() != "test-rate-limit" {
		t.Errorf("Expected name 'test-rate-limit', got '%s'", filter.GetName())
	}

	// Test events within rate limit
	event := &ProcessedEvent{
		Type: EventTypeTrade,
		Book: "btc_mxn",
	}

	// First event should be allowed
	if !filter.ShouldProcess(event) {
		t.Error("Expected first event to be allowed")
	}

	// Second event should be allowed
	if !filter.ShouldProcess(event) {
		t.Error("Expected second event to be allowed")
	}

	// Third event should be disallowed (rate limit exceeded)
	if filter.ShouldProcess(event) {
		t.Error("Expected third event to be disallowed due to rate limit")
	}

	// Test different book (should have separate rate limit)
	event.Book = "eth_mxn"
	if !filter.ShouldProcess(event) {
		t.Error("Expected event for different book to be allowed")
	}

	// Test updating rate limit
	filter.SetRateLimit(5, 1*time.Minute)
	event.Book = "btc_mxn"
	if !filter.ShouldProcess(event) {
		t.Error("Expected event to be allowed after increasing rate limit")
	}
}

func TestCompositeFilter(t *testing.T) {
	bookFilter := NewBookFilter("book-filter", []string{"btc_mxn"})
	typeFilter := NewTypeFilter("type-filter", []EventType{EventTypeTrade})

	filter := NewCompositeFilter("composite-filter", bookFilter, typeFilter)

	if filter.GetName() != "composite-filter" {
		t.Errorf("Expected name 'composite-filter', got '%s'", filter.GetName())
	}

	// Test event that passes all filters
	event := &ProcessedEvent{
		Type: EventTypeTrade,
		Book: "btc_mxn",
	}

	if !filter.ShouldProcess(event) {
		t.Error("Expected event that passes all filters to be allowed")
	}

	// Test event that fails book filter
	event.Book = "eth_mxn"
	if filter.ShouldProcess(event) {
		t.Error("Expected event that fails book filter to be disallowed")
	}

	// Test event that fails type filter
	event.Book = "btc_mxn"
	event.Type = EventTypeTicker
	if filter.ShouldProcess(event) {
		t.Error("Expected event that fails type filter to be disallowed")
	}

	// Test adding a filter
	timeFilter := NewTimeFilter("time-filter", time.Now().Add(-1*time.Hour), time.Now().Add(1*time.Hour))
	filter.AddFilter(timeFilter)

	event.Type = EventTypeTrade
	event.Timestamp = time.Now()
	if !filter.ShouldProcess(event) {
		t.Error("Expected event that passes all filters including new time filter to be allowed")
	}

	// Test removing a filter
	filter.RemoveFilter("book-filter")
	event.Book = "eth_mxn" // This should now be allowed since book filter was removed
	if !filter.ShouldProcess(event) {
		t.Error("Expected event to be allowed after removing book filter")
	}
}

func TestVolumeFilter(t *testing.T) {
	filter := NewVolumeFilter("test-volume-filter", 1.0, 10.0, "amount")

	if filter.GetName() != "test-volume-filter" {
		t.Errorf("Expected name 'test-volume-filter', got '%s'", filter.GetName())
	}

	// Test event with volume within range
	event := &ProcessedEvent{
		Type: EventTypeTrade,
		Book: "btc_mxn",
		Metadata: map[string]interface{}{
			"amount": 5.0,
		},
	}

	if !filter.ShouldProcess(event) {
		t.Error("Expected event with volume within range to be allowed")
	}

	// Test event with volume below range
	event.Metadata["amount"] = 0.5
	if filter.ShouldProcess(event) {
		t.Error("Expected event with volume below range to be disallowed")
	}

	// Test event with volume above range
	event.Metadata["amount"] = 15.0
	if filter.ShouldProcess(event) {
		t.Error("Expected event with volume above range to be disallowed")
	}

	// Test event without volume field (should be allowed)
	delete(event.Metadata, "amount")
	if !filter.ShouldProcess(event) {
		t.Error("Expected event without volume field to be allowed")
	}

	// Test updating volume threshold
	filter.SetVolumeThreshold(0.1, 20.0)
	event.Metadata["amount"] = 15.0
	if !filter.ShouldProcess(event) {
		t.Error("Expected event with volume within new range to be allowed")
	}
}

func TestFilterIntegration(t *testing.T) {
	// Create a processor with filters
	tradeEvents := make(chan *models.TradeEvent, 1)
	tickerEvents := make(chan *consumer.TickerEvent, 1)
	orderBookEvents := make(chan *consumer.OrderBookEvent, 1)
	logger := logger.NewDefault()
	metrics := metrics.New("test")

	processor := NewProcessor(tradeEvents, tickerEvents, orderBookEvents, logger, metrics)

	// Add filters
	bookFilter := NewBookFilter("book-filter", []string{"btc_mxn"})
	typeFilter := NewTypeFilter("type-filter", []EventType{EventTypeTrade})
	processor.AddEventFilter(bookFilter)
	processor.AddEventFilter(typeFilter)

	// Test event that passes all filters
	tradeEvent := &models.TradeEvent{
		ID:        12345,
		Book:      "btc_mxn",
		Price:     1000.0,
		Amount:    0.5,
		Value:     500.0,
		Timestamp: time.Now(),
		Source:    "test",
	}

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
	case <-time.After(1 * time.Second):
		t.Error("Expected processed event, but none received")
	}

	// Test event that fails filters
	tradeEvent.Book = "eth_mxn" // This should be filtered out
	err = processor.ProcessTradeEvent(tradeEvent)
	if err != nil {
		t.Errorf("ProcessTradeEvent() unexpected error: %v", err)
	}

	// Check if event was filtered out
	select {
	case <-processor.GetProcessedEvents():
		t.Error("Expected event to be filtered out, but it was processed")
	case <-time.After(100 * time.Millisecond):
		// Event was filtered out, which is expected
	}
}
