package processor

import (
	"context"
	"log"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
)

// TradeSilenceRecorder records trade-age metrics for observability.
type TradeSilenceRecorder interface {
	SetLastTradeAgeSeconds(ageSec float64)
}

// TradeProcessor processes incoming trade messages
type TradeProcessor interface {
	Start(ctx context.Context) error
	Stop() error
	ProcessTrade(trade *bitso.WebSocketTrade) error
	GetProcessedTradesStream() <-chan *models.TradeEvent
	GetStatistics() *ProcessorStatistics
}

// Processor implements TradeProcessor
type Processor struct {
	logger *log.Logger

	// Input stream
	tradesInput <-chan *bitso.WebSocketTrade

	// Output stream
	tradesOutput chan *models.TradeEvent

	// Trade silence observability (no forced reconnect — Bitso sends keep-alives while idle)
	silenceThreshold      time.Duration
	silenceWarnCooldown   time.Duration
	silenceRecorder       TradeSilenceRecorder
	lastSilenceWarning    time.Time
	watchdogMu            sync.Mutex

	// Statistics
	stats      *ProcessorStatistics
	statsMutex sync.RWMutex

	// State management
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// ProcessorStatistics tracks processor performance
type ProcessorStatistics struct {
	StartTime        time.Time
	TradesProcessed  int64
	TradesDropped    int64
	LastTradeTime    time.Time
	LastTradeID      uint64
	AverageLatencyMs float64

	// Book-specific statistics
	BookStats map[string]*BookStatistics
}

// BookStatistics tracks statistics per trading book
type BookStatistics struct {
	TradesCount int64
	Volume      float64 // Total volume in major currency
	Value       float64 // Total value in minor currency
	LastPrice   float64
	LastAmount  float64
}

// ProcessorConfig holds configuration for the processor
type ProcessorConfig struct {
	Logger                   *log.Logger
	TradesInput              <-chan *bitso.WebSocketTrade
	OutputBuffer             int
	SilenceThreshold       time.Duration
	SilenceWarnCooldown    time.Duration
	SilenceRecorder        TradeSilenceRecorder
}

// NewProcessor creates a new trade processor
func NewProcessor(config *ProcessorConfig) *Processor {
	logger := config.Logger
	if logger == nil {
		logger = log.New(log.Writer(), "[TRADE-PROCESSOR] ", log.LstdFlags|log.Lshortfile)
	}

	outputBuffer := config.OutputBuffer
	if outputBuffer == 0 {
		outputBuffer = 100
	}

	cooldown := config.SilenceWarnCooldown
	if cooldown <= 0 {
		cooldown = 2 * time.Minute
	}

	return &Processor{
		logger:              logger,
		tradesInput:         config.TradesInput,
		tradesOutput:        make(chan *models.TradeEvent, outputBuffer),
		silenceThreshold:    config.SilenceThreshold,
		silenceWarnCooldown: cooldown,
		silenceRecorder:     config.SilenceRecorder,
		stats: &ProcessorStatistics{
			StartTime: time.Now(),
			BookStats: make(map[string]*BookStatistics),
		},
		stopChan: make(chan struct{}),
	}
}

// Start begins processing trades
func (p *Processor) Start(ctx context.Context) error {
	p.logger.Println("Starting trade processor...")

	p.wg.Add(1)
	go p.processingLoop(ctx)

	// Start statistics reporter
	p.wg.Add(1)
	go p.statsReporter(ctx)

	p.logger.Println("✓ Trade processor started")
	return nil
}

// Stop gracefully stops the processor
func (p *Processor) Stop() error {
	p.logger.Println("Stopping trade processor...")

	close(p.stopChan)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Println("Trade processor stopped")
	case <-time.After(5 * time.Second):
		p.logger.Println("Warning: Trade processor stop timeout")
	}

	close(p.tradesOutput)
	return nil
}

// processingLoop continuously processes incoming trades
func (p *Processor) processingLoop(ctx context.Context) {
	defer p.wg.Done()
	p.logger.Println("Processing loop started")

	for {
		select {
		case <-p.stopChan:
			p.logger.Println("Processing loop stopping")
			return

		case <-ctx.Done():
			p.logger.Println("Context cancelled, processing loop stopping")
			return

		case wsTrade, ok := <-p.tradesInput:
			if !ok {
				p.logger.Println("Input channel closed")
				return
			}

			if err := p.ProcessTrade(wsTrade); err != nil {
				p.logger.Printf("Error processing trade: %v", err)
			}
		}
	}
}

// ProcessTrade processes a single WebSocket trade.
func (p *Processor) ProcessTrade(wsTrade *bitso.WebSocketTrade) error {
	if wsTrade == nil {
		return nil
	}

	tradeEvent := models.FromBitsoWebSocketTrade(wsTrade)
	if tradeEvent == nil {
		p.logger.Println("Warning: Failed to convert WebSocket trade")
		p.incrementDropped()
		return nil
	}
	return p.ProcessTradeEvent(tradeEvent)
}

// ProcessTradeEvent ingests a normalized trade into the output stream.
func (p *Processor) ProcessTradeEvent(tradeEvent *models.TradeEvent) error {
	if tradeEvent == nil {
		return nil
	}

	p.updateStats(tradeEvent)

	select {
	case p.tradesOutput <- tradeEvent:
		p.logger.Printf("Processed trade: %s ID=%d Price=%.2f Amount=%.8f",
			tradeEvent.Book, tradeEvent.ID, tradeEvent.Price, tradeEvent.Amount)

	case <-time.After(1 * time.Second):
		p.logger.Println("Warning: Output channel full, dropping trade")
		p.incrementDropped()
	}

	return nil
}

// LastTradeAge returns how long ago the most recent trade was ingested.
func (p *Processor) LastTradeAge() time.Duration {
	stats := p.GetStatistics()
	if stats.LastTradeTime.IsZero() {
		return 0
	}
	return time.Since(stats.LastTradeTime)
}

// HasReceivedTrade reports whether any trade has been ingested.
func (p *Processor) HasReceivedTrade() bool {
	stats := p.GetStatistics()
	return !stats.LastTradeTime.IsZero()
}

// StartTime returns when the processor was created.
func (p *Processor) StartTime() time.Time {
	return p.GetStatistics().StartTime
}

// GetProcessedTradesStream returns the output channel for processed trades
func (p *Processor) GetProcessedTradesStream() <-chan *models.TradeEvent {
	return p.tradesOutput
}

// GetStatistics returns current processor statistics
func (p *Processor) GetStatistics() *ProcessorStatistics {
	p.statsMutex.RLock()
	defer p.statsMutex.RUnlock()

	// Create a copy to avoid race conditions
	statsCopy := *p.stats
	statsCopy.BookStats = make(map[string]*BookStatistics)
	for book, bookStats := range p.stats.BookStats {
		bookStatsCopy := *bookStats
		statsCopy.BookStats[book] = &bookStatsCopy
	}

	return &statsCopy
}

// updateStats updates processor statistics
func (p *Processor) updateStats(trade *models.TradeEvent) {
	p.statsMutex.Lock()
	defer p.statsMutex.Unlock()

	p.stats.TradesProcessed++
	p.stats.LastTradeTime = trade.Timestamp
	p.stats.LastTradeID = trade.ID

	// Update average latency (simple moving average)
	latency := float64(trade.GetLatencyMs())
	if p.stats.AverageLatencyMs == 0 {
		p.stats.AverageLatencyMs = latency
	} else {
		// Exponential moving average with alpha = 0.1
		p.stats.AverageLatencyMs = 0.9*p.stats.AverageLatencyMs + 0.1*latency
	}

	// Update book-specific statistics
	bookStats, exists := p.stats.BookStats[trade.Book]
	if !exists {
		bookStats = &BookStatistics{}
		p.stats.BookStats[trade.Book] = bookStats
	}

	bookStats.TradesCount++
	bookStats.Volume += trade.Amount
	bookStats.Value += trade.Value
	bookStats.LastPrice = trade.Price
	bookStats.LastAmount = trade.Amount
}

// incrementDropped increments the dropped counter
func (p *Processor) incrementDropped() {
	p.statsMutex.Lock()
	defer p.statsMutex.Unlock()
	p.stats.TradesDropped++
}

// statsReporter periodically logs statistics
func (p *Processor) statsReporter(ctx context.Context) {
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
			p.checkTradeSilence(ctx)
		}
	}
}

func shouldWarnTradeSilence(lastTrade time.Time, threshold, cooldown time.Duration, lastWarning, now time.Time) bool {
	if lastTrade.IsZero() || threshold <= 0 {
		return false
	}
	if now.Sub(lastTrade) <= threshold {
		return false
	}
	if !lastWarning.IsZero() && now.Sub(lastWarning) < cooldown {
		return false
	}
	return true
}

func (p *Processor) checkTradeSilence(ctx context.Context) {
	_ = ctx
	if p.silenceThreshold <= 0 {
		return
	}

	stats := p.GetStatistics()
	now := time.Now()
	if !stats.LastTradeTime.IsZero() && p.silenceRecorder != nil {
		p.silenceRecorder.SetLastTradeAgeSeconds(now.Sub(stats.LastTradeTime).Seconds())
	}

	p.watchdogMu.Lock()
	defer p.watchdogMu.Unlock()

	if !shouldWarnTradeSilence(stats.LastTradeTime, p.silenceThreshold, p.silenceWarnCooldown, p.lastSilenceWarning, now) {
		return
	}

	age := now.Sub(stats.LastTradeTime).Round(time.Second)
	p.logger.Printf("Trade silence: last trade %v ago (threshold %v); WebSocket may still be connected (Bitso sends keep-alives). REST fallback handles ingestion.",
		age, p.silenceThreshold)
	p.lastSilenceWarning = now
}

// logStatistics logs current statistics
func (p *Processor) logStatistics() {
	stats := p.GetStatistics()

	uptime := time.Since(stats.StartTime).Round(time.Second)

	p.logger.Println("=== Trade Processor Statistics ===")
	p.logger.Printf("Uptime: %v", uptime)
	p.logger.Printf("Trades Processed: %d", stats.TradesProcessed)
	p.logger.Printf("Trades Dropped: %d", stats.TradesDropped)
	p.logger.Printf("Average Latency: %.2fms", stats.AverageLatencyMs)

	if !stats.LastTradeTime.IsZero() {
		p.logger.Printf("Last Trade: %v ago (ID: %d)",
			time.Since(stats.LastTradeTime).Round(time.Second),
			stats.LastTradeID)
	}

	// Log book statistics
	for book, bookStats := range stats.BookStats {
		p.logger.Printf("  %s: Trades=%d, Volume=%.4f, LastPrice=%.2f",
			book, bookStats.TradesCount, bookStats.Volume, bookStats.LastPrice)
	}
}
