package processor

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// OrderBookProcessorInterface processes incoming order book messages
type OrderBookProcessorInterface interface {
	Start(ctx context.Context) error
	Stop() error
	ProcessOrderBook(orderBook *bitso.OrderBook) error
	ProcessDiffOrder(diffOrder *bitso.WebSocketDiffOrder) error
	GetOrderBookStream() <-chan *bitso.OrderBook
	GetStatistics() *OrderBookProcessorStatistics
}

// OrderBookProcessorStatistics tracks order book processor performance
type OrderBookProcessorStatistics struct {
	StartTime           time.Time
	OrderBooksProcessed int64
	DiffOrdersProcessed int64
	OrderBooksDropped   int64
	LastOrderBookTime   time.Time
	LastOrderBookID     string
	AverageLatencyMs    float64

	// Book-specific statistics
	BookStats map[string]*OrderBookBookStatistics
}

// OrderBookBookStatistics tracks statistics per trading book
type OrderBookBookStatistics struct {
	OrderBooksCount int64
	DiffOrdersCount int64
	LastBidPrice    float64
	LastAskPrice    float64
	LastSpread      float64
	LastBidSize     float64
	LastAskSize     float64
	MaxDepth        int
}

// OrderBookProcessorConfig holds configuration for the order book processor
type OrderBookProcessorConfig struct {
	Logger            *log.Logger
	OrderBooksInput   <-chan *bitso.OrderBook
	DiffOrdersInput   <-chan *bitso.WebSocketDiffOrder
	OutputBuffer      int
	MaxOrderBookDepth int
}

// OrderBookProcessor implements OrderBookProcessorInterface
type OrderBookProcessor struct {
	logger *log.Logger

	// Input streams
	orderBooksInput <-chan *bitso.OrderBook
	diffOrdersInput <-chan *bitso.WebSocketDiffOrder

	// Output stream
	orderBooksOutput chan *bitso.OrderBook

	// Configuration
	maxOrderBookDepth int

	// Statistics
	stats      *OrderBookProcessorStatistics
	statsMutex sync.RWMutex

	// State management
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewOrderBookProcessor creates a new order book processor
func NewOrderBookProcessor(config *OrderBookProcessorConfig) *OrderBookProcessor {
	logger := config.Logger
	if logger == nil {
		logger = log.New(log.Writer(), "[ORDERBOOK-PROCESSOR] ", log.LstdFlags|log.Lshortfile)
	}

	outputBuffer := config.OutputBuffer
	if outputBuffer == 0 {
		outputBuffer = 100
	}

	maxDepth := config.MaxOrderBookDepth
	if maxDepth == 0 {
		maxDepth = 50
	}

	return &OrderBookProcessor{
		logger:            logger,
		orderBooksInput:   config.OrderBooksInput,
		diffOrdersInput:   config.DiffOrdersInput,
		orderBooksOutput:  make(chan *bitso.OrderBook, outputBuffer),
		maxOrderBookDepth: maxDepth,
		stats: &OrderBookProcessorStatistics{
			StartTime: time.Now(),
			BookStats: make(map[string]*OrderBookBookStatistics),
		},
		stopChan: make(chan struct{}),
	}
}

// Start begins processing order books
func (p *OrderBookProcessor) Start(ctx context.Context) error {
	p.logger.Println("Starting order book processor...")

	p.wg.Add(1)
	go p.orderBookProcessingLoop(ctx)

	p.wg.Add(1)
	go p.diffOrderProcessingLoop(ctx)

	// Start statistics reporter
	p.wg.Add(1)
	go p.statsReporter(ctx)

	p.logger.Println("✓ Order book processor started")
	return nil
}

// Stop gracefully stops the processor
func (p *OrderBookProcessor) Stop() error {
	p.logger.Println("Stopping order book processor...")

	close(p.stopChan)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Println("Order book processor stopped")
	case <-time.After(5 * time.Second):
		p.logger.Println("Warning: Order book processor stop timeout")
	}

	close(p.orderBooksOutput)
	return nil
}

// orderBookProcessingLoop continuously processes incoming order books
func (p *OrderBookProcessor) orderBookProcessingLoop(ctx context.Context) {
	defer p.wg.Done()
	p.logger.Println("Order book processing loop started")

	for {
		select {
		case <-p.stopChan:
			p.logger.Println("Order book processing loop stopping")
			return

		case <-ctx.Done():
			p.logger.Println("Context cancelled, order book processing loop stopping")
			return

		case orderBook, ok := <-p.orderBooksInput:
			if !ok {
				p.logger.Println("Order book input channel closed")
				return
			}

			if err := p.ProcessOrderBook(orderBook); err != nil {
				p.logger.Printf("Error processing order book: %v", err)
			}
		}
	}
}

// diffOrderProcessingLoop continuously processes incoming diff orders
func (p *OrderBookProcessor) diffOrderProcessingLoop(ctx context.Context) {
	defer p.wg.Done()
	p.logger.Println("Diff order processing loop started")

	for {
		select {
		case <-p.stopChan:
			p.logger.Println("Diff order processing loop stopping")
			return

		case <-ctx.Done():
			p.logger.Println("Context cancelled, diff order processing loop stopping")
			return

		case diffOrder, ok := <-p.diffOrdersInput:
			if !ok {
				p.logger.Println("Diff order input channel closed")
				return
			}

			if err := p.ProcessDiffOrder(diffOrder); err != nil {
				p.logger.Printf("Error processing diff order: %v", err)
			}
		}
	}
}

// ProcessOrderBook processes a single order book
func (p *OrderBookProcessor) ProcessOrderBook(orderBook *bitso.OrderBook) error {
	if orderBook == nil {
		return nil
	}

	// Validate order book
	if err := p.validateOrderBook(orderBook); err != nil {
		p.logger.Printf("Invalid order book: %v", err)
		p.incrementDropped()
		return nil
	}

	// Limit order book depth
	p.limitOrderBookDepth(orderBook)

	// Update statistics
	p.updateStats(orderBook, nil)

	// Send to output channel (non-blocking)
	select {
	case p.orderBooksOutput <- orderBook:
		p.logger.Printf("Processed order book: %s Bids=%d Asks=%d",
			orderBook.Book.String(), len(orderBook.Bids), len(orderBook.Asks))

	case <-time.After(1 * time.Second):
		p.logger.Println("Warning: Order book output channel full, dropping order book")
		p.incrementDropped()
	}

	return nil
}

// ProcessDiffOrder processes a single diff order
func (p *OrderBookProcessor) ProcessDiffOrder(diffOrder *bitso.WebSocketDiffOrder) error {
	if diffOrder == nil {
		return nil
	}

	// Validate diff order
	if err := p.validateDiffOrder(diffOrder); err != nil {
		p.logger.Printf("Invalid diff order: %v", err)
		p.incrementDropped()
		return nil
	}

	// Update statistics
	p.updateStats(nil, diffOrder)

	p.logger.Printf("Processed diff order: %s Payload=%d",
		diffOrder.Book.String(), len(diffOrder.Payload))

	return nil
}

// GetOrderBookStream returns the output channel for processed order books
func (p *OrderBookProcessor) GetOrderBookStream() <-chan *bitso.OrderBook {
	return p.orderBooksOutput
}

// GetStatistics returns current processor statistics
func (p *OrderBookProcessor) GetStatistics() *OrderBookProcessorStatistics {
	p.statsMutex.RLock()
	defer p.statsMutex.RUnlock()

	// Create a copy to avoid race conditions
	statsCopy := *p.stats
	statsCopy.BookStats = make(map[string]*OrderBookBookStatistics)
	for book, bookStats := range p.stats.BookStats {
		bookStatsCopy := *bookStats
		statsCopy.BookStats[book] = &bookStatsCopy
	}

	return &statsCopy
}

// validateOrderBook validates an order book
func (p *OrderBookProcessor) validateOrderBook(orderBook *bitso.OrderBook) error {
	if orderBook.Book == nil {
		return fmt.Errorf("order book book is nil")
	}

	if len(orderBook.Bids) == 0 && len(orderBook.Asks) == 0 {
		return fmt.Errorf("order book has no bids or asks")
	}

	// Validate bid prices (should be in descending order)
	for i := 1; i < len(orderBook.Bids); i++ {
		if orderBook.Bids[i-1].Price < orderBook.Bids[i].Price {
			return fmt.Errorf("bid prices not in descending order")
		}
	}

	// Validate ask prices (should be in ascending order)
	for i := 1; i < len(orderBook.Asks); i++ {
		if orderBook.Asks[i-1].Price > orderBook.Asks[i].Price {
			return fmt.Errorf("ask prices not in ascending order")
		}
	}

	return nil
}

// validateDiffOrder validates a diff order
func (p *OrderBookProcessor) validateDiffOrder(diffOrder *bitso.WebSocketDiffOrder) error {
	if diffOrder.Book == nil {
		return fmt.Errorf("diff order book is nil")
	}

	if len(diffOrder.Payload) == 0 {
		return fmt.Errorf("diff order has no payload")
	}

	return nil
}

// limitOrderBookDepth limits the depth of an order book
func (p *OrderBookProcessor) limitOrderBookDepth(orderBook *bitso.OrderBook) {
	if len(orderBook.Bids) > p.maxOrderBookDepth {
		orderBook.Bids = orderBook.Bids[:p.maxOrderBookDepth]
	}

	if len(orderBook.Asks) > p.maxOrderBookDepth {
		orderBook.Asks = orderBook.Asks[:p.maxOrderBookDepth]
	}
}

// updateStats updates processor statistics
func (p *OrderBookProcessor) updateStats(orderBook *bitso.OrderBook, diffOrder *bitso.WebSocketDiffOrder) {
	p.statsMutex.Lock()
	defer p.statsMutex.Unlock()

	if orderBook != nil {
		p.stats.OrderBooksProcessed++
		p.stats.LastOrderBookTime = time.Now()

		// Update book-specific statistics
		bookStr := orderBook.Book.String()
		bookStats, exists := p.stats.BookStats[bookStr]
		if !exists {
			bookStats = &OrderBookBookStatistics{}
			p.stats.BookStats[bookStr] = bookStats
		}

		bookStats.OrderBooksCount++

		// Update bid/ask information
		if len(orderBook.Bids) > 0 {
			bookStats.LastBidPrice = orderBook.Bids[0].Price
			bookStats.LastBidSize = orderBook.Bids[0].Amount
		}

		if len(orderBook.Asks) > 0 {
			bookStats.LastAskPrice = orderBook.Asks[0].Price
			bookStats.LastAskSize = orderBook.Asks[0].Amount
		}

		// Calculate spread
		if bookStats.LastBidPrice > 0 && bookStats.LastAskPrice > 0 {
			bookStats.LastSpread = bookStats.LastAskPrice - bookStats.LastBidPrice
		}

		// Update max depth
		totalDepth := len(orderBook.Bids) + len(orderBook.Asks)
		if totalDepth > bookStats.MaxDepth {
			bookStats.MaxDepth = totalDepth
		}
	}

	if diffOrder != nil {
		p.stats.DiffOrdersProcessed++

		// Update book-specific statistics
		bookStr := diffOrder.Book.String()
		bookStats, exists := p.stats.BookStats[bookStr]
		if !exists {
			bookStats = &OrderBookBookStatistics{}
			p.stats.BookStats[bookStr] = bookStats
		}

		bookStats.DiffOrdersCount++
	}
}

// incrementDropped increments the dropped counter
func (p *OrderBookProcessor) incrementDropped() {
	p.statsMutex.Lock()
	defer p.statsMutex.Unlock()
	p.stats.OrderBooksDropped++
}

// statsReporter periodically logs statistics
func (p *OrderBookProcessor) statsReporter(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			p.logStatistics()
			return

		case <-ctx.Done():
			return

		case <-ticker.C:
			p.logStatistics()
		}
	}
}

// logStatistics logs current statistics
func (p *OrderBookProcessor) logStatistics() {
	stats := p.GetStatistics()

	uptime := time.Since(stats.StartTime).Round(time.Second)

	p.logger.Println("=== Order Book Processor Statistics ===")
	p.logger.Printf("Uptime: %v", uptime)
	p.logger.Printf("Order Books Processed: %d", stats.OrderBooksProcessed)
	p.logger.Printf("Diff Orders Processed: %d", stats.DiffOrdersProcessed)
	p.logger.Printf("Order Books Dropped: %d", stats.OrderBooksDropped)

	if !stats.LastOrderBookTime.IsZero() {
		p.logger.Printf("Last Order Book: %v ago",
			time.Since(stats.LastOrderBookTime).Round(time.Second))
	}

	// Log book statistics
	for book, bookStats := range stats.BookStats {
		p.logger.Printf("  %s: OrderBooks=%d, DiffOrders=%d, LastBid=%.2f, LastAsk=%.2f, Spread=%.2f",
			book, bookStats.OrderBooksCount, bookStats.DiffOrdersCount,
			bookStats.LastBidPrice, bookStats.LastAskPrice, bookStats.LastSpread)
	}
}
