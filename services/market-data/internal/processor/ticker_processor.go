package processor

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// TickerProcessorInterface processes incoming ticker messages
type TickerProcessorInterface interface {
	Start(ctx context.Context) error
	Stop() error
	ProcessTicker(ticker *bitso.Ticker) error
	GetTickerStream() <-chan *bitso.Ticker
	GetStatistics() *TickerProcessorStatistics
}

// TickerProcessorStatistics tracks ticker processor performance
type TickerProcessorStatistics struct {
	StartTime        time.Time
	TickersProcessed int64
	TickersDropped   int64
	LastTickerTime   time.Time
	LastTickerID     string
	AverageLatencyMs float64

	// Book-specific statistics
	BookStats map[string]*TickerBookStatistics
}

// TickerBookStatistics tracks statistics per trading book
type TickerBookStatistics struct {
	TickersCount       int64
	LastPrice          float64
	LastVolume         float64
	LastHigh           float64
	LastLow            float64
	LastVWAP           float64
	PriceChange        float64
	PriceChangePercent float64
	LastUpdateTime     time.Time
}

// TickerProcessorConfig holds configuration for the ticker processor
type TickerProcessorConfig struct {
	Logger       *log.Logger
	TickersInput <-chan *bitso.Ticker
	OutputBuffer int
}

// TickerProcessor implements TickerProcessorInterface
type TickerProcessor struct {
	logger *log.Logger

	// Input stream
	tickersInput <-chan *bitso.Ticker

	// Output stream
	tickersOutput chan *bitso.Ticker

	// Statistics
	stats      *TickerProcessorStatistics
	statsMutex sync.RWMutex

	// State management
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewTickerProcessor creates a new ticker processor
func NewTickerProcessor(config *TickerProcessorConfig) *TickerProcessor {
	logger := config.Logger
	if logger == nil {
		logger = log.New(log.Writer(), "[TICKER-PROCESSOR] ", log.LstdFlags|log.Lshortfile)
	}

	outputBuffer := config.OutputBuffer
	if outputBuffer == 0 {
		outputBuffer = 100
	}

	return &TickerProcessor{
		logger:        logger,
		tickersInput:  config.TickersInput,
		tickersOutput: make(chan *bitso.Ticker, outputBuffer),
		stats: &TickerProcessorStatistics{
			StartTime: time.Now(),
			BookStats: make(map[string]*TickerBookStatistics),
		},
		stopChan: make(chan struct{}),
	}
}

// Start begins processing tickers
func (p *TickerProcessor) Start(ctx context.Context) error {
	p.logger.Println("Starting ticker processor...")

	p.wg.Add(1)
	go p.processingLoop(ctx)

	// Start statistics reporter
	p.wg.Add(1)
	go p.statsReporter(ctx)

	p.logger.Println("✓ Ticker processor started")
	return nil
}

// Stop gracefully stops the processor
func (p *TickerProcessor) Stop() error {
	p.logger.Println("Stopping ticker processor...")

	close(p.stopChan)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Println("Ticker processor stopped")
	case <-time.After(5 * time.Second):
		p.logger.Println("Warning: Ticker processor stop timeout")
	}

	close(p.tickersOutput)
	return nil
}

// processingLoop continuously processes incoming tickers
func (p *TickerProcessor) processingLoop(ctx context.Context) {
	defer p.wg.Done()
	p.logger.Println("Ticker processing loop started")

	for {
		select {
		case <-p.stopChan:
			p.logger.Println("Ticker processing loop stopping")
			return

		case <-ctx.Done():
			p.logger.Println("Context cancelled, ticker processing loop stopping")
			return

		case ticker, ok := <-p.tickersInput:
			if !ok {
				p.logger.Println("Ticker input channel closed")
				return
			}

			if err := p.ProcessTicker(ticker); err != nil {
				p.logger.Printf("Error processing ticker: %v", err)
			}
		}
	}
}

// ProcessTicker processes a single ticker
func (p *TickerProcessor) ProcessTicker(ticker *bitso.Ticker) error {
	if ticker == nil {
		return nil
	}

	// Validate ticker
	if err := p.validateTicker(ticker); err != nil {
		p.logger.Printf("Invalid ticker: %v", err)
		p.incrementDropped()
		return nil
	}

	// Update statistics
	p.updateStats(ticker)

	// Send to output channel (non-blocking)
	select {
	case p.tickersOutput <- ticker:
		p.logger.Printf("Processed ticker: %s Price=%.2f Volume=%.8f High=%.2f Low=%.2f",
			ticker.Book.String(), ticker.Last.Float64(), ticker.Volume.Float64(), ticker.High.Float64(), ticker.Low.Float64())

	case <-time.After(1 * time.Second):
		p.logger.Println("Warning: Ticker output channel full, dropping ticker")
		p.incrementDropped()
	}

	return nil
}

// GetTickerStream returns the output channel for processed tickers
func (p *TickerProcessor) GetTickerStream() <-chan *bitso.Ticker {
	return p.tickersOutput
}

// GetStatistics returns current processor statistics
func (p *TickerProcessor) GetStatistics() *TickerProcessorStatistics {
	p.statsMutex.RLock()
	defer p.statsMutex.RUnlock()

	// Create a copy to avoid race conditions
	statsCopy := *p.stats
	statsCopy.BookStats = make(map[string]*TickerBookStatistics)
	for book, bookStats := range p.stats.BookStats {
		bookStatsCopy := *bookStats
		statsCopy.BookStats[book] = &bookStatsCopy
	}

	return &statsCopy
}

// validateTicker validates a ticker
func (p *TickerProcessor) validateTicker(ticker *bitso.Ticker) error {
	// Book is a struct, not a pointer, so check if it's empty
	if ticker.Book.String() == "" {
		return fmt.Errorf("ticker book is empty")
	}

	// Monetary is a string type, convert to float64 for comparison
	if ticker.Last.Float64() <= 0 {
		return fmt.Errorf("ticker last price is invalid: %f", ticker.Last.Float64())
	}

	// Monetary is a string type, convert to float64 for comparison
	if ticker.Volume.Float64() < 0 {
		return fmt.Errorf("ticker volume is negative: %f", ticker.Volume.Float64())
	}

	if ticker.High.Float64() <= 0 {
		return fmt.Errorf("ticker high price is invalid: %f", ticker.High.Float64())
	}

	if ticker.Low.Float64() <= 0 {
		return fmt.Errorf("ticker low price is invalid: %f", ticker.Low.Float64())
	}

	lowVal := ticker.Low.Float64()
	highVal := ticker.High.Float64()
	lastVal := ticker.Last.Float64()

	if lowVal > highVal {
		return fmt.Errorf("ticker low price (%f) is greater than high price (%f)", lowVal, highVal)
	}

	if lastVal < lowVal || lastVal > highVal {
		return fmt.Errorf("ticker last price (%f) is outside high/low range (%f-%f)", lastVal, lowVal, highVal)
	}

	return nil
}

// updateStats updates processor statistics
func (p *TickerProcessor) updateStats(ticker *bitso.Ticker) {
	p.statsMutex.Lock()
	defer p.statsMutex.Unlock()

	p.stats.TickersProcessed++
	p.stats.LastTickerTime = time.Now()

	// Update book-specific statistics
	bookStr := ticker.Book.String()
	bookStats, exists := p.stats.BookStats[bookStr]
	if !exists {
		bookStats = &TickerBookStatistics{
			LastPrice: ticker.Last.Float64(),
			LastHigh:  ticker.High.Float64(),
			LastLow:   ticker.Low.Float64(),
		}
		p.stats.BookStats[bookStr] = bookStats
	}

	bookStats.TickersCount++
	bookStats.LastVolume = ticker.Volume.Float64()
	bookStats.LastUpdateTime = time.Now()

	// Calculate price change
	lastPrice := ticker.Last.Float64()
	if bookStats.LastPrice > 0 {
		bookStats.PriceChange = lastPrice - bookStats.LastPrice
		bookStats.PriceChangePercent = (bookStats.PriceChange / bookStats.LastPrice) * 100
	}

	// Update price levels
	bookStats.LastPrice = lastPrice
	bookStats.LastHigh = ticker.High.Float64()
	bookStats.LastLow = ticker.Low.Float64()

	// Calculate VWAP (simplified)
	volume := ticker.Volume.Float64()
	if volume > 0 {
		bookStats.LastVWAP = (lastPrice * volume) / volume
	}
}

// incrementDropped increments the dropped counter
func (p *TickerProcessor) incrementDropped() {
	p.statsMutex.Lock()
	defer p.statsMutex.Unlock()
	p.stats.TickersDropped++
}

// statsReporter periodically logs statistics
func (p *TickerProcessor) statsReporter(ctx context.Context) {
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
func (p *TickerProcessor) logStatistics() {
	stats := p.GetStatistics()

	uptime := time.Since(stats.StartTime).Round(time.Second)

	p.logger.Println("=== Ticker Processor Statistics ===")
	p.logger.Printf("Uptime: %v", uptime)
	p.logger.Printf("Tickers Processed: %d", stats.TickersProcessed)
	p.logger.Printf("Tickers Dropped: %d", stats.TickersDropped)

	if !stats.LastTickerTime.IsZero() {
		p.logger.Printf("Last Ticker: %v ago",
			time.Since(stats.LastTickerTime).Round(time.Second))
	}

	// Log book statistics
	for book, bookStats := range stats.BookStats {
		p.logger.Printf("  %s: Tickers=%d, LastPrice=%.2f, Volume=%.8f, Change=%.2f%%",
			book, bookStats.TickersCount, bookStats.LastPrice, bookStats.LastVolume, bookStats.PriceChangePercent)
	}
}
