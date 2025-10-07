package signals

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"bitso-trading-platform/shared/pkg/models"
)

// SignalProcessor processes trading signals and converts them to events
type SignalProcessor struct {
	logger *log.Logger
}

// NewSignalProcessor creates a new signal processor
func NewSignalProcessor() *SignalProcessor {
	return &SignalProcessor{
		logger: log.New(log.Writer(), "[SIGNAL-PROCESSOR] ", log.LstdFlags|log.Lshortfile),
	}
}

// ProcessBuySignal converts a buy signal to a trade signal event
func (sp *SignalProcessor) ProcessBuySignal(book, strategy, reason string, price, amount float64) (*models.TradeSignalEvent, error) {
	event := &models.TradeSignalEvent{
		EventID:   fmt.Sprintf("buy-%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(),
		Book:      book,
		Strategy:  strategy,
		Signal:    "BUY",
		Price:     price,
		Amount:    amount,
		Metadata: map[string]interface{}{
			"reason": reason,
		},
	}

	sp.logger.Printf("Processed BUY signal: book=%s, price=%.2f, amount=%.8f", book, price, amount)
	return event, nil
}

// ProcessSellSignal converts a sell signal to a trade signal event
func (sp *SignalProcessor) ProcessSellSignal(book, strategy, reason string, price, amount float64) (*models.TradeSignalEvent, error) {
	event := &models.TradeSignalEvent{
		EventID:   fmt.Sprintf("sell-%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(),
		Book:      book,
		Strategy:  strategy,
		Signal:    "SELL",
		Price:     price,
		Amount:    amount,
		Metadata: map[string]interface{}{
			"reason": reason,
		},
	}

	sp.logger.Printf("Processed SELL signal: book=%s, price=%.2f, amount=%.8f", book, price, amount)
	return event, nil
}

// SerializeEvent converts a trade signal event to JSON
func (sp *SignalProcessor) SerializeEvent(event *models.TradeSignalEvent) ([]byte, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize event: %w", err)
	}
	return data, nil
}
