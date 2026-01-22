package publisher

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
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

// SignalPublisher publishes trading signals to Kafka
type SignalPublisher interface {
	Start(ctx context.Context) error
	Stop() error
	PublishSignal(signal *strategies.TradingSignal) error
	GetStatistics() *PublisherStatistics
}

// Publisher implements SignalPublisher
type Publisher struct {
	logger  *logger.Logger
	metrics *metrics.Metrics

	// Kafka producer
	producer *kafka.Producer
	topic    string

	// Input channels
	signalChannels map[string]chan *strategies.TradingSignal
	signalsMu      sync.RWMutex

	// State management
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Statistics
	stats      *PublisherStatistics
	statsMutex sync.RWMutex
}

// PublisherConfig holds configuration for the signal publisher
type PublisherConfig struct {
	Brokers          []string
	Topic            string
	BatchSize        int
	BatchTimeout     time.Duration
	CompressionCodec string
	RequiredAcks     int
}

// PublisherStatistics tracks publisher performance
type PublisherStatistics struct {
	StartTime         time.Time
	SignalsPublished  int64
	SignalsFailed     int64
	BytesPublished    int64
	LastPublishTime   time.Time
	LastError         string
	SignalsByStrategy map[string]int64
	SignalsByType     map[string]int64
}

// NewPublisher creates a new signal publisher
func NewPublisher(config *PublisherConfig, logger *logger.Logger, metrics *metrics.Metrics) (*Publisher, error) {
	if config == nil {
		return nil, fmt.Errorf("publisher config cannot be nil")
	}

	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	if metrics == nil {
		return nil, fmt.Errorf("metrics cannot be nil")
	}

	// Create Kafka producer
	producerConfig := &kafka.ProducerConfig{
		Brokers:          config.Brokers,
		Topic:            config.Topic,
		BatchSize:        config.BatchSize,
		BatchTimeout:     config.BatchTimeout,
		CompressionCodec: config.CompressionCodec,
		RequiredAcks:     config.RequiredAcks,
	}

	producer, err := kafka.NewProducer(producerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	return &Publisher{
		logger:         logger,
		metrics:        metrics,
		producer:       producer,
		topic:          config.Topic,
		signalChannels: make(map[string]chan *strategies.TradingSignal),
		stopChan:       make(chan struct{}),
		stats: &PublisherStatistics{
			StartTime:         time.Now(),
			SignalsByStrategy: make(map[string]int64),
			SignalsByType:     make(map[string]int64),
		},
	}, nil
}

// Start starts the signal publisher
func (p *Publisher) Start(ctx context.Context) error {
	p.logger.Info("Starting signal publisher...")

	// Start signal processing
	p.wg.Add(1)
	go p.processSignals(ctx)

	p.logger.Info("Signal publisher started successfully")
	return nil
}

// Stop stops the signal publisher
func (p *Publisher) Stop() error {
	p.logger.Info("Stopping signal publisher...")

	// Signal stop
	close(p.stopChan)

	// Wait for goroutines
	p.wg.Wait()

	// Close producer
	if p.producer != nil {
		if err := p.producer.Close(); err != nil {
			p.logger.Errorf("Error closing Kafka producer: %v", err)
		}
	}

	p.logger.Info("Signal publisher stopped")
	return nil
}

// PublishSignal publishes a trading signal to Kafka
func (p *Publisher) PublishSignal(signal *strategies.TradingSignal) error {
	start := time.Now()

	// Convert signal to event
	event := p.convertSignalToEvent(signal)

	// Serialize event
	data, err := json.Marshal(event)
	if err != nil {
		p.recordError("serialization_error", err)
		return fmt.Errorf("failed to serialize signal: %w", err)
	}

	// Publish to Kafka
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.producer.Produce(ctx, []byte(signal.Book.String()), data); err != nil {
		p.recordError("publish_error", err)
		return fmt.Errorf("failed to publish signal: %w", err)
	}

	// Record success
	p.recordSuccess(signal, len(data), time.Since(start))

	return nil
}

// RegisterStrategy registers a strategy's signal channel
func (p *Publisher) RegisterStrategy(strategyName string, signalChan chan *strategies.TradingSignal) {
	p.signalsMu.Lock()
	defer p.signalsMu.Unlock()

	p.signalChannels[strategyName] = signalChan
	p.logger.Infof("Registered signal channel for strategy '%s'", strategyName)
}

// UnregisterStrategy unregisters a strategy's signal channel
func (p *Publisher) UnregisterStrategy(strategyName string) {
	p.signalsMu.Lock()
	defer p.signalsMu.Unlock()

	delete(p.signalChannels, strategyName)
	p.logger.Infof("Unregistered signal channel for strategy '%s'", strategyName)
}

// GetStatistics returns publisher statistics
func (p *Publisher) GetStatistics() *PublisherStatistics {
	p.statsMutex.RLock()
	defer p.statsMutex.RUnlock()

	// Create a copy to avoid race conditions
	stats := *p.stats
	stats.SignalsByStrategy = make(map[string]int64)
	stats.SignalsByType = make(map[string]int64)

	for k, v := range p.stats.SignalsByStrategy {
		stats.SignalsByStrategy[k] = v
	}
	for k, v := range p.stats.SignalsByType {
		stats.SignalsByType[k] = v
	}

	return &stats
}

// processSignals processes signals from all strategies
func (p *Publisher) processSignals(ctx context.Context) {
	defer p.wg.Done()

	p.logger.Info("Starting signal processing...")

	for {
		select {
		case <-p.stopChan:
			p.logger.Info("Signal processing stopping...")
			return
		case <-ctx.Done():
			p.logger.Info("Signal processing context cancelled")
			return
		default:
			// Process signals from all strategies
			p.processStrategySignals()
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// processStrategySignals processes signals from all registered strategies
func (p *Publisher) processStrategySignals() {
	p.signalsMu.RLock()
	defer p.signalsMu.RUnlock()

	for strategyName, signalChan := range p.signalChannels {
		select {
		case signal, ok := <-signalChan:
			if !ok {
				continue
			}

			// Publish signal
			if err := p.PublishSignal(signal); err != nil {
				p.logger.Errorf("Failed to publish signal from strategy '%s': %v", strategyName, err)
			} else {
				p.logger.Debugf("Published signal from strategy '%s': %s", strategyName, signal.Reason)
			}
		default:
			// No signals pending
		}
	}
}

// convertSignalToEvent converts a trading signal to a signal event
func (p *Publisher) convertSignalToEvent(signal *strategies.TradingSignal) *models.TradeSignalEvent {
	signalType := "UNKNOWN"
	switch signal.Type {
	case strategies.SignalBuy:
		signalType = "BUY"
	case strategies.SignalSell:
		signalType = "SELL"
	case strategies.SignalHold:
		signalType = "HOLD"
	}

	return &models.TradeSignalEvent{
		EventID:   fmt.Sprintf("signal-%d", time.Now().UnixNano()),
		Timestamp: signal.Timestamp,
		Book:      signal.Book.String(),
		Strategy:  "unknown", // Will be set by caller
		Signal:    signalType,
		Price:     signal.Price,
		Amount:    signal.Amount,
		Metadata: map[string]interface{}{
			"reason": signal.Reason,
		},
	}
}

// recordSuccess records a successful signal publication
func (p *Publisher) recordSuccess(signal *strategies.TradingSignal, bytes int, duration time.Duration) {
	p.statsMutex.Lock()
	p.stats.SignalsPublished++
	p.stats.BytesPublished += int64(bytes)
	p.stats.LastPublishTime = time.Now()
	p.statsMutex.Unlock()

	// Get signal type
	signalType := "unknown"
	switch signal.Type {
	case strategies.SignalBuy:
		signalType = "buy"
	case strategies.SignalSell:
		signalType = "sell"
	case strategies.SignalHold:
		signalType = "hold"
	}

	// Record metrics
	p.metrics.RecordSignalPublished("unknown", signal.Book.String(), signalType)
	p.metrics.RecordKafkaProducerLatency(p.topic, duration)
	p.metrics.RecordKafkaMessageProduced(p.topic, 0)
}

// recordError records a signal publication error
func (p *Publisher) recordError(errorType string, err error) {
	p.statsMutex.Lock()
	p.stats.SignalsFailed++
	p.stats.LastError = err.Error()
	p.statsMutex.Unlock()

	// Record metrics
	p.metrics.RecordSignalFailed("unknown", "unknown", "unknown", errorType)
}
