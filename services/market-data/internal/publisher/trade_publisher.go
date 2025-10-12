package publisher

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"bitso-trading-platform/market-data/internal/models"
	"bitso-trading-platform/shared/pkg/kafka"
)

// TradePublisher publishes trade events to Kafka
type TradePublisher interface {
	Start(ctx context.Context) error
	Stop() error
	Publish(trade *models.TradeEvent) error
	GetStatistics() *PublisherStatistics
}

// Publisher implements TradePublisher
type Publisher struct {
	logger *log.Logger

	// Kafka producer
	producer *kafka.Producer
	topic    string

	// Input stream
	tradesInput <-chan *models.TradeEvent

	// Statistics
	stats      *PublisherStatistics
	statsMutex sync.RWMutex

	// State management
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// PublisherStatistics tracks publishing performance
type PublisherStatistics struct {
	StartTime         time.Time
	MessagesPublished int64
	MessagesFailed    int64
	BytesPublished    int64
	LastPublishTime   time.Time
	LastError         string
	LastErrorTime     time.Time

	// Performance metrics
	AveragePublishTimeMs float64
}

// PublisherConfig holds configuration for the publisher
type PublisherConfig struct {
	Logger      *log.Logger
	Producer    *kafka.Producer
	Topic       string
	TradesInput <-chan *models.TradeEvent
}

// NewPublisher creates a new trade publisher
func NewPublisher(config *PublisherConfig) *Publisher {
	logger := config.Logger
	if logger == nil {
		logger = log.New(log.Writer(), "[TRADE-PUBLISHER] ", log.LstdFlags|log.Lshortfile)
	}

	return &Publisher{
		logger:      logger,
		producer:    config.Producer,
		topic:       config.Topic,
		tradesInput: config.TradesInput,
		stats: &PublisherStatistics{
			StartTime: time.Now(),
		},
		stopChan: make(chan struct{}),
	}
}

// Start begins publishing trades
func (p *Publisher) Start(ctx context.Context) error {
	p.logger.Printf("Starting trade publisher for topic: %s", p.topic)

	p.wg.Add(1)
	go p.publishingLoop(ctx)

	// Start statistics reporter
	p.wg.Add(1)
	go p.statsReporter(ctx)

	p.logger.Println("✓ Trade publisher started")
	return nil
}

// Stop gracefully stops the publisher
func (p *Publisher) Stop() error {
	p.logger.Println("Stopping trade publisher...")

	close(p.stopChan)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Println("Trade publisher stopped")
	case <-time.After(5 * time.Second):
		p.logger.Println("Warning: Trade publisher stop timeout")
	}

	return nil
}

// publishingLoop continuously publishes trades from the input stream
func (p *Publisher) publishingLoop(ctx context.Context) {
	defer p.wg.Done()
	p.logger.Println("Publishing loop started")

	for {
		select {
		case <-p.stopChan:
			p.logger.Println("Publishing loop stopping")
			return

		case <-ctx.Done():
			p.logger.Println("Context cancelled, publishing loop stopping")
			return

		case trade, ok := <-p.tradesInput:
			if !ok {
				p.logger.Println("Input channel closed")
				return
			}

			if err := p.Publish(trade); err != nil {
				p.logger.Printf("Error publishing trade: %v", err)
				p.recordError(err)
			}
		}
	}
}

// Publish publishes a single trade event to Kafka
func (p *Publisher) Publish(trade *models.TradeEvent) error {
	if trade == nil {
		return fmt.Errorf("trade event is nil")
	}

	startTime := time.Now()

	// Serialize trade to JSON
	data, err := json.Marshal(trade)
	if err != nil {
		return fmt.Errorf("failed to marshal trade: %w", err)
	}

	// Create Kafka key (book + trade ID for partitioning)
	key := []byte(fmt.Sprintf("%s:%d", trade.Book, trade.ID))

	// Publish to Kafka with timeout
	publishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.producer.Produce(publishCtx, key, data); err != nil {
		p.incrementFailed()
		return fmt.Errorf("failed to produce message: %w", err)
	}

	// Update statistics
	publishTime := time.Since(startTime)
	p.updateStats(len(data), publishTime)

	p.logger.Printf("Published trade: %s ID=%d to topic=%s (%.2fms)",
		trade.Book, trade.ID, p.topic, publishTime.Seconds()*1000)

	return nil
}

// GetStatistics returns current publisher statistics
func (p *Publisher) GetStatistics() *PublisherStatistics {
	p.statsMutex.RLock()
	defer p.statsMutex.RUnlock()

	statsCopy := *p.stats
	return &statsCopy
}

// updateStats updates publisher statistics
func (p *Publisher) updateStats(bytes int, publishTime time.Duration) {
	p.statsMutex.Lock()
	defer p.statsMutex.Unlock()

	p.stats.MessagesPublished++
	p.stats.BytesPublished += int64(bytes)
	p.stats.LastPublishTime = time.Now()

	// Update average publish time (exponential moving average)
	publishTimeMs := publishTime.Seconds() * 1000
	if p.stats.AveragePublishTimeMs == 0 {
		p.stats.AveragePublishTimeMs = publishTimeMs
	} else {
		p.stats.AveragePublishTimeMs = 0.9*p.stats.AveragePublishTimeMs + 0.1*publishTimeMs
	}
}

// incrementFailed increments the failed counter
func (p *Publisher) incrementFailed() {
	p.statsMutex.Lock()
	defer p.statsMutex.Unlock()
	p.stats.MessagesFailed++
}

// recordError records an error
func (p *Publisher) recordError(err error) {
	p.statsMutex.Lock()
	defer p.statsMutex.Unlock()

	p.stats.LastError = err.Error()
	p.stats.LastErrorTime = time.Now()
	p.stats.MessagesFailed++
}

// statsReporter periodically logs statistics
func (p *Publisher) statsReporter(ctx context.Context) {
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
func (p *Publisher) logStatistics() {
	stats := p.GetStatistics()

	uptime := time.Since(stats.StartTime).Round(time.Second)
	successRate := float64(0)
	if stats.MessagesPublished+stats.MessagesFailed > 0 {
		successRate = float64(stats.MessagesPublished) / float64(stats.MessagesPublished+stats.MessagesFailed) * 100
	}

	p.logger.Println("=== Trade Publisher Statistics ===")
	p.logger.Printf("Uptime: %v", uptime)
	p.logger.Printf("Messages Published: %d", stats.MessagesPublished)
	p.logger.Printf("Messages Failed: %d", stats.MessagesFailed)
	p.logger.Printf("Success Rate: %.2f%%", successRate)
	p.logger.Printf("Bytes Published: %d (%.2f KB)", stats.BytesPublished, float64(stats.BytesPublished)/1024)
	p.logger.Printf("Average Publish Time: %.2fms", stats.AveragePublishTimeMs)

	if !stats.LastPublishTime.IsZero() {
		p.logger.Printf("Last Publish: %v ago", time.Since(stats.LastPublishTime).Round(time.Second))
	}

	if !stats.LastErrorTime.IsZero() {
		p.logger.Printf("Last Error: %v ago - %s",
			time.Since(stats.LastErrorTime).Round(time.Second),
			stats.LastError)
	}
}
