package manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"bitso-trading-platform/order-management/internal/config"
	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/repository"
	"bitso-trading-platform/order-management/internal/risk"
	"bitso-trading-platform/order-management/internal/validator"
	sharedMetrics "bitso-trading-platform/shared/pkg/metrics"
	sharedModels "bitso-trading-platform/shared/pkg/models"
)

// OrderManager manages order lifecycle
type OrderManager interface {
	Start(ctx context.Context) error
	Stop() error
	ProcessSignal(ctx context.Context, signal *sharedModels.TradeSignalEvent) (*models.Order, error)
	CreateOrder(signal *sharedModels.TradeSignalEvent) (*models.Order, error)
	UpdateOrderStatus(ctx context.Context, orderID string, status models.OrderStatus, metadata map[string]interface{}) error
	CancelOrder(ctx context.Context, orderID string) error
	GetOrder(ctx context.Context, orderID string) (*models.Order, error)
	ListOrders(ctx context.Context, filters *models.OrderFilters) ([]*models.Order, error)
}

// Manager implements OrderManager
type Manager struct {
	logger       *logger.Logger
	config       *config.Config
	validator    validator.OrderValidator
	riskManager  risk.RiskManager
	repository   repository.OrderRepository
	stateMachine *StateMachine
	metrics      *metrics.MetricsCollector
	pnlRecorder  sharedMetrics.PnLRecorder // optional; nil disables intraday P&L recording

	// Internal state
	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.RWMutex
}

// NewOrderManager creates a new order manager. pnlRecorder is optional (nil disables intraday P&L metrics).
func NewOrderManager(
	config *config.Config,
	logger *logger.Logger,
	validator validator.OrderValidator,
	riskManager risk.RiskManager,
	repository repository.OrderRepository,
	metrics *metrics.MetricsCollector,
	pnlRecorder sharedMetrics.PnLRecorder,
) *Manager {
	return &Manager{
		logger:       logger,
		config:       config,
		validator:    validator,
		riskManager:  riskManager,
		repository:   repository,
		stateMachine: NewStateMachine(logger),
		metrics:      metrics,
		pnlRecorder:  pnlRecorder,
		stopChan:     make(chan struct{}),
	}
}

// Start starts the order manager
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting order manager...", nil)

	// Start background tasks
	m.wg.Add(1)
	go m.monitorOrders(ctx)

	m.logger.Info("Order manager started successfully", nil)
	return nil
}

// Stop stops the order manager
func (m *Manager) Stop() error {
	m.logger.Info("Stopping order manager...", nil)

	close(m.stopChan)
	m.wg.Wait()

	m.logger.Info("Order manager stopped", nil)
	return nil
}

// ProcessSignal processes a trading signal and creates an order
func (m *Manager) ProcessSignal(ctx context.Context, signal *sharedModels.TradeSignalEvent) (*models.Order, error) {
	start := time.Now()
	defer func() {
		m.metrics.RecordOrderDuration("signal_processing", time.Since(start))
	}()

	m.logger.Debug("Processing trading signal", map[string]interface{}{
		"signal_id": signal.EventID,
		"book":      signal.Book,
		"signal":    signal.Signal,
	})

	// Validate signal
	if err := m.validator.ValidateSignal(signal); err != nil {
		m.logger.Warn("Signal validation failed", map[string]interface{}{
			"signal_id": signal.EventID,
			"error":     err.Error(),
		})
		return nil, fmt.Errorf("signal validation failed: %w", err)
	}

	// Create order from signal
	order, err := m.CreateOrder(signal)
	if err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	// Validate order
	validationStart := time.Now()
	if err := m.validator.ValidateOrder(order); err != nil {
		m.logger.Warn("Order validation failed", map[string]interface{}{
			"order_id": order.ID,
			"error":    err.Error(),
		})

		// Mark as rejected
		order.Reject(err.Error())
		m.repository.Update(ctx, order)
		m.metrics.RecordOrderRejected(order.Book, order.Strategy, "validation_failed")

		return nil, fmt.Errorf("order validation failed: %w", err)
	}
	m.metrics.RecordValidationDuration("order", time.Since(validationStart))

	// Check risk
	riskStart := time.Now()
	if err := m.riskManager.CheckRisk(ctx, order); err != nil {
		m.logger.Warn("Risk check failed", map[string]interface{}{
			"order_id": order.ID,
			"error":    err.Error(),
		})

		// Mark as rejected
		order.Reject(err.Error())
		m.repository.Update(ctx, order)
		m.metrics.RecordOrderRejected(order.Book, order.Strategy, "risk_check_failed")

		return nil, fmt.Errorf("risk check failed: %w", err)
	}
	m.metrics.RecordRiskCheck("order", "passed")
	m.logger.Debug("Risk check passed", map[string]interface{}{
		"order_id": order.ID,
		"duration": time.Since(riskStart),
	})

	// Transition to validated
	if err := m.stateMachine.Transition(order, models.OrderStatusValidated); err != nil {
		return nil, fmt.Errorf("failed to transition to validated: %w", err)
	}

	// Update order in repository
	if err := m.repository.Update(ctx, order); err != nil {
		return nil, fmt.Errorf("failed to update order: %w", err)
	}

	m.logger.Info("Order validated successfully", map[string]interface{}{
		"order_id": order.ID,
		"book":     order.Book,
		"side":     order.Side,
		"amount":   order.Amount,
		"duration": time.Since(start),
	})

	return order, nil
}

// CreateOrder creates an order from a trading signal
func (m *Manager) CreateOrder(signal *sharedModels.TradeSignalEvent) (*models.Order, error) {
	start := time.Now()

	// Determine order side from signal
	side := "buy"
	if signal.Signal == "SELL" {
		side = "sell"
	}

	// Determine order type (for now, always limit orders)
	orderType := "limit"

	// Create order
	order := models.NewOrder(
		signal.EventID,
		signal.Book,
		side,
		orderType,
		signal.Strategy,
		signal.Price,
		signal.Amount,
	)

	// Add signal metadata
	if signal.Metadata != nil {
		order.Metadata["signal_metadata"] = signal.Metadata
	}
	order.Metadata["signal_timestamp"] = signal.Timestamp

	// Save to repository
	if err := m.repository.Create(context.Background(), order); err != nil {
		return nil, fmt.Errorf("failed to save order: %w", err)
	}

	m.metrics.RecordOrderCreated(order.Book, order.Strategy)
	m.metrics.RecordOrderDuration("creation", time.Since(start))

	m.logger.Info("Order created from signal", map[string]interface{}{
		"order_id":  order.ID,
		"signal_id": signal.EventID,
		"book":      order.Book,
		"side":      order.Side,
		"amount":    order.Amount,
	})

	return order, nil
}

// UpdateOrderStatus updates an order's status
func (m *Manager) UpdateOrderStatus(ctx context.Context, orderID string, status models.OrderStatus, metadata map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get order
	order, err := m.repository.Get(ctx, orderID)
	if err != nil {
		return fmt.Errorf("failed to get order: %w", err)
	}

	// Validate transition
	if err := m.stateMachine.Transition(order, status); err != nil {
		return fmt.Errorf("invalid state transition: %w", err)
	}

	// Add metadata if provided
	if metadata != nil {
		for k, v := range metadata {
			order.Metadata[k] = v
		}
	}

	// Update in repository
	if err := m.repository.Update(ctx, order); err != nil {
		return fmt.Errorf("failed to update order: %w", err)
	}

	// Record metrics based on new status
	switch status {
	case models.OrderStatusFilled:
		m.metrics.RecordOrderFilled(order.Book, order.Strategy)
		m.recordTradeClosedForIntraday(order, metadata)
	case models.OrderStatusCancelled:
		m.metrics.RecordOrderCancelled(order.Book, order.Strategy, "manual")
	case models.OrderStatusRejected:
		m.metrics.RecordOrderRejected(order.Book, order.Strategy, "manual")
	}

	m.logger.Info("Order status updated", map[string]interface{}{
		"order_id":   orderID,
		"new_status": status,
	})

	return nil
}

// CancelOrder cancels an order
func (m *Manager) CancelOrder(ctx context.Context, orderID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get order
	order, err := m.repository.Get(ctx, orderID)
	if err != nil {
		return fmt.Errorf("failed to get order: %w", err)
	}

	// Check if cancellation is allowed
	if !m.stateMachine.CanCancel(order.Status) {
		return fmt.Errorf("cannot cancel order in status: %s", order.Status)
	}

	// Transition to cancelled
	if err := m.stateMachine.Transition(order, models.OrderStatusCancelled); err != nil {
		return fmt.Errorf("failed to transition to cancelled: %w", err)
	}

	// Update in repository
	if err := m.repository.Update(ctx, order); err != nil {
		return fmt.Errorf("failed to update order: %w", err)
	}

	m.metrics.RecordOrderCancelled(order.Book, order.Strategy, "user_requested")

	m.logger.Info("Order cancelled", map[string]interface{}{
		"order_id": orderID,
	})

	return nil
}

// GetOrder retrieves an order by ID
func (m *Manager) GetOrder(ctx context.Context, orderID string) (*models.Order, error) {
	return m.repository.Get(ctx, orderID)
}

// ListOrders lists orders with optional filters
func (m *Manager) ListOrders(ctx context.Context, filters *models.OrderFilters) ([]*models.Order, error) {
	return m.repository.List(ctx, filters)
}

// monitorOrders monitors active orders (background task)
func (m *Manager) monitorOrders(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			m.logger.Debug("Order monitoring stopped", nil)
			return
		case <-ctx.Done():
			m.logger.Debug("Order monitoring context cancelled", nil)
			return
		case <-ticker.C:
			m.updateActiveOrderMetrics(ctx)
		}
	}
}

// recordTradeClosedForIntraday records a closed trade to the P&L recorder when present.
// Realized PnL is read from metadata["realized_pnl"] (float64) when provided by position/fill sync; else 0.
func (m *Manager) recordTradeClosedForIntraday(order *models.Order, metadata map[string]interface{}) {
	if m.pnlRecorder == nil {
		return
	}
	currency := quoteCurrencyFromBook(order.Book)
	var realizedPnL decimal.Decimal
	if metadata != nil {
		if v, ok := metadata["realized_pnl"]; ok {
			switch t := v.(type) {
			case float64:
				realizedPnL = decimal.NewFromFloat(t)
			case float32:
				realizedPnL = decimal.NewFromFloat(float64(t))
			case int:
				realizedPnL = decimal.NewFromInt(int64(t))
			case int64:
				realizedPnL = decimal.NewFromInt(t)
			}
		}
	}
	outcome := sharedMetrics.TradeOutcome{
		Book:        order.Book,
		Strategy:    order.Strategy,
		Currency:    currency,
		RealizedPnL: sharedMetrics.NewMonetaryAmount(realizedPnL, currency),
		IsWin:       realizedPnL.GreaterThan(decimal.Zero),
	}
	m.pnlRecorder.RecordTradeClosed(outcome)
}

// quoteCurrencyFromBook returns the quote currency for metrics (e.g. btc_mxn -> MXN).
func quoteCurrencyFromBook(book string) string {
	// Bitso books are like btc_mxn, eth_mxn, xrp_mxn; default to MXN
	if len(book) >= 4 && book[len(book)-3:] == "mxn" {
		return "MXN"
	}
	if len(book) >= 4 && book[len(book)-3:] == "usd" {
		return "USD"
	}
	return "MXN"
}

// updateActiveOrderMetrics updates metrics for active orders
func (m *Manager) updateActiveOrderMetrics(ctx context.Context) {
	activeOrders, err := m.repository.GetActiveOrders(ctx)
	if err != nil {
		m.logger.Error("Failed to get active orders", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	// Count by book
	bookCounts := make(map[string]int)
	for _, order := range activeOrders {
		bookCounts[order.Book]++
	}

	// Update metrics
	for book, count := range bookCounts {
		m.metrics.SetActiveOrders(book, float64(count))
	}
}
