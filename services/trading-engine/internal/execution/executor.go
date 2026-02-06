package execution

import (
	"context"
	"fmt"
	"log"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
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

// SessionRiskProvider returns current session risk metrics (e.g. from order-management).
// Used to enforce MaxDailyLoss and MaxDrawdownPct before placing orders.
type SessionRiskProvider interface {
	GetSessionRisk(ctx context.Context) (dailyRealizedPnL, drawdownPct float64, err error)
}

// Executor is the interface for order execution and session risk checks.
// Execute methods return the Bitso order ID on success (or "dry-run" when dry-run mode).
type Executor interface {
	ExecuteBuySignal(signal TradingSignal) (orderID string, err error)
	ExecuteSellSignal(signal TradingSignal) (orderID string, err error)
	CheckSessionLimits(dailyRealizedPnL, drawdownPct float64) error
}

// BasicExecutor implements a basic trading execution
type BasicExecutor struct {
	bitsoClient *bitso.Client
	config      *models.TradingConfig
	logger      *log.Logger
	dryRun      bool // if true, log order but do not call PlaceOrder
}

// NewBasicExecutor creates a new basic executor. If dryRun is true, orders are logged only and PlaceOrder is not called.
// config can be nil; if non-nil, MaxDailyLoss and MaxDrawdownPct are used in CheckSessionLimits.
func NewBasicExecutor(bitsoClient *bitso.Client, config *models.TradingConfig, dryRun bool) *BasicExecutor {
	logger := log.New(log.Writer(), "[EXECUTOR] ", log.LstdFlags|log.Lshortfile)
	return &BasicExecutor{
		bitsoClient: bitsoClient,
		config:      config,
		logger:      logger,
		dryRun:      dryRun,
	}
}

// CheckSessionLimits returns an error if daily realized P&L or drawdown exceed configured limits (MaxDailyLoss, MaxDrawdownPct).
// Pass the current session values from order-management or 0,0 to skip. 0 limits mean disabled.
func (e *BasicExecutor) CheckSessionLimits(dailyRealizedPnL, drawdownPct float64) error {
	if e.config == nil {
		return nil
	}
	if e.config.MaxDailyLoss > 0 && dailyRealizedPnL <= -e.config.MaxDailyLoss {
		return fmt.Errorf("daily loss limit exceeded: realized P&L %.2f <= -%.2f", dailyRealizedPnL, e.config.MaxDailyLoss)
	}
	if e.config.MaxDrawdownPct > 0 && drawdownPct >= e.config.MaxDrawdownPct {
		return fmt.Errorf("max drawdown exceeded: %.2f%% >= %.2f%%", drawdownPct, e.config.MaxDrawdownPct)
	}
	return nil
}

// ExecuteBuySignal handles buy signals
func (e *BasicExecutor) ExecuteBuySignal(signal TradingSignal) (string, error) {
	e.logger.Printf("Executing BUY signal for %s at price %.8f, amount: %.8f, reason: %s",
		signal.Book.String(), signal.Price, signal.Amount, signal.Reason)

	if e.dryRun {
		e.logger.Printf("[DRY-RUN] Would place BUY order for %s amount %.8f price %.8f (no Bitso API call)", signal.Book.String(), signal.Amount, signal.Price)
		return "dry-run", nil
	}

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
		return "", fmt.Errorf("failed to place buy order: %w", err)
	}

	e.logger.Printf("Successfully placed BUY order %s for %s", oid, signal.Book.String())
	return oid, nil
}

// ExecuteSellSignal handles sell signals
func (e *BasicExecutor) ExecuteSellSignal(signal TradingSignal) (string, error) {
	e.logger.Printf("Executing SELL signal for %s at price %.8f, amount: %.8f, reason: %s",
		signal.Book.String(), signal.Price, signal.Amount, signal.Reason)

	if e.dryRun {
		e.logger.Printf("[DRY-RUN] Would place SELL order for %s amount %.8f price %.8f (no Bitso API call)", signal.Book.String(), signal.Amount, signal.Price)
		return "dry-run", nil
	}

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
		return "", fmt.Errorf("failed to place sell order: %w", err)
	}

	e.logger.Printf("Successfully placed SELL order %s for %s", oid, signal.Book.String())
	return oid, nil
}
