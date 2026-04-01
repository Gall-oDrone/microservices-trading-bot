package consumer

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/manager"
	"bitso-trading-platform/shared/pkg/kafka"
)

// OrderPlacedEvent matches the payload from trading-engine (trading.orders.placed)
type OrderPlacedEvent struct {
	OrderID  string  `json:"order_id"`
	EventID  string  `json:"event_id,omitempty"`
	Book     string  `json:"book"`
	Side     string  `json:"side"`
	Amount   float64 `json:"amount"`
	Price    float64 `json:"price"`
	Strategy string  `json:"strategy"`
}

// OrdersPlacedConsumer consumes trading.orders.placed and records orders in order-management
type OrdersPlacedConsumer struct {
	consumer    *kafka.Consumer
	orderManager manager.OrderManager
	log         *logger.Logger
}

// NewOrdersPlacedConsumer creates a consumer for the orders-placed topic
func NewOrdersPlacedConsumer(
	brokers []string,
	topic string,
	groupID string,
	orderManager manager.OrderManager,
	log *logger.Logger,
	autoOffsetReset string,
) (*OrdersPlacedConsumer, error) {
	if len(brokers) == 0 || topic == "" || groupID == "" {
		return nil, nil // disabled
	}
	if autoOffsetReset == "" {
		autoOffsetReset = "earliest"
	}
	// Low-traffic topic: long MaxWait/ReadBatchTimeout avoid broker fetch timeouts when idle at log end.
	// SessionTimeout/HeartbeatInterval reduce spurious group rebalances under load.
	// Do not wrap ReadMessage in a short outer context.Timeout — that cancels in-flight fetches and spams logs.
	cfg := &kafka.ConsumerConfig{
		Brokers:           brokers,
		Topic:             topic,
		GroupID:           groupID,
		AutoOffsetReset:   autoOffsetReset,
		MinBytes:          1,
		MaxWait:           30 * time.Second,
		ReadBatchTimeout:  90 * time.Second,
		SessionTimeout:    45 * time.Second,
		HeartbeatInterval: 9 * time.Second,
		CommitInterval:    1 * time.Second,
	}
	c, err := kafka.NewConsumer(cfg)
	if err != nil {
		return nil, err
	}
	return &OrdersPlacedConsumer{
		consumer:     c,
		orderManager: orderManager,
		log:          log,
	}, nil
}

// Run consumes messages and records placed orders. Stops when ctx is cancelled.
func (oc *OrdersPlacedConsumer) Run(ctx context.Context) {
	oc.log.Info("Orders-placed consumer started", map[string]interface{}{
		"topic": oc.consumer.Config().Topic,
	})
	for {
		select {
		case <-ctx.Done():
			oc.log.Info("Orders-placed consumer stopping", nil)
			return
		default:
			// Block on ctx until a message arrives or service shutdown (no short deadline — avoids cancelling kafka-go fetches).
			value, err := oc.consumer.Consume(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				oc.log.Warn("Orders-placed consume error", map[string]interface{}{"error": err.Error()})
				time.Sleep(2 * time.Second)
				continue
			}
			var evt OrderPlacedEvent
			if err := json.Unmarshal(value, &evt); err != nil {
				oc.log.Warn("Invalid orders-placed payload", map[string]interface{}{"error": err.Error(), "value": string(value)})
				continue
			}
			if evt.OrderID == "" || evt.Book == "" || evt.Side == "" {
				oc.log.Warn("Orders-placed missing required fields", map[string]interface{}{"order_id": evt.OrderID, "book": evt.Book})
				continue
			}
			// Skip dry-run placeholders
			if evt.OrderID == "dry-run" {
				continue
			}
			recordCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err = oc.orderManager.RecordOrderPlaced(recordCtx, evt.OrderID, evt.Book, strings.ToLower(evt.Side), evt.Amount, evt.Price, evt.Strategy, evt.EventID)
			cancel()
			if err != nil {
				oc.log.Warn("RecordOrderPlaced failed", map[string]interface{}{"error": err.Error(), "bitso_order_id": evt.OrderID})
			}
		}
	}
}

// Close closes the Kafka consumer
func (oc *OrdersPlacedConsumer) Close() error {
	if oc.consumer != nil {
		return oc.consumer.Close()
	}
	return nil
}
