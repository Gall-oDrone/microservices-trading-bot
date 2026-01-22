package processor

import (
	"context"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/strategy-executor/internal/consumer"
	"bitso-trading-platform/strategy-executor/internal/logger"
	"bitso-trading-platform/strategy-executor/internal/metrics"
)

// DataProcessor processes incoming market data and distributes it to strategies
type DataProcessor interface {
	Start(ctx context.Context) error
	Stop() error
	ProcessTradeEvent(event *models.TradeEvent) error
	ProcessTickerEvent(event *consumer.TickerEvent) error
	ProcessOrderBookEvent(event *consumer.OrderBookEvent) error
	GetProcessedEvents() <-chan *ProcessedEvent
	GetStatistics() *ProcessorStatistics
}

// Processor implements DataProcessor
type Processor struct {
	logger  *logger.Logger
	metrics *metrics.Metrics

	// Input channels
	tradeEvents     <-chan *models.TradeEvent
	tickerEvents    <-chan *consumer.TickerEvent
	orderBookEvents <-chan *consumer.OrderBookEvent

	// Output channel
	processedEvents chan *ProcessedEvent

	// State management
	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.RWMutex

	// Statistics
	stats      *ProcessorStatistics
	statsMutex sync.RWMutex

	// Event filters
	eventFilters []EventFilter
}

// ProcessedEvent represents a processed market data event
type ProcessedEvent struct {
	Type      EventType              `json:"type"`
	Book      string                 `json:"book"`
	Data      interface{}            `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// EventType represents the type of processed event
type EventType string

const (
	EventTypeTrade     EventType = "trade"
	EventTypeTicker    EventType = "ticker"
	EventTypeOrderBook EventType = "orderbook"
)

// EventFilter filters events based on criteria
type EventFilter interface {
	ShouldProcess(event *ProcessedEvent) bool
	GetName() string
}

// ProcessorStatistics tracks processor performance
type ProcessorStatistics struct {
	StartTime        time.Time
	EventsProcessed  int64
	EventsFiltered   int64
	EventsFailed     int64
	LastEventTime    time.Time
	LastEventType    EventType
	AverageLatencyMs float64
	EventsByType     map[EventType]int64
	EventsByBook     map[string]int64
}

// NewProcessor creates a new data processor
func NewProcessor(
	tradeEvents <-chan *models.TradeEvent,
	tickerEvents <-chan *consumer.TickerEvent,
	orderBookEvents <-chan *consumer.OrderBookEvent,
	logger *logger.Logger,
	metrics *metrics.Metrics,
) *Processor {
	return &Processor{
		logger:          logger,
		metrics:         metrics,
		tradeEvents:     tradeEvents,
		tickerEvents:    tickerEvents,
		orderBookEvents: orderBookEvents,
		processedEvents: make(chan *ProcessedEvent, 1000),
		stopChan:        make(chan struct{}),
		stats: &ProcessorStatistics{
			StartTime:    time.Now(),
			EventsByType: make(map[EventType]int64),
			EventsByBook: make(map[string]int64),
		},
		eventFilters: make([]EventFilter, 0),
	}
}

// Start starts the data processor
func (p *Processor) Start(ctx context.Context) error {
	p.logger.Info("Starting data processor...")

	// Start processing goroutines
	p.wg.Add(3)
	go p.processTradeEvents(ctx)
	go p.processTickerEvents(ctx)
	go p.processOrderBookEvents(ctx)

	p.logger.Info("Data processor started successfully")
	return nil
}

// Stop stops the data processor
func (p *Processor) Stop() error {
	p.logger.Info("Stopping data processor...")

	// Signal stop
	close(p.stopChan)

	// Wait for goroutines to finish
	p.wg.Wait()

	// Close output channel
	close(p.processedEvents)

	p.logger.Info("Data processor stopped")
	return nil
}

// ProcessTradeEvent processes a single trade event
func (p *Processor) ProcessTradeEvent(event *models.TradeEvent) error {
	start := time.Now()

	processedEvent := &ProcessedEvent{
		Type:      EventTypeTrade,
		Book:      event.Book,
		Data:      event,
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"source":     event.Source,
			"trade_id":   event.ID,
			"price":      event.Price,
			"amount":     event.Amount,
			"maker_side": event.MakerSide,
		},
	}

	// Apply filters
	if !p.shouldProcessEvent(processedEvent) {
		p.recordFilteredEvent(processedEvent)
		return nil
	}

	// Send to output channel
	select {
	case p.processedEvents <- processedEvent:
		p.recordProcessedEvent(processedEvent, time.Since(start))
		return nil
	case <-p.stopChan:
		return nil // Processor is stopping
	default:
		p.recordFailedEvent(processedEvent, "channel_full")
		return nil
	}
}

// ProcessTickerEvent processes a single ticker event
func (p *Processor) ProcessTickerEvent(event *consumer.TickerEvent) error {
	start := time.Now()

	processedEvent := &ProcessedEvent{
		Type:      EventTypeTicker,
		Book:      event.Book,
		Data:      event,
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"bid":    event.Bid,
			"ask":    event.Ask,
			"last":   event.Last,
			"volume": event.Volume,
		},
	}

	// Apply filters
	if !p.shouldProcessEvent(processedEvent) {
		p.recordFilteredEvent(processedEvent)
		return nil
	}

	// Send to output channel
	select {
	case p.processedEvents <- processedEvent:
		p.recordProcessedEvent(processedEvent, time.Since(start))
		return nil
	case <-p.stopChan:
		return nil // Processor is stopping
	default:
		p.recordFailedEvent(processedEvent, "channel_full")
		return nil
	}
}

// ProcessOrderBookEvent processes a single order book event
func (p *Processor) ProcessOrderBookEvent(event *consumer.OrderBookEvent) error {
	start := time.Now()

	processedEvent := &ProcessedEvent{
		Type:      EventTypeOrderBook,
		Book:      event.Book,
		Data:      event,
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"bids_count": len(event.Bids),
			"asks_count": len(event.Asks),
		},
	}

	// Apply filters
	if !p.shouldProcessEvent(processedEvent) {
		p.recordFilteredEvent(processedEvent)
		return nil
	}

	// Send to output channel
	select {
	case p.processedEvents <- processedEvent:
		p.recordProcessedEvent(processedEvent, time.Since(start))
		return nil
	case <-p.stopChan:
		return nil // Processor is stopping
	default:
		p.recordFailedEvent(processedEvent, "channel_full")
		return nil
	}
}

// GetProcessedEvents returns the channel for processed events
func (p *Processor) GetProcessedEvents() <-chan *ProcessedEvent {
	return p.processedEvents
}

// GetStatistics returns processor statistics
func (p *Processor) GetStatistics() *ProcessorStatistics {
	p.statsMutex.RLock()
	defer p.statsMutex.RUnlock()

	// Create a copy to avoid race conditions
	stats := *p.stats
	stats.EventsByType = make(map[EventType]int64)
	stats.EventsByBook = make(map[string]int64)

	for k, v := range p.stats.EventsByType {
		stats.EventsByType[k] = v
	}
	for k, v := range p.stats.EventsByBook {
		stats.EventsByBook[k] = v
	}

	return &stats
}

// AddEventFilter adds an event filter
func (p *Processor) AddEventFilter(filter EventFilter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.eventFilters = append(p.eventFilters, filter)
}

// RemoveEventFilter removes an event filter by name
func (p *Processor) RemoveEventFilter(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, filter := range p.eventFilters {
		if filter.GetName() == name {
			p.eventFilters = append(p.eventFilters[:i], p.eventFilters[i+1:]...)
			break
		}
	}
}

// processTradeEvents processes trade events from the input channel
func (p *Processor) processTradeEvents(ctx context.Context) {
	defer p.wg.Done()

	p.logger.Info("Starting trade events processor...")

	for {
		select {
		case <-p.stopChan:
			p.logger.Info("Trade events processor stopping...")
			return
		case <-ctx.Done():
			p.logger.Info("Trade events processor context cancelled")
			return
		case event, ok := <-p.tradeEvents:
			if !ok {
				p.logger.Info("Trade events channel closed")
				return
			}

			if err := p.ProcessTradeEvent(event); err != nil {
				p.logger.Errorf("Failed to process trade event: %v", err)
			}
		}
	}
}

// processTickerEvents processes ticker events from the input channel
func (p *Processor) processTickerEvents(ctx context.Context) {
	defer p.wg.Done()

	p.logger.Info("Starting ticker events processor...")

	for {
		select {
		case <-p.stopChan:
			p.logger.Info("Ticker events processor stopping...")
			return
		case <-ctx.Done():
			p.logger.Info("Ticker events processor context cancelled")
			return
		case event, ok := <-p.tickerEvents:
			if !ok {
				p.logger.Info("Ticker events channel closed")
				return
			}

			if err := p.ProcessTickerEvent(event); err != nil {
				p.logger.Errorf("Failed to process ticker event: %v", err)
			}
		}
	}
}

// processOrderBookEvents processes order book events from the input channel
func (p *Processor) processOrderBookEvents(ctx context.Context) {
	defer p.wg.Done()

	p.logger.Info("Starting order book events processor...")

	for {
		select {
		case <-p.stopChan:
			p.logger.Info("Order book events processor stopping...")
			return
		case <-ctx.Done():
			p.logger.Info("Order book events processor context cancelled")
			return
		case event, ok := <-p.orderBookEvents:
			if !ok {
				p.logger.Info("Order book events channel closed")
				return
			}

			if err := p.ProcessOrderBookEvent(event); err != nil {
				p.logger.Errorf("Failed to process order book event: %v", err)
			}
		}
	}
}

// shouldProcessEvent checks if an event should be processed based on filters
func (p *Processor) shouldProcessEvent(event *ProcessedEvent) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, filter := range p.eventFilters {
		if !filter.ShouldProcess(event) {
			return false
		}
	}

	return true
}

// recordProcessedEvent records a successfully processed event
func (p *Processor) recordProcessedEvent(event *ProcessedEvent, duration time.Duration) {
	p.statsMutex.Lock()
	p.stats.EventsProcessed++
	p.stats.LastEventTime = time.Now()
	p.stats.LastEventType = event.Type
	p.stats.EventsByType[event.Type]++
	p.stats.EventsByBook[event.Book]++
	p.statsMutex.Unlock()

	// Record metrics
	p.metrics.RecordMarketDataMessageProcessed(string(event.Type), event.Book, duration)
}

// recordFilteredEvent records a filtered event
func (p *Processor) recordFilteredEvent(event *ProcessedEvent) {
	p.statsMutex.Lock()
	p.stats.EventsFiltered++
	p.statsMutex.Unlock()
}

// recordFailedEvent records a failed event processing
func (p *Processor) recordFailedEvent(event *ProcessedEvent, reason string) {
	p.statsMutex.Lock()
	p.stats.EventsFailed++
	p.statsMutex.Unlock()

	// Record metrics
	p.metrics.RecordMarketDataError(string(event.Type), event.Book, reason)
}
