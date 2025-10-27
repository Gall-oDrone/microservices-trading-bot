package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/kafka"
	"bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/strategy-executor/internal/logger"
	"bitso-trading-platform/strategy-executor/internal/metrics"

	kafkaGo "github.com/segmentio/kafka-go"
)

// MarketDataConsumer consumes market data from Kafka topics
type MarketDataConsumer interface {
	Start(ctx context.Context) error
	Stop() error
	SubscribeToBook(book string) error
	UnsubscribeFromBook(book string) error
	GetConsumedMessages() <-chan *models.TradeEvent
	GetStatistics() *ConsumerStatistics
}

// Consumer implements MarketDataConsumer
type Consumer struct {
	logger  *logger.Logger
	metrics *metrics.Metrics

	// Kafka consumers for different topics
	tradeConsumer     *kafka.Consumer
	tickerConsumer    *kafka.Consumer
	orderBookConsumer *kafka.Consumer

	// Configuration
	config *ConsumerConfig

	// Output channels
	tradeEvents     chan *models.TradeEvent
	tickerEvents    chan *TickerEvent
	orderBookEvents chan *OrderBookEvent

	// State management
	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.RWMutex

	// Statistics
	stats      *ConsumerStatistics
	statsMutex sync.RWMutex

	// Subscribed books
	subscribedBooks map[string]bool
}

// ConsumerConfig holds configuration for the market data consumer
type ConsumerConfig struct {
	Brokers         []string
	ConsumerGroup   string
	Topics          TopicsConfig
	AutoOffsetReset string
	CommitInterval  time.Duration
	MaxWait         time.Duration
}

// TopicsConfig holds topic configuration
type TopicsConfig struct {
	Trades    string
	Tickers   string
	OrderBook string
}

// TickerEvent represents a ticker update event
type TickerEvent struct {
	Book      string    `json:"book"`
	Bid       float64   `json:"bid"`
	Ask       float64   `json:"ask"`
	Last      float64   `json:"last"`
	Volume    float64   `json:"volume"`
	Timestamp time.Time `json:"timestamp"`
}

// OrderBookEvent represents an order book update event
type OrderBookEvent struct {
	Book      string                 `json:"book"`
	Bids      []OrderBookEntry       `json:"bids"`
	Asks      []OrderBookEntry       `json:"asks"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// OrderBookEntry represents a single order book entry
type OrderBookEntry struct {
	Price  float64 `json:"price"`
	Amount float64 `json:"amount"`
}

// ConsumerStatistics tracks consumer performance
type ConsumerStatistics struct {
	StartTime         time.Time
	MessagesConsumed  int64
	MessagesProcessed int64
	MessagesFailed    int64
	LastMessageTime   time.Time
	LastMessageID     string
	AverageLatencyMs  float64
	BooksSubscribed   int
	ConsumerLag       map[string]int64
}

// NewConsumer creates a new market data consumer
func NewConsumer(config *ConsumerConfig, logger *logger.Logger, metrics *metrics.Metrics) (*Consumer, error) {
	if config == nil {
		return nil, fmt.Errorf("consumer config cannot be nil")
	}

	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	if metrics == nil {
		return nil, fmt.Errorf("metrics cannot be nil")
	}

	return &Consumer{
		logger:          logger,
		metrics:         metrics,
		config:          config,
		tradeEvents:     make(chan *models.TradeEvent, 1000),
		tickerEvents:    make(chan *TickerEvent, 1000),
		orderBookEvents: make(chan *OrderBookEvent, 1000),
		stopChan:        make(chan struct{}),
		stats:           &ConsumerStatistics{StartTime: time.Now(), ConsumerLag: make(map[string]int64)},
		subscribedBooks: make(map[string]bool),
	}, nil
}

// Start starts the market data consumer
func (c *Consumer) Start(ctx context.Context) error {
	c.logger.Info("Starting market data consumer...")

	// Initialize Kafka consumers
	if err := c.initializeConsumers(); err != nil {
		return fmt.Errorf("failed to initialize consumers: %w", err)
	}

	// Start consuming from each topic
	c.wg.Add(3)
	go c.consumeTrades(ctx)
	go c.consumeTickers(ctx)
	go c.consumeOrderBooks(ctx)

	c.logger.Info("Market data consumer started successfully")
	return nil
}

// Stop stops the market data consumer
func (c *Consumer) Stop() error {
	c.logger.Info("Stopping market data consumer...")

	// Signal stop
	close(c.stopChan)

	// Close consumers
	if c.tradeConsumer != nil {
		c.tradeConsumer.Close()
	}
	if c.tickerConsumer != nil {
		c.tickerConsumer.Close()
	}
	if c.orderBookConsumer != nil {
		c.orderBookConsumer.Close()
	}

	// Wait for goroutines to finish
	c.wg.Wait()

	// Close output channels
	close(c.tradeEvents)
	close(c.tickerEvents)
	close(c.orderBookEvents)

	c.logger.Info("Market data consumer stopped")
	return nil
}

// SubscribeToBook subscribes to market data for a specific book
func (c *Consumer) SubscribeToBook(book string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.subscribedBooks[book] = true
	c.statsMutex.Lock()
	c.stats.BooksSubscribed = len(c.subscribedBooks)
	c.statsMutex.Unlock()

	c.logger.Infof("Subscribed to book: %s", book)
	return nil
}

// UnsubscribeFromBook unsubscribes from market data for a specific book
func (c *Consumer) UnsubscribeFromBook(book string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.subscribedBooks, book)
	c.statsMutex.Lock()
	c.stats.BooksSubscribed = len(c.subscribedBooks)
	c.statsMutex.Unlock()

	c.logger.Infof("Unsubscribed from book: %s", book)
	return nil
}

// GetConsumedMessages returns the channel for consumed trade events
func (c *Consumer) GetConsumedMessages() <-chan *models.TradeEvent {
	return c.tradeEvents
}

// GetStatistics returns consumer statistics
func (c *Consumer) GetStatistics() *ConsumerStatistics {
	c.statsMutex.RLock()
	defer c.statsMutex.RUnlock()

	// Create a copy to avoid race conditions
	stats := *c.stats
	stats.ConsumerLag = make(map[string]int64)
	for k, v := range c.stats.ConsumerLag {
		stats.ConsumerLag[k] = v
	}

	return &stats
}

// initializeConsumers initializes Kafka consumers for all topics
func (c *Consumer) initializeConsumers() error {
	// Trade consumer
	tradeConfig := &kafka.ConsumerConfig{
		Brokers:         c.config.Brokers,
		Topic:           c.config.Topics.Trades,
		GroupID:         c.config.ConsumerGroup,
		AutoOffsetReset: c.config.AutoOffsetReset,
		CommitInterval:  c.config.CommitInterval,
		MaxWait:         c.config.MaxWait,
	}

	var err error
	c.tradeConsumer, err = kafka.NewConsumer(tradeConfig)
	if err != nil {
		return fmt.Errorf("failed to create trade consumer: %w", err)
	}

	// Ticker consumer
	tickerConfig := &kafka.ConsumerConfig{
		Brokers:         c.config.Brokers,
		Topic:           c.config.Topics.Tickers,
		GroupID:         c.config.ConsumerGroup,
		AutoOffsetReset: c.config.AutoOffsetReset,
		CommitInterval:  c.config.CommitInterval,
		MaxWait:         c.config.MaxWait,
	}

	c.tickerConsumer, err = kafka.NewConsumer(tickerConfig)
	if err != nil {
		return fmt.Errorf("failed to create ticker consumer: %w", err)
	}

	// Order book consumer
	orderBookConfig := &kafka.ConsumerConfig{
		Brokers:         c.config.Brokers,
		Topic:           c.config.Topics.OrderBook,
		GroupID:         c.config.ConsumerGroup,
		AutoOffsetReset: c.config.AutoOffsetReset,
		CommitInterval:  c.config.CommitInterval,
		MaxWait:         c.config.MaxWait,
	}

	c.orderBookConsumer, err = kafka.NewConsumer(orderBookConfig)
	if err != nil {
		return fmt.Errorf("failed to create order book consumer: %w", err)
	}

	return nil
}

// consumeTrades consumes trade events from Kafka
func (c *Consumer) consumeTrades(ctx context.Context) {
	defer c.wg.Done()

	c.logger.Info("Starting trade consumer...")

	for {
		select {
		case <-c.stopChan:
			c.logger.Info("Trade consumer stopping...")
			return
		case <-ctx.Done():
			c.logger.Info("Trade consumer context cancelled")
			return
		default:
			// Consume message
			msg, err := c.tradeConsumer.ConsumeMessage(ctx)
			if err != nil {
				c.logger.Errorf("Failed to consume trade message: %v", err)
				c.recordError("trades", "consume_error")
				continue
			}

			// Process message
			if err := c.processTradeMessage(msg); err != nil {
				c.logger.Errorf("Failed to process trade message: %v", err)
				c.recordError("trades", "process_error")
				continue
			}

			// Commit message
			if err := c.tradeConsumer.CommitMessage(ctx, msg); err != nil {
				c.logger.Errorf("Failed to commit trade message: %v", err)
			}
		}
	}
}

// consumeTickers consumes ticker events from Kafka
func (c *Consumer) consumeTickers(ctx context.Context) {
	defer c.wg.Done()

	c.logger.Info("Starting ticker consumer...")

	for {
		select {
		case <-c.stopChan:
			c.logger.Info("Ticker consumer stopping...")
			return
		case <-ctx.Done():
			c.logger.Info("Ticker consumer context cancelled")
			return
		default:
			// Consume message
			msg, err := c.tickerConsumer.ConsumeMessage(ctx)
			if err != nil {
				c.logger.Errorf("Failed to consume ticker message: %v", err)
				c.recordError("tickers", "consume_error")
				continue
			}

			// Process message
			if err := c.processTickerMessage(msg); err != nil {
				c.logger.Errorf("Failed to process ticker message: %v", err)
				c.recordError("tickers", "process_error")
				continue
			}

			// Commit message
			if err := c.tickerConsumer.CommitMessage(ctx, msg); err != nil {
				c.logger.Errorf("Failed to commit ticker message: %v", err)
			}
		}
	}
}

// consumeOrderBooks consumes order book events from Kafka
func (c *Consumer) consumeOrderBooks(ctx context.Context) {
	defer c.wg.Done()

	c.logger.Info("Starting order book consumer...")

	for {
		select {
		case <-c.stopChan:
			c.logger.Info("Order book consumer stopping...")
			return
		case <-ctx.Done():
			c.logger.Info("Order book consumer context cancelled")
			return
		default:
			// Consume message
			msg, err := c.orderBookConsumer.ConsumeMessage(ctx)
			if err != nil {
				c.logger.Errorf("Failed to consume order book message: %v", err)
				c.recordError("orderbook", "consume_error")
				continue
			}

			// Process message
			if err := c.processOrderBookMessage(msg); err != nil {
				c.logger.Errorf("Failed to process order book message: %v", err)
				c.recordError("orderbook", "process_error")
				continue
			}

			// Commit message
			if err := c.orderBookConsumer.CommitMessage(ctx, msg); err != nil {
				c.logger.Errorf("Failed to commit order book message: %v", err)
			}
		}
	}
}

// processTradeMessage processes a trade message
func (c *Consumer) processTradeMessage(msg kafkaGo.Message) error {
	start := time.Now()

	// Parse trade event
	var tradeEvent models.TradeEvent
	if err := json.Unmarshal(msg.Value, &tradeEvent); err != nil {
		return fmt.Errorf("failed to unmarshal trade event: %w", err)
	}

	// Check if we're subscribed to this book
	c.mu.RLock()
	subscribed := c.subscribedBooks[tradeEvent.Book]
	c.mu.RUnlock()

	if !subscribed {
		// Skip if not subscribed to this book
		return nil
	}

	// Send to output channel
	select {
	case c.tradeEvents <- &tradeEvent:
		c.recordSuccess("trades", tradeEvent.Book, time.Since(start))
		return nil
	case <-c.stopChan:
		return fmt.Errorf("consumer is stopping")
	default:
		return fmt.Errorf("trade events channel is full")
	}
}

// processTickerMessage processes a ticker message
func (c *Consumer) processTickerMessage(msg kafkaGo.Message) error {
	start := time.Now()

	// Parse ticker event
	var tickerEvent TickerEvent
	if err := json.Unmarshal(msg.Value, &tickerEvent); err != nil {
		return fmt.Errorf("failed to unmarshal ticker event: %w", err)
	}

	// Check if we're subscribed to this book
	c.mu.RLock()
	subscribed := c.subscribedBooks[tickerEvent.Book]
	c.mu.RUnlock()

	if !subscribed {
		// Skip if not subscribed to this book
		return nil
	}

	// Send to output channel
	select {
	case c.tickerEvents <- &tickerEvent:
		c.recordSuccess("tickers", tickerEvent.Book, time.Since(start))
		return nil
	case <-c.stopChan:
		return fmt.Errorf("consumer is stopping")
	default:
		return fmt.Errorf("ticker events channel is full")
	}
}

// processOrderBookMessage processes an order book message
func (c *Consumer) processOrderBookMessage(msg kafkaGo.Message) error {
	start := time.Now()

	// Parse order book event
	var orderBookEvent OrderBookEvent
	if err := json.Unmarshal(msg.Value, &orderBookEvent); err != nil {
		return fmt.Errorf("failed to unmarshal order book event: %w", err)
	}

	// Check if we're subscribed to this book
	c.mu.RLock()
	subscribed := c.subscribedBooks[orderBookEvent.Book]
	c.mu.RUnlock()

	if !subscribed {
		// Skip if not subscribed to this book
		return nil
	}

	// Send to output channel
	select {
	case c.orderBookEvents <- &orderBookEvent:
		c.recordSuccess("orderbook", orderBookEvent.Book, time.Since(start))
		return nil
	case <-c.stopChan:
		return fmt.Errorf("consumer is stopping")
	default:
		return fmt.Errorf("order book events channel is full")
	}
}

// recordSuccess records a successful message processing
func (c *Consumer) recordSuccess(topic, book string, duration time.Duration) {
	c.statsMutex.Lock()
	c.stats.MessagesConsumed++
	c.stats.MessagesProcessed++
	c.stats.LastMessageTime = time.Now()
	c.statsMutex.Unlock()

	// Record metrics
	c.metrics.RecordMarketDataMessageReceived(topic, book)
	c.metrics.RecordMarketDataMessageProcessed(topic, book, duration)
	c.metrics.RecordKafkaMessageConsumed(topic, 0) // Partition 0 for simplicity
}

// recordError records a message processing error
func (c *Consumer) recordError(topic, errorType string) {
	c.statsMutex.Lock()
	c.stats.MessagesConsumed++
	c.stats.MessagesFailed++
	c.statsMutex.Unlock()

	// Record metrics
	c.metrics.RecordMarketDataError(topic, "unknown", errorType)
}
