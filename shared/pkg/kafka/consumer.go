package kafka

import (
	"context"
	"fmt"
	"log"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

// ConsumerConfig holds configuration for Kafka consumer
type ConsumerConfig struct {
	Brokers         []string      // List of Kafka broker addresses
	Topic           string        // Topic to consume from
	GroupID         string        // Consumer group ID
	AutoOffsetReset string        // Where to start reading: "earliest" or "latest" (default: "latest")
	MinBytes        int           // Min bytes to fetch per request (default: 10KB)
	MaxBytes        int           // Max bytes to fetch per request (default: 10MB)
	MaxWait         time.Duration // Max time to wait for min bytes (default: 500ms)
	CommitInterval  time.Duration // How often to commit offsets (default: 1s)
	StartOffset     int64         // Starting offset (optional, -1=latest, -2=earliest)
	Logger          *log.Logger   // Optional logger
}

// Consumer wraps kafka-go Reader with additional functionality
type Consumer struct {
	reader *kafka.Reader
	config *ConsumerConfig
	logger *log.Logger
}

// NewConsumer creates a new Kafka consumer
func NewConsumer(config *ConsumerConfig) (*Consumer, error) {
	if config == nil {
		return nil, fmt.Errorf("consumer config cannot be nil")
	}

	if len(config.Brokers) == 0 {
		return nil, fmt.Errorf("at least one broker is required")
	}

	if config.Topic == "" {
		return nil, fmt.Errorf("topic is required")
	}

	if config.GroupID == "" {
		return nil, fmt.Errorf("group ID is required")
	}

	// Set defaults
	if config.AutoOffsetReset == "" {
		config.AutoOffsetReset = "latest"
	}
	if config.MinBytes == 0 {
		config.MinBytes = 10e3 // 10KB
	}
	if config.MaxBytes == 0 {
		config.MaxBytes = 10e6 // 10MB
	}
	if config.MaxWait == 0 {
		config.MaxWait = 500 * time.Millisecond
	}
	if config.CommitInterval == 0 {
		config.CommitInterval = 1 * time.Second
	}

	// Map offset reset to kafka-go constants
	startOffset := kafka.LastOffset // Default to latest
	if config.AutoOffsetReset == "earliest" {
		startOffset = kafka.FirstOffset
	}
	if config.StartOffset != 0 {
		startOffset = config.StartOffset
	}

	// Create logger if not provided
	logger := config.Logger
	if logger == nil {
		logger = log.New(log.Writer(), "[KAFKA-CONSUMER] ", log.LstdFlags)
	}

	// Create reader configuration
	readerConfig := kafka.ReaderConfig{
		Brokers:        config.Brokers,
		Topic:          config.Topic,
		GroupID:        config.GroupID,
		MinBytes:       config.MinBytes,
		MaxBytes:       config.MaxBytes,
		MaxWait:        config.MaxWait,
		CommitInterval: config.CommitInterval,
		StartOffset:    startOffset,
		Logger:         logger,
		ErrorLogger:    logger,
	}

	// Create reader
	reader := kafka.NewReader(readerConfig)

	return &Consumer{
		reader: reader,
		config: config,
		logger: logger,
	}, nil
}

// Consume reads a single message from Kafka
// Returns the message value as bytes
func (c *Consumer) Consume(ctx context.Context) ([]byte, error) {
	msg, err := c.reader.ReadMessage(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read message: %w", err)
	}

	return msg.Value, nil
}

// ConsumeMessage reads a single message from Kafka
// Returns the full kafka.Message with metadata
func (c *Consumer) ConsumeMessage(ctx context.Context) (kafka.Message, error) {
	msg, err := c.reader.ReadMessage(ctx)
	if err != nil {
		return kafka.Message{}, fmt.Errorf("failed to read message: %w", err)
	}

	return msg, nil
}

// FetchMessage fetches a message without committing the offset
// This allows you to process the message and commit manually
func (c *Consumer) FetchMessage(ctx context.Context) (kafka.Message, error) {
	msg, err := c.reader.FetchMessage(ctx)
	if err != nil {
		return kafka.Message{}, fmt.Errorf("failed to fetch message: %w", err)
	}

	return msg, nil
}

// CommitMessage commits the offset for a specific message
func (c *Consumer) CommitMessage(ctx context.Context, msg kafka.Message) error {
	return c.reader.CommitMessages(ctx, msg)
}

// CommitMessages commits offsets for multiple messages
func (c *Consumer) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	return c.reader.CommitMessages(ctx, msgs...)
}

// Close closes the consumer and releases resources
func (c *Consumer) Close() error {
	if c.reader != nil {
		return c.reader.Close()
	}
	return nil
}

// Stats returns consumer statistics
func (c *Consumer) Stats() kafka.ReaderStats {
	return c.reader.Stats()
}

// Lag returns the current consumer lag (difference between latest offset and consumer offset)
func (c *Consumer) Lag() (int64, error) {
	stats := c.reader.Stats()
	return stats.Lag, nil
}

// SetOffset sets the consumer offset to a specific value
// partition: partition number, offset: the offset to seek to
func (c *Consumer) SetOffset(offset int64) error {
	return c.reader.SetOffset(offset)
}

// ReadLag returns the lag for all partitions
func (c *Consumer) ReadLag(ctx context.Context) (int64, error) {
	lag, err := c.reader.ReadLag(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to read lag: %w", err)
	}
	return lag, nil
}

// Config returns the consumer configuration
func (c *Consumer) Config() kafka.ReaderConfig {
	return c.reader.Config()
}
