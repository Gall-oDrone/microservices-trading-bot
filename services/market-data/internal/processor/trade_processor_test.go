package processor

import (
	"context"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// TestProcessor tests the trade processor functionality
func TestProcessor(t *testing.T) {
	// Create test logger
	logger := log.New(os.Stdout, "[TEST-PROCESSOR] ", log.LstdFlags)

	// Create input channel
	tradesInput := make(chan *bitso.WebSocketTrade, 10)

	// Create processor
	config := &ProcessorConfig{
		Logger:       logger,
		TradesInput:  tradesInput,
		OutputBuffer: 5,
	}
	processor := NewProcessor(config)

	// Test processor creation
	if processor == nil {
		t.Fatal("Failed to create processor")
	}

	// Test initial statistics
	stats := processor.GetStatistics()
	if stats == nil {
		t.Fatal("Failed to get initial statistics")
	}
	if stats.TradesProcessed != 0 {
		t.Errorf("Expected 0 trades processed, got %d", stats.TradesProcessed)
	}
	if stats.BookStats == nil {
		t.Fatal("BookStats should be initialized")
	}

	// Start processor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := processor.Start(ctx); err != nil {
		t.Fatalf("Failed to start processor: %v", err)
	}

	// Test processing a valid trade
	testTrade := createTestWebSocketTrade()
	tradesInput <- testTrade

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Check statistics
	stats = processor.GetStatistics()
	if stats.TradesProcessed != 1 {
		t.Errorf("Expected 1 trade processed, got %d", stats.TradesProcessed)
	}

	// Check book statistics
	bookStats, exists := stats.BookStats["btc_mxn"]
	if !exists {
		t.Fatal("Book statistics not found for btc_mxn")
	}
	if bookStats.TradesCount != 1 {
		t.Errorf("Expected 1 trade for btc_mxn, got %d", bookStats.TradesCount)
	}
	if bookStats.LastPrice != 50000.0 {
		t.Errorf("Expected last price 50000.0, got %f", bookStats.LastPrice)
	}

	// Test processing nil trade
	tradesInput <- nil
	time.Sleep(100 * time.Millisecond)

	// Statistics should remain the same
	stats = processor.GetStatistics()
	if stats.TradesProcessed != 1 {
		t.Errorf("Expected 1 trade processed after nil trade, got %d", stats.TradesProcessed)
	}

	// Test output stream
	outputStream := processor.GetProcessedTradesStream()
	select {
	case tradeEvent := <-outputStream:
		if tradeEvent == nil {
			t.Fatal("Received nil trade event")
		}
		if tradeEvent.Book != "btc_mxn" {
			t.Errorf("Expected book btc_mxn, got %s", tradeEvent.Book)
		}
		if tradeEvent.ID != 12345 {
			t.Errorf("Expected ID 12345, got %d", tradeEvent.ID)
		}
		if tradeEvent.Price != 50000.0 {
			t.Errorf("Expected price 50000.0, got %f", tradeEvent.Price)
		}
		if tradeEvent.MakerSide != "buy" {
			t.Errorf("Expected maker side buy, got %s", tradeEvent.MakerSide)
		}
		if tradeEvent.GetTakerSide() != "sell" {
			t.Errorf("Expected taker side sell, got %s", tradeEvent.GetTakerSide())
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for trade event")
	}

	// Stop processor
	if err := processor.Stop(); err != nil {
		t.Errorf("Failed to stop processor: %v", err)
	}
}

// TestProcessorConcurrency tests concurrent processing
func TestProcessorConcurrency(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-CONCURRENCY] ", log.LstdFlags)
	tradesInput := make(chan *bitso.WebSocketTrade, 100)

	config := &ProcessorConfig{
		Logger:       logger,
		TradesInput:  tradesInput,
		OutputBuffer: 50,
	}
	processor := NewProcessor(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := processor.Start(ctx); err != nil {
		t.Fatalf("Failed to start processor: %v", err)
	}

	// Send multiple trades concurrently
	var wg sync.WaitGroup
	numTrades := 10

	for i := 0; i < numTrades; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			trade := createTestWebSocketTrade()
			trade.Payload[0].TID = uint64(10000 + id)
			trade.Payload[0].Price = bitso.Monetary{Value: 50000.0 + float64(id)}
			tradesInput <- trade
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	// Check statistics
	stats := processor.GetStatistics()
	if stats.TradesProcessed != int64(numTrades) {
		t.Errorf("Expected %d trades processed, got %d", numTrades, stats.TradesProcessed)
	}

	// Check output stream
	outputStream := processor.GetProcessedTradesStream()
	receivedCount := 0
	for i := 0; i < numTrades; i++ {
		select {
		case <-outputStream:
			receivedCount++
		case <-time.After(1 * time.Second):
			t.Fatalf("Timeout waiting for trade %d", i)
		}
	}

	if receivedCount != numTrades {
		t.Errorf("Expected %d trades in output stream, got %d", numTrades, receivedCount)
	}

	processor.Stop()
}

// TestProcessorStatistics tests statistics tracking
func TestProcessorStatistics(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-STATS] ", log.LstdFlags)
	tradesInput := make(chan *bitso.WebSocketTrade, 10)

	config := &ProcessorConfig{
		Logger:       logger,
		TradesInput:  tradesInput,
		OutputBuffer: 5,
	}
	processor := NewProcessor(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := processor.Start(ctx); err != nil {
		t.Fatalf("Failed to start processor: %v", err)
	}

	// Send trades with different books
	books := []string{"btc_mxn", "eth_mxn", "xrp_mxn"}
	for i, book := range books {
		trade := createTestWebSocketTrade()
		trade.Book = bitso.ToBook(book)
		trade.Payload[0].TID = uint64(1000 + i)
		trade.Payload[0].Price = bitso.Monetary{Value: 1000.0 + float64(i)*100}
		tradesInput <- trade
	}

	time.Sleep(100 * time.Millisecond)

	stats := processor.GetStatistics()
	if stats.TradesProcessed != 3 {
		t.Errorf("Expected 3 trades processed, got %d", stats.TradesProcessed)
	}

	// Check book-specific statistics
	for _, book := range books {
		bookStats, exists := stats.BookStats[book]
		if !exists {
			t.Errorf("Book statistics not found for %s", book)
			continue
		}
		if bookStats.TradesCount != 1 {
			t.Errorf("Expected 1 trade for %s, got %d", book, bookStats.TradesCount)
		}
	}

	processor.Stop()
}

// TestProcessorLatency tests latency calculation
func TestProcessorLatency(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-LATENCY] ", log.LstdFlags)
	tradesInput := make(chan *bitso.WebSocketTrade, 10)

	config := &ProcessorConfig{
		Logger:       logger,
		TradesInput:  tradesInput,
		OutputBuffer: 5,
	}
	processor := NewProcessor(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := processor.Start(ctx); err != nil {
		t.Fatalf("Failed to start processor: %v", err)
	}

	// Create trade with specific timestamp
	trade := createTestWebSocketTrade()
	// Set creation timestamp to 100ms ago
	trade.Payload[0].CreationTimestamp = uint64(time.Now().Add(-100 * time.Millisecond).UnixMilli())
	tradesInput <- trade

	time.Sleep(100 * time.Millisecond)

	// Check output stream for latency
	outputStream := processor.GetProcessedTradesStream()
	select {
	case tradeEvent := <-outputStream:
		latency := tradeEvent.GetLatencyMs()
		if latency < 50 || latency > 200 {
			t.Errorf("Expected latency around 100ms, got %dms", latency)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for trade event")
	}

	processor.Stop()
}

// TestProcessorErrorHandling tests error handling
func TestProcessorErrorHandling(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-ERRORS] ", log.LstdFlags)
	tradesInput := make(chan *bitso.WebSocketTrade, 10)

	config := &ProcessorConfig{
		Logger:       logger,
		TradesInput:  tradesInput,
		OutputBuffer: 5,
	}
	processor := NewProcessor(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := processor.Start(ctx); err != nil {
		t.Fatalf("Failed to start processor: %v", err)
	}

	// Test invalid trade (empty payload)
	invalidTrade := &bitso.WebSocketTrade{
		Book:    bitso.ToBook("btc_mxn"),
		Payload: []bitso.WebSocketTradePayload{}, // Empty payload
	}
	tradesInput <- invalidTrade

	time.Sleep(100 * time.Millisecond)

	// Should not crash and should increment dropped counter
	stats := processor.GetStatistics()
	if stats.TradesDropped == 0 {
		t.Error("Expected dropped counter to be incremented for invalid trade")
	}

	processor.Stop()
}

// BenchmarkProcessor benchmarks the processor performance
func BenchmarkProcessor(b *testing.B) {
	logger := log.New(os.Stdout, "[BENCH-PROCESSOR] ", log.LstdFlags)
	tradesInput := make(chan *bitso.WebSocketTrade, 1000)

	config := &ProcessorConfig{
		Logger:       logger,
		TradesInput:  tradesInput,
		OutputBuffer: 1000,
	}
	processor := NewProcessor(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	processor.Start(ctx)

	// Start goroutine to consume output
	outputStream := processor.GetProcessedTradesStream()
	go func() {
		for range outputStream {
			// Consume all output
		}
	}()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			trade := createTestWebSocketTrade()
			trade.Payload[0].TID = uint64(time.Now().UnixNano())
			select {
			case tradesInput <- trade:
			default:
				// Channel full, skip
			}
		}
	})

	processor.Stop()
}

// Helper function to create a test WebSocket trade
func createTestWebSocketTrade() *bitso.WebSocketTrade {
	return &bitso.WebSocketTrade{
		Book: bitso.ToBook("btc_mxn"),
		Payload: []bitso.WebSocketTradePayload{
			{
				TID:               12345,
				Price:             bitso.Monetary{Value: 50000.0},
				Amount:            bitso.Monetary{Value: 0.001},
				Value:             bitso.Monetary{Value: 50.0},
				MakerOrderID:      "maker-order-123",
				TakerOrderID:      "taker-order-456",
				MakerSide:         "0", // 0 = buy, 1 = sell
				CreationTimestamp: uint64(time.Now().UnixMilli()),
			},
		},
		Sent: uint64(time.Now().UnixMilli()),
	}
}
