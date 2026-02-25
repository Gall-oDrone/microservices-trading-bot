package export

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/segmentio/kafka-go"
)

// KafkaNotifier produces completion events to a Kafka topic
type KafkaNotifier struct {
	writer *kafka.Writer
	topic  string
}

// NewKafkaNotifier creates a Kafka notifier. Brokers is comma-separated (e.g. "localhost:9092").
func NewKafkaNotifier(brokers, topic string) *KafkaNotifier {
	brokerList := strings.Split(strings.TrimSpace(brokers), ",")
	for i := range brokerList {
		brokerList[i] = strings.TrimSpace(brokerList[i])
	}
	writer := &kafka.Writer{
		Addr:     kafka.TCP(brokerList...),
		Topic:    topic,
		Balancer: &kafka.LeastBytes{},
	}
	return &KafkaNotifier{writer: writer, topic: topic}
}

// Notify writes the event as a Kafka message (value = JSON)
func (k *KafkaNotifier) Notify(ctx context.Context, event *BacktestCompletionEvent) error {
	value, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	err = k.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(event.BacktestID),
		Value: value,
	})
	if err != nil {
		return fmt.Errorf("kafka write: %w", err)
	}
	return nil
}

// Name returns the notifier name
func (k *KafkaNotifier) Name() string {
	return "kafka"
}

// Close closes the Kafka writer
func (k *KafkaNotifier) Close() error {
	return k.writer.Close()
}
