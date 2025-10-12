package kafka

import (
	"context"
	"fmt"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

// ProducerConfig holds configuration for Kafka producer
type ProducerConfig struct {
	Brokers          []string      // List of Kafka broker addresses
	Topic            string        // Topic to produce to
	BatchSize        int           // Max messages per batch (default: 100)
	BatchTimeout     time.Duration // Max time to wait before sending batch (default: 1s)
	CompressionCodec string        // Compression codec: "none", "gzip", "snappy", "lz4", "zstd" (default: "snappy")
	RequiredAcks     int           // Required acknowledgments: -1=all, 0=none, 1=leader (default: -1)
	MaxAttempts      int           // Max retry attempts (default: 10)
	WriteTimeout     time.Duration // Timeout for write operations (default: 10s)
}

// Producer wraps kafka-go Writer with additional functionality
type Producer struct {
	writer *kafka.Writer
	config *ProducerConfig
}

// NewProducer creates a new Kafka producer
func NewProducer(config *ProducerConfig) (*Producer, error) {
	if config == nil {
		return nil, fmt.Errorf("producer config cannot be nil")
	}

	if len(config.Brokers) == 0 {
		return nil, fmt.Errorf("at least one broker is required")
	}

	if config.Topic == "" {
		return nil, fmt.Errorf("topic is required")
	}

	// Set defaults
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.BatchTimeout == 0 {
		config.BatchTimeout = 1 * time.Second
	}
	if config.CompressionCodec == "" {
		config.CompressionCodec = "snappy"
	}
	if config.RequiredAcks == 0 {
		config.RequiredAcks = -1 // All in-sync replicas
	}
	if config.MaxAttempts == 0 {
		config.MaxAttempts = 10
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 10 * time.Second
	}

	// Map compression codec
	var compression kafka.Compression
	switch config.CompressionCodec {
	case "none":
		compression = kafka.Compression(0)
	case "gzip":
		compression = kafka.Gzip
	case "snappy":
		compression = kafka.Snappy
	case "lz4":
		compression = kafka.Lz4
	case "zstd":
		compression = kafka.Zstd
	default:
		compression = kafka.Snappy
	}

	// Create writer
	writer := &kafka.Writer{
		Addr:         kafka.TCP(config.Brokers...),
		Topic:        config.Topic,
		Balancer:     &kafka.LeastBytes{}, // Use least bytes balancing
		BatchSize:    config.BatchSize,
		BatchTimeout: config.BatchTimeout,
		Compression:  compression,
		RequiredAcks: kafka.RequiredAcks(config.RequiredAcks),
		MaxAttempts:  config.MaxAttempts,
		WriteTimeout: config.WriteTimeout,
		Async:        false, // Synchronous by default
	}

	return &Producer{
		writer: writer,
		config: config,
	}, nil
}

// Produce sends a message to Kafka
func (p *Producer) Produce(ctx context.Context, key, value []byte) error {
	msg := kafka.Message{
		Key:   key,
		Value: value,
		Time:  time.Now(),
	}

	return p.writer.WriteMessages(ctx, msg)
}

// ProduceMessage sends a kafka.Message to Kafka
func (p *Producer) ProduceMessage(ctx context.Context, msg kafka.Message) error {
	if msg.Time.IsZero() {
		msg.Time = time.Now()
	}
	return p.writer.WriteMessages(ctx, msg)
}

// ProduceMessages sends multiple messages to Kafka
func (p *Producer) ProduceMessages(ctx context.Context, messages ...kafka.Message) error {
	for i := range messages {
		if messages[i].Time.IsZero() {
			messages[i].Time = time.Now()
		}
	}
	return p.writer.WriteMessages(ctx, messages...)
}

// Close closes the producer and releases resources
func (p *Producer) Close() error {
	if p.writer != nil {
		return p.writer.Close()
	}
	return nil
}

// Stats returns producer statistics
func (p *Producer) Stats() kafka.WriterStats {
	return p.writer.Stats()
}
