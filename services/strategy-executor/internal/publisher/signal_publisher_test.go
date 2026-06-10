package publisher

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/strategy-executor/internal/logger"
	"bitso-trading-platform/strategy-executor/internal/metrics"
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

func TestNewPublisher(t *testing.T) {
	config := &PublisherConfig{
		Brokers:          []string{"localhost:9092"},
		Topic:            "test-topic",
		BatchSize:        100,
		BatchTimeout:     time.Second,
		CompressionCodec: "snappy",
		RequiredAcks:     -1,
	}

	testLogger := logger.NewDefault()
	testMetrics := metrics.New("test")

	tests := []struct {
		name    string
		config  *PublisherConfig
		logger  *logger.Logger
		metrics *metrics.Metrics
		wantErr bool
	}{
		{
			name:    "valid configuration",
			config:  config,
			logger:  testLogger,
			metrics: testMetrics,
			wantErr: false,
		},
		{
			name:    "nil config",
			config:  nil,
			logger:  testLogger,
			metrics: testMetrics,
			wantErr: true,
		},
		{
			name:    "nil logger",
			config:  config,
			logger:  nil,
			metrics: testMetrics,
			wantErr: true,
		},
		{
			name:    "nil metrics",
			config:  config,
			logger:  testLogger,
			metrics: nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher, err := NewPublisher(tt.config, tt.logger, tt.metrics)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				if publisher != nil {
					t.Error("Expected nil publisher on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if publisher == nil {
					t.Error("Expected publisher, got nil")
				}
				// Clean up
				if publisher != nil {
					publisher.Stop()
				}
			}
		})
	}
}

func TestPublisher_RegisterUnregisterStrategy(t *testing.T) {
	config := &PublisherConfig{
		Brokers:          []string{"localhost:9092"},
		Topic:            "test-topic",
		BatchSize:        100,
		BatchTimeout:     time.Second,
		CompressionCodec: "snappy",
		RequiredAcks:     -1,
	}

	logger := logger.NewDefault()
	metrics := metrics.New("test")

	publisher, err := NewPublisher(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewPublisher() failed: %v", err)
	}
	defer publisher.Stop()

	// Register strategy
	signalChan := make(chan *strategies.TradingSignal, 10)
	publisher.RegisterStrategy("test-strategy", signalChan)

	// Check if strategy is registered
	publisher.signalsMu.RLock()
	_, exists := publisher.signalChannels["test-strategy"]
	publisher.signalsMu.RUnlock()

	if !exists {
		t.Error("Expected strategy to be registered")
	}

	// Unregister strategy
	publisher.UnregisterStrategy("test-strategy")

	// Check if strategy is unregistered
	publisher.signalsMu.RLock()
	_, exists = publisher.signalChannels["test-strategy"]
	publisher.signalsMu.RUnlock()

	if exists {
		t.Error("Expected strategy to be unregistered")
	}

	close(signalChan)
}

func TestPublisher_GetStatistics(t *testing.T) {
	config := &PublisherConfig{
		Brokers:          []string{"localhost:9092"},
		Topic:            "test-topic",
		BatchSize:        100,
		BatchTimeout:     time.Second,
		CompressionCodec: "snappy",
		RequiredAcks:     -1,
	}

	logger := logger.NewDefault()
	metrics := metrics.New("test")

	publisher, err := NewPublisher(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewPublisher() failed: %v", err)
	}
	defer publisher.Stop()

	// Get initial statistics
	stats := publisher.GetStatistics()

	if stats == nil {
		t.Fatal("GetStatistics() returned nil")
	}

	if stats.SignalsPublished != 0 {
		t.Errorf("Expected 0 signals published, got %d", stats.SignalsPublished)
	}

	if stats.SignalsFailed != 0 {
		t.Errorf("Expected 0 signals failed, got %d", stats.SignalsFailed)
	}
}

func TestPublisher_ConvertSignalToEvent(t *testing.T) {
	config := &PublisherConfig{
		Brokers:          []string{"localhost:9092"},
		Topic:            "test-topic",
		BatchSize:        100,
		BatchTimeout:     time.Second,
		CompressionCodec: "snappy",
		RequiredAcks:     -1,
	}

	logger := logger.NewDefault()
	metrics := metrics.New("test")

	publisher, err := NewPublisher(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewPublisher() failed: %v", err)
	}
	defer publisher.Stop()

	book := bitso.NewBook(bitso.BTC, bitso.MXN)

	tests := []struct {
		name         string
		strategyName string
		signal       *strategies.TradingSignal
		wantSignal   string
		wantStrategy string
	}{
		{
			name:         "buy signal",
			strategyName: "mean_reversion_btc_mxn",
			signal: &strategies.TradingSignal{
				Type:      strategies.SignalBuy,
				Book:      book,
				Ticker:    nil,
				Amount:    0.5,
				Price:     1000.0,
				Reason:    "test buy",
				Timestamp: time.Now().Unix(),
			},
			wantSignal:   "BUY",
			wantStrategy: "mean_reversion_btc_mxn",
		},
		{
			name:         "sell signal",
			strategyName: "momentum_btc_mxn",
			signal: &strategies.TradingSignal{
				Type:      strategies.SignalSell,
				Book:      book,
				Ticker:    nil,
				Amount:    0.5,
				Price:     1000.0,
				Reason:    "test sell",
				Timestamp: time.Now().Unix(),
			},
			wantSignal:   "SELL",
			wantStrategy: "momentum_btc_mxn",
		},
		{
			name:         "hold signal",
			strategyName: "",
			signal: &strategies.TradingSignal{
				Type:      strategies.SignalHold,
				Book:      book,
				Ticker:    nil,
				Amount:    0,
				Price:     1000.0,
				Reason:    "test hold",
				Timestamp: time.Now().Unix(),
			},
			wantSignal:   "HOLD",
			wantStrategy: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := publisher.convertSignalToEvent(tt.strategyName, tt.signal)

			if event == nil {
				t.Fatal("convertSignalToEvent() returned nil")
			}

			if event.Signal != tt.wantSignal {
				t.Errorf("Expected signal type '%s', got '%s'", tt.wantSignal, event.Signal)
			}

			if event.Strategy != tt.wantStrategy {
				t.Errorf("Expected strategy '%s', got '%s'", tt.wantStrategy, event.Strategy)
			}

			if event.Book != book.String() {
				t.Errorf("Expected book '%s', got '%s'", book.String(), event.Book)
			}

			if event.Price != tt.signal.Price {
				t.Errorf("Expected price %f, got %f", tt.signal.Price, event.Price)
			}

			if event.Amount != tt.signal.Amount {
				t.Errorf("Expected amount %f, got %f", tt.signal.Amount, event.Amount)
			}

			if reason, ok := event.Metadata["reason"].(string); ok {
				if reason != tt.signal.Reason {
					t.Errorf("Expected reason '%s', got '%s'", tt.signal.Reason, reason)
				}
			} else {
				t.Error("Expected reason in metadata")
			}
		})
	}
}

func TestPublisherStatistics(t *testing.T) {
	stats := &PublisherStatistics{
		StartTime:         time.Now(),
		SignalsPublished:  100,
		SignalsFailed:     10,
		BytesPublished:    5000,
		LastPublishTime:   time.Now(),
		LastError:         "test error",
		SignalsByStrategy: make(map[string]int64),
		SignalsByType:     make(map[string]int64),
	}

	stats.SignalsByStrategy["basic"] = 50
	stats.SignalsByStrategy["trend"] = 50

	stats.SignalsByType["buy"] = 60
	stats.SignalsByType["sell"] = 40

	if stats.SignalsPublished != 100 {
		t.Errorf("Expected 100 signals published, got %d", stats.SignalsPublished)
	}

	if stats.SignalsFailed != 10 {
		t.Errorf("Expected 10 signals failed, got %d", stats.SignalsFailed)
	}

	if stats.BytesPublished != 5000 {
		t.Errorf("Expected 5000 bytes published, got %d", stats.BytesPublished)
	}

	if len(stats.SignalsByStrategy) != 2 {
		t.Errorf("Expected 2 strategies, got %d", len(stats.SignalsByStrategy))
	}

	if len(stats.SignalsByType) != 2 {
		t.Errorf("Expected 2 signal types, got %d", len(stats.SignalsByType))
	}
}

func TestPublisherConfig(t *testing.T) {
	config := &PublisherConfig{
		Brokers:          []string{"localhost:9092", "localhost:9093"},
		Topic:            "test-topic",
		BatchSize:        100,
		BatchTimeout:     time.Second,
		CompressionCodec: "snappy",
		RequiredAcks:     -1,
	}

	if len(config.Brokers) != 2 {
		t.Errorf("Expected 2 brokers, got %d", len(config.Brokers))
	}

	if config.Topic != "test-topic" {
		t.Errorf("Expected topic 'test-topic', got '%s'", config.Topic)
	}

	if config.BatchSize != 100 {
		t.Errorf("Expected batch size 100, got %d", config.BatchSize)
	}

	if config.BatchTimeout != time.Second {
		t.Errorf("Expected batch timeout 1s, got %v", config.BatchTimeout)
	}

	if config.CompressionCodec != "snappy" {
		t.Errorf("Expected compression codec 'snappy', got '%s'", config.CompressionCodec)
	}

	if config.RequiredAcks != -1 {
		t.Errorf("Expected required acks -1, got %d", config.RequiredAcks)
	}
}
