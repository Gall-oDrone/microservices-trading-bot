package execution

import (
	"bitso-trading-platform/shared/pkg/bitso"
	"fmt"
	"log"
)

// SignalType represents the type of trading signal
type SignalType int

const (
	SignalNone SignalType = iota
	SignalBuy
	SignalSell
	SignalHold
)

// TradingSignal represents a trading signal received from strategy service
type TradingSignal struct {
	Type      SignalType
	Book      *bitso.Book
	Amount    float64
	Price     float64
	Reason    string
	Timestamp int64
}

// Executor is the interface that wraps the Execute method.
type Executor interface {
	ExecuteBuySignal(signal TradingSignal) error
	ExecuteSellSignal(signal TradingSignal) error
}

// BasicExecutor implements a basic trading execution
type BasicExecutor struct {
	bitsoClient *bitso.Client
	logger      *log.Logger
}

// NewBasicExecutor creates a new basic executor
func NewBasicExecutor(bitsoClient *bitso.Client) *BasicExecutor {
	logger := log.New(log.Writer(), "[EXECUTOR] ", log.LstdFlags|log.Lshortfile)
	return &BasicExecutor{
		bitsoClient: bitsoClient,
		logger:      logger,
	}
}

// ExecuteBuySignal handles buy signals
func (e *BasicExecutor) ExecuteBuySignal(signal TradingSignal) error {
	e.logger.Printf("Executing BUY signal for %s at price %.8f, amount: %.8f, reason: %s",
		signal.Book.String(), signal.Price, signal.Amount, signal.Reason)

	// Create order placement
	op := &bitso.OrderPlacement{
		Book:  *signal.Book,
		Side:  bitso.OrderSide(1), // Buy
		Type:  bitso.OrderTypeLimit,
		Major: bitso.ToMonetary(signal.Amount),
		Price: bitso.ToMonetary(signal.Price),
	}

	// Place the order
	oid, err := e.bitsoClient.PlaceOrder(op)
	if err != nil {
		return fmt.Errorf("failed to place buy order: %w", err)
	}

	e.logger.Printf("Successfully placed BUY order %s for %s", oid, signal.Book.String())
	return nil
}

// ExecuteSellSignal handles sell signals
func (e *BasicExecutor) ExecuteSellSignal(signal TradingSignal) error {
	e.logger.Printf("Executing SELL signal for %s at price %.8f, amount: %.8f, reason: %s",
		signal.Book.String(), signal.Price, signal.Amount, signal.Reason)

	// Create order placement
	op := &bitso.OrderPlacement{
		Book:  *signal.Book,
		Side:  bitso.OrderSide(2), // Sell
		Type:  bitso.OrderTypeLimit,
		Major: bitso.ToMonetary(signal.Amount),
		Price: bitso.ToMonetary(signal.Price),
	}

	// Place the order
	oid, err := e.bitsoClient.PlaceOrder(op)
	if err != nil {
		return fmt.Errorf("failed to place sell order: %w", err)
	}

	e.logger.Printf("Successfully placed SELL order %s for %s", oid, signal.Book.String())
	return nil
}
