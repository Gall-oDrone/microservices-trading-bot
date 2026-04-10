package consumer

import (
	"context"
	"encoding/json"
	"time"

	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/manager"
	"bitso-trading-platform/shared/pkg/kafka"
	sharedModels "bitso-trading-platform/shared/pkg/models"
)

// SignalsConsumer consumes trading.signals and runs ProcessSignal so Redis has a row keyed by event_id before trading.orders.placed links Bitso OIDs.
// Producers should partition this topic by TradeSignalEvent.EventID (see strategy-executor) so related messages keep per-signal ordering across partitions.
type SignalsConsumer struct {
	consumer     *kafka.Consumer
	orderManager manager.OrderManager
	log          *logger.Logger
}

// NewSignalsConsumer creates a consumer for the trading signals topic.
func NewSignalsConsumer(
	brokers []string,
	topic string,
	groupID string,
	orderManager manager.OrderManager,
	log *logger.Logger,
	autoOffsetReset string,
) (*SignalsConsumer, error) {
	if len(brokers) == 0 || topic == "" || groupID == "" {
		return nil, nil
	}
	if autoOffsetReset == "" {
		autoOffsetReset = "earliest"
	}
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
	return &SignalsConsumer{
		consumer:     c,
		orderManager: orderManager,
		log:          log,
	}, nil
}

// Run consumes messages and processes trading signals until ctx is cancelled.
func (sc *SignalsConsumer) Run(ctx context.Context) {
	sc.log.Info("Trading-signals consumer started", map[string]interface{}{
		"topic": sc.consumer.Config().Topic,
	})
	for {
		select {
		case <-ctx.Done():
			sc.log.Info("Trading-signals consumer stopping", nil)
			return
		default:
			payload, err := sc.consumer.Consume(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				sc.log.Warn("Trading-signals consume error", map[string]interface{}{"error": err.Error()})
				time.Sleep(2 * time.Second)
				continue
			}
			var signal sharedModels.TradeSignalEvent
			if err := json.Unmarshal(payload, &signal); err != nil {
				sc.log.Warn("Invalid trading signal payload", map[string]interface{}{"error": err.Error()})
				continue
			}
			if signal.EventID == "" || signal.Book == "" || signal.Signal == "" {
				sc.log.Warn("Trading signal missing required fields", map[string]interface{}{
					"event_id": signal.EventID,
					"book":     signal.Book,
				})
				continue
			}
			procCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_, err = sc.orderManager.ProcessSignal(procCtx, &signal)
			cancel()
			if err != nil {
				sc.log.Warn("ProcessSignal failed", map[string]interface{}{
					"error":    err.Error(),
					"event_id": signal.EventID,
					"book":     signal.Book,
				})
				continue
			}
			sc.log.Info("Processed trading signal", map[string]interface{}{
				"event_id": signal.EventID,
				"book":     signal.Book,
				"signal":   signal.Signal,
			})
		}
	}
}

// Close closes the Kafka consumer.
func (sc *SignalsConsumer) Close() error {
	if sc.consumer != nil {
		return sc.consumer.Close()
	}
	return nil
}
