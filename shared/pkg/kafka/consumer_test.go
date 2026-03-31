package kafka

import (
	"testing"
	"time"
)

func TestNewConsumer_DefaultFetchTuning(t *testing.T) {
	c, err := NewConsumer(&ConsumerConfig{
		Brokers:         []string{"127.0.0.1:9092"},
		Topic:           "test.topic",
		GroupID:         "test-group",
		AutoOffsetReset: "latest",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	rc := c.Config()
	if rc.MinBytes != 1 {
		t.Errorf("MinBytes: got %d want 1", rc.MinBytes)
	}
	if rc.MaxWait != 10*time.Second {
		t.Errorf("MaxWait: got %v want 10s", rc.MaxWait)
	}
}
