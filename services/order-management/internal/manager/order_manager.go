package manager

import (
	"context"
	"fmt"
	"strings"
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
	"bitso-trading-platform/shared/pkg/bitso"
	sharedMetrics "bitso-trading-platform/shared/pkg/metrics"
	sharedModels "bitso-trading-platform/shared/pkg/models"
)

// OrderManager manages order lifecycle
type OrderManager interface {
	Start(ctx context.Context) error
	Stop() error
	ProcessSignal(ctx context.Context, signal *sharedModels.TradeSignalEvent) (*models.Order, error)
	CreateOrder(signal *sharedModels.TradeSignalEvent) (*models.Order, error)
	// SyncOrderFromBitso updates an order from Bitso (used by Bitso sync job).
	SyncOrderFromBitso(ctx context.Context, bitsoOrderID string, filledAmount, avgPrice float64, status models.OrderStatus) error
	// RecordOrderPlaced records an order placed by the trading-engine (from Kafka trading.orders.placed). Idempotent.
	// signalEventID must match TradeSignalEvent.EventID when present so the existing signal-created order is linked (metadata bitso_order_id) instead of duplicating rows.
	RecordOrderPlaced(ctx context.Context, bitsoOrderID, book, side string, amount, price float64, strategy, signalEventID string) (*models.Order, error)
	UpdateOrderStatus(ctx context.Context, orderID string, status models.OrderStatus, metadata map[string]interface{}) error
	CancelOrder(ctx context.Context, orderID string) error
	GetOrder(ctx context.Context, orderID string) (*models.Order, error)
	ListOrders(ctx context.Context, filters *models.OrderFilters) ([]*models.Order, error)
	GetPositionSummary(ctx context.Context) (*models.PositionSummary, error)
	// ListActiveBitsoOrderIDs returns Bitso order IDs for active orders (for sync job)
	ListActiveBitsoOrderIDs(ctx context.Context) ([]string, error)
}

// Manager implements OrderManager
type Manager struct {
	logger       *logger.Logger
	config       *config.Config
	validator    validator.OrderValidator
	riskManager  risk.RiskManager
	repository   repository.OrderRepository
	positionRepo repository.PositionRepository
	stateMachine *StateMachine
	metrics      *metrics.MetricsCollector
	pnlRecorder  sharedMetrics.PnLRecorder // optional; nil disables intraday P&L recording
	fillLedger   repository.FillLedger     // optional; nil skips append-only fill audit

	// Internal state
	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.RWMutex

	// activeBooksMu guards lastBooksWithNonZeroActive for Prometheus gauge resets.
	activeBooksMu              sync.Mutex
	lastBooksWithNonZeroActive map[string]struct{}

	// repositoryActiveOrdersGauge: when true, orders_active is derived from the local repo (10s ticker).
	// When false (Bitso sync enabled), orders_active is set from Bitso /open_orders only — avoids Grafana
	// disagreeing with the Stage dashboard when OM has not ingested an order yet.
	repositoryActiveOrdersGauge bool

	// orderFillPublisher emits OrderFillEvent when an order first becomes fully filled (optional).
	orderFillPublisher func(context.Context, *sharedModels.OrderFillEvent) error
}

// NewOrderManager creates a new order manager. pnlRecorder is optional (nil disables intraday P&L metrics).
// fillLedger is optional (nil skips Redis/in-memory fill ledger append).
// repositoryActiveOrdersGauge: pass true if Bitso sync is not used; pass false when Bitso sync updates orders_active from /open_orders.
func NewOrderManager(
	config *config.Config,
	logger *logger.Logger,
	validator validator.OrderValidator,
	riskManager risk.RiskManager,
	repository repository.OrderRepository,
	positionRepo repository.PositionRepository,
	metrics *metrics.MetricsCollector,
	pnlRecorder sharedMetrics.PnLRecorder,
	fillLedger repository.FillLedger,
	repositoryActiveOrdersGauge bool,
) *Manager {
	return &Manager{
		logger:       logger,
		config:       config,
		validator:    validator,
		riskManager:  riskManager,
		repository:   repository,
		positionRepo: positionRepo,
		stateMachine: NewStateMachine(logger),
		metrics:      metrics,
		pnlRecorder:  pnlRecorder,
		fillLedger:   fillLedger,
		stopChan:     make(chan struct{}),

		lastBooksWithNonZeroActive:  make(map[string]struct{}),
		repositoryActiveOrdersGauge: repositoryActiveOrdersGauge,
	}
}

// SetOrderFillPublisher registers a callback to publish full fills (e.g. to Kafka for strategy-executor).
// Call once during startup before concurrent sync traffic.
func (m *Manager) SetOrderFillPublisher(fn func(context.Context, *sharedModels.OrderFillEvent) error) {
	m.orderFillPublisher = fn
}

// PublishOrderFillForTest invokes the configured order-fill Kafka publisher (same code path as a real fill).
// Used only when OM_DEV_ORDER_FILL_PUBLISH_TEST_ENABLED enables the dev HTTP route.
func (m *Manager) PublishOrderFillForTest(ctx context.Context, ev *sharedModels.OrderFillEvent) error {
	if m.orderFillPublisher == nil {
		return fmt.Errorf("order fill publisher not configured")
	}
	return m.orderFillPublisher(ctx, ev)
}

func (m *Manager) maybePublishOrderFill(ctx context.Context, order *models.Order, preStatus models.OrderStatus) {
	if m.orderFillPublisher == nil {
		return
	}
	if order.Status != models.OrderStatusFilled || preStatus == models.OrderStatusFilled {
		return
	}
	if order.SignalID == "" {
		return
	}
	ev := &sharedModels.OrderFillEvent{
		EventID:      order.SignalID,
		OrderID:      order.ID,
		TimestampMs:  time.Now().UnixMilli(),
		Book:         order.Book,
		Side:         order.Side,
		AveragePrice: order.AveragePrice,
		FilledAmount: order.FilledAmount,
		Strategy:     order.Strategy,
	}
	if order.Metadata != nil {
		if v, ok := order.Metadata["fill_liquidity"].(string); ok && v != "" {
			ev.Liquidity = v
		} else if v, ok := order.Metadata["liquidity"].(string); ok && v != "" {
			ev.Liquidity = v
		}
	}
	if err := m.orderFillPublisher(ctx, ev); err != nil {
		m.logger.Warn("order fill publish failed", map[string]interface{}{
			"order_id":  order.ID,
			"signal_id": order.SignalID,
			"error":     err.Error(),
		})
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

	// Idempotent replay: same Kafka signal delivered twice should not create a second order.
	if existing, err := m.repository.GetBySignalID(ctx, signal.EventID); err == nil && existing != nil {
		m.logger.Debug("Signal already processed", map[string]interface{}{
			"signal_id": signal.EventID,
			"order_id":  existing.ID,
		})
		return existing, nil
	} else if err != nil && !strings.Contains(err.Error(), "not found") {
		return nil, fmt.Errorf("get by signal: %w", err)
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

// RecordOrderPlaced records an order placed by the trading-engine (consumed from Kafka trading.orders.placed).
// Idempotent: if an order with this bitso_order_id already exists, returns it without error.
// When signalEventID matches an existing order (same as TradeSignalEvent.EventID), attaches bitso_order_id to that row
// so Bitso sync (ListActiveBitsoOrderIDs) and fill metrics work. Otherwise creates a standalone order (legacy / tests).
func (m *Manager) RecordOrderPlaced(ctx context.Context, bitsoOrderID, book, side string, amount, price float64, strategy, signalEventID string) (*models.Order, error) {
	existing, err := m.repository.GetByBitsoOrderID(ctx, bitsoOrderID)
	if err == nil && existing != nil {
		m.logger.Debug("Order already recorded for bitso_order_id", map[string]interface{}{"bitso_order_id": bitsoOrderID})
		return existing, nil
	}

	if signalEventID != "" {
		// Trading-engine may publish trading.orders.placed before this service finishes ProcessSignal for the same event_id.
		var bySignal *models.Order
		var sigErr error
		for attempt := 0; attempt < 60; attempt++ {
			bySignal, sigErr = m.repository.GetBySignalID(ctx, signalEventID)
			if sigErr == nil && bySignal != nil {
				break
			}
			if sigErr != nil && !strings.Contains(sigErr.Error(), "not found") {
				return nil, fmt.Errorf("get order by signal for placement: %w", sigErr)
			}
			if attempt < 59 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(50 * time.Millisecond):
				}
			}
		}
		if sigErr == nil && bySignal != nil {
			if bid, ok := bySignal.Metadata["bitso_order_id"].(string); ok && bid != "" {
				if bid == bitsoOrderID {
					return bySignal, nil
				}
				return nil, fmt.Errorf("signal %s already has bitso_order_id %s (got %s)", signalEventID, bid, bitsoOrderID)
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			bySignal, err = m.repository.Get(ctx, bySignal.ID)
			if err != nil {
				return nil, err
			}
			if bid, ok := bySignal.Metadata["bitso_order_id"].(string); ok && bid != "" {
				if bid == bitsoOrderID {
					return bySignal, nil
				}
				return nil, fmt.Errorf("signal %s already has bitso_order_id %s (got %s)", signalEventID, bid, bitsoOrderID)
			}
			if bySignal.Metadata == nil {
				bySignal.Metadata = make(map[string]interface{})
			}
			bySignal.Metadata["bitso_order_id"] = bitsoOrderID
			switch bySignal.Status {
			case models.OrderStatusValidated:
				if err := m.stateMachine.Transition(bySignal, models.OrderStatusSubmitted); err != nil {
					return nil, err
				}
			case models.OrderStatusPending:
				if err := m.stateMachine.Transition(bySignal, models.OrderStatusValidated); err != nil {
					return nil, err
				}
				if err := m.stateMachine.Transition(bySignal, models.OrderStatusSubmitted); err != nil {
					return nil, err
				}
			default:
				// Submitted+ : only attach exchange id for sync
			}
			if err := m.repository.Update(ctx, bySignal); err != nil {
				return nil, fmt.Errorf("link bitso to signal order: %w", err)
			}
			m.updateActiveOrderMetrics(ctx)
			m.logger.Info("Linked Bitso order to signal order", map[string]interface{}{
				"order_id":       bySignal.ID,
				"signal_id":      signalEventID,
				"bitso_order_id": bitsoOrderID,
				"book":           book,
				"side":           side,
			})
			return bySignal, nil
		}
	}

	order := models.NewOrder(
		bitsoOrderID, // use as signal ID for reference
		book,
		side,
		"limit",
		strategy,
		price,
		amount,
	)
	order.Metadata["bitso_order_id"] = bitsoOrderID
	order.UpdateStatus(models.OrderStatusSubmitted)
	if err := m.repository.Create(ctx, order); err != nil {
		return nil, fmt.Errorf("create order for placed: %w", err)
	}
	m.metrics.RecordOrderCreated(book, strategy)
	m.updateActiveOrderMetrics(ctx)
	m.logger.Info("Recorded order placed", map[string]interface{}{
		"order_id":       order.ID,
		"bitso_order_id": bitsoOrderID,
		"book":           book,
		"side":           side,
	})
	return order, nil
}

// SyncOrderFromBitso updates an order from Bitso exchange state (filled amount, status). Used by the Bitso sync job.
func (m *Manager) SyncOrderFromBitso(ctx context.Context, bitsoOrderID string, filledAmount, avgPrice float64, status models.OrderStatus) error {
	order, err := m.repository.GetByBitsoOrderID(ctx, bitsoOrderID)
	if err != nil {
		m.logger.Debug("SyncOrderFromBitso: GetByBitsoOrderID failed", map[string]interface{}{
			"bitso_order_id": bitsoOrderID,
			"error":          err.Error(),
		})
		return err
	}
	// Skip truly closed orders (filled, cancelled) but NOT rejected.
	// Rejected orders may have been placed on Bitso by trading-engine despite risk check failures.
	if order.IsClosed() && order.Status != models.OrderStatusRejected {
		m.logger.Debug("SyncOrderFromBitso: order already closed, skipping", map[string]interface{}{
			"order_id":       order.ID,
			"bitso_order_id": bitsoOrderID,
			"order_status":   string(order.Status),
		})
		return nil // already final
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	order, err = m.repository.Get(ctx, order.ID)
	if err != nil {
		return err
	}
	preSyncStatus := order.Status
	if order.IsClosed() {
		m.logger.Debug("SyncOrderFromBitso: order closed after reload, skipping", map[string]interface{}{
			"order_id":       order.ID,
			"bitso_order_id": bitsoOrderID,
			"order_status":   string(order.Status),
		})
		return nil
	}
	if order.Metadata == nil {
		order.Metadata = make(map[string]interface{})
	}

	m.logger.Info("SyncOrderFromBitso: processing", map[string]interface{}{
		"order_id":         order.ID,
		"bitso_order_id":   bitsoOrderID,
		"prev_status":      string(preSyncStatus),
		"current_status":   string(order.Status),
		"target_status":    string(status),
		"prev_filled":      order.FilledAmount,
		"incoming_filled":  filledAmount,
		"avg_price":        avgPrice,
	})

	var fillDelta float64
	if filledAmount > order.FilledAmount && (status == models.OrderStatusPartiallyFilled || status == models.OrderStatusFilled) {
		fillDelta = filledAmount - order.FilledAmount
	}
	if fillDelta > 0 {
		pos, err := m.getOrCreatePosition(ctx, order.Book)
		if err != nil {
			return fmt.Errorf("position for fill: %w", err)
		}
		rd := pos.ApplyFill(order, fillDelta, avgPrice)
		prev := metaFloat(order.Metadata, orderFillRealizedPnLMetaKey)
		setMetaFloat(order.Metadata, orderFillRealizedPnLMetaKey, prev+rd)
		if err := m.positionRepo.Update(ctx, pos); err != nil {
			return fmt.Errorf("position update: %w", err)
		}
		// Do not use RecordFill here: it mutates status from fill amounts, which can disagree with
		// Bitso (e.g. remaining≈0 locally while Bitso still reports "partially filled"). That made
		// Transition(order, partial) fail after status was already set to filled, blocking sync and
		// intraday RecordTradeClosed. Exchange status is applied below via the state machine.
		order.AccumulateFill(fillDelta, avgPrice)
		m.logger.Info("SyncOrderFromBitso: applied fill", map[string]interface{}{
			"order_id":       order.ID,
			"bitso_order_id": bitsoOrderID,
			"fill_delta":     fillDelta,
			"realized_pnl":   rd,
			"total_realized": metaFloat(order.Metadata, orderFillRealizedPnLMetaKey),
		})
	} else {
		m.logger.Debug("SyncOrderFromBitso: no fill delta", map[string]interface{}{
			"order_id":        order.ID,
			"bitso_order_id":  bitsoOrderID,
			"incoming_filled": filledAmount,
			"order_filled":    order.FilledAmount,
		})
	}

	preTransition := order.Status
	if status != order.Status {
		if err := m.stateMachine.Transition(order, status); err != nil {
			m.logger.Warn("SyncOrderFromBitso: state transition failed", map[string]interface{}{
				"order_id":       order.ID,
				"bitso_order_id": bitsoOrderID,
				"from_status":    string(preTransition),
				"to_status":      string(status),
				"error":          err.Error(),
			})
			return err
		}
		m.logger.Info("SyncOrderFromBitso: state transitioned", map[string]interface{}{
			"order_id":       order.ID,
			"bitso_order_id": bitsoOrderID,
			"from_status":    string(preTransition),
			"to_status":      string(status),
		})
	}
	if err := m.repository.Update(ctx, order); err != nil {
		return err
	}
	switch status {
	case models.OrderStatusFilled:
		m.metrics.RecordOrderFilled(order.Book, order.Strategy)
		totalRealized := metaFloat(order.Metadata, orderFillRealizedPnLMetaKey)
		m.recordTradeClosedForIntraday(order, map[string]interface{}{"realized_pnl": totalRealized})
		m.appendFillLedger(ctx, order, bitsoOrderID, totalRealized)
		m.logger.Info("SyncOrderFromBitso: recorded fill metrics", map[string]interface{}{
			"order_id":       order.ID,
			"bitso_order_id": bitsoOrderID,
			"book":           order.Book,
			"strategy":       order.Strategy,
			"realized_pnl":   totalRealized,
		})
	case models.OrderStatusCancelled:
		m.metrics.RecordOrderCancelled(order.Book, order.Strategy, "exchange")
	}
	m.updateActiveOrderMetrics(ctx)
	m.maybePublishOrderFill(ctx, order, preSyncStatus)
	return nil
}

// SyncOrderFromBitsoTrades updates order state from Bitso /order_trades when /orders lookup omits the OID (e.g. completed).
func (m *Manager) SyncOrderFromBitsoTrades(ctx context.Context, bitsoOrderID string, trades []bitso.UserOrderTrade) error {
	if len(trades) == 0 {
		m.logger.Info("SyncOrderFromBitsoTrades: no trades", map[string]interface{}{
			"bitso_order_id": bitsoOrderID,
		})
		return nil
	}
	order, err := m.repository.GetByBitsoOrderID(ctx, bitsoOrderID)
	if err != nil {
		m.logger.Info("SyncOrderFromBitsoTrades: GetByBitsoOrderID failed", map[string]interface{}{
			"bitso_order_id": bitsoOrderID,
			"error":          err.Error(),
		})
		return err
	}
	// Skip truly closed orders (filled, cancelled) but NOT rejected.
	// Rejected orders may have been placed on Bitso by trading-engine despite risk check failures.
	// If they got filled, we still need to process them for P&L.
	if order.IsClosed() && order.Status != models.OrderStatusRejected {
		m.logger.Info("SyncOrderFromBitsoTrades: order already closed", map[string]interface{}{
			"order_id":       order.ID,
			"bitso_order_id": bitsoOrderID,
			"order_status":   string(order.Status),
		})
		return nil
	}

	// If order was rejected but has fills, we need to recover it
	wasRejected := order.Status == models.OrderStatusRejected
	if wasRejected {
		m.logger.Info("SyncOrderFromBitsoTrades: recovering rejected order with Bitso fills", map[string]interface{}{
			"order_id":       order.ID,
			"bitso_order_id": bitsoOrderID,
		})
	}
	var filledMajor, vwapNum float64
	for _, t := range trades {
		maj := (&t.Major).Float64()
		pr := (&t.Price).Float64()
		filledMajor += maj
		vwapNum += maj * pr
	}
	if filledMajor <= 0 {
		m.logger.Debug("SyncOrderFromBitsoTrades: no filled amount", map[string]interface{}{
			"bitso_order_id": bitsoOrderID,
			"num_trades":     len(trades),
		})
		return nil
	}
	avgPrice := vwapNum / filledMajor
	eps := 1e-7
	if order.Amount > 0 {
		eps = order.Amount * 1e-9
		if eps < 1e-12 {
			eps = 1e-12
		}
	}
	status := models.OrderStatusPartiallyFilled
	if filledMajor+eps >= order.Amount {
		status = models.OrderStatusFilled
	}

	m.logger.Info("SyncOrderFromBitsoTrades: calculated fill", map[string]interface{}{
		"order_id":       order.ID,
		"bitso_order_id": bitsoOrderID,
		"num_trades":     len(trades),
		"filled_major":   filledMajor,
		"order_amount":   order.Amount,
		"avg_price":      avgPrice,
		"computed_status": string(status),
	})

	return m.SyncOrderFromBitso(ctx, bitsoOrderID, filledMajor, avgPrice, status)
}

// ListActiveBitsoOrderIDs returns Bitso order IDs for all active orders that have one (for sync job)
func (m *Manager) ListActiveBitsoOrderIDs(ctx context.Context) ([]string, error) {
	orders, err := m.repository.GetActiveOrders(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, o := range orders {
		if id, ok := o.Metadata["bitso_order_id"].(string); ok && id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// MarkOrderStale marks an order as cancelled when it's missing from Bitso after multiple retries.
// This cleans up stale orders that may have been completed/cancelled on Bitso but weren't synced properly.
func (m *Manager) MarkOrderStale(ctx context.Context, bitsoOrderID string) error {
	order, err := m.repository.GetByBitsoOrderID(ctx, bitsoOrderID)
	if err != nil {
		return err
	}
	if order.IsClosed() {
		return nil // already closed
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Reload under lock
	order, err = m.repository.Get(ctx, order.ID)
	if err != nil {
		return err
	}
	if order.IsClosed() {
		return nil
	}

	// Mark as cancelled with stale reason
	if order.Metadata == nil {
		order.Metadata = make(map[string]interface{})
	}
	order.Metadata["stale_reason"] = "missing_from_bitso_after_retries"
	order.Metadata["stale_at"] = time.Now().UTC().Format(time.RFC3339)

	if err := m.stateMachine.Transition(order, models.OrderStatusCancelled); err != nil {
		return err
	}
	if err := m.repository.Update(ctx, order); err != nil {
		return err
	}

	m.metrics.RecordOrderCancelled(order.Book, order.Strategy, "stale")
	m.updateActiveOrderMetrics(ctx)

	m.logger.Info("Order marked as stale", map[string]interface{}{
		"order_id":       order.ID,
		"bitso_order_id": bitsoOrderID,
		"book":           order.Book,
	})

	return nil
}

func (m *Manager) getOrCreatePosition(ctx context.Context, book string) (*models.Position, error) {
	pos, err := m.positionRepo.Get(ctx, book)
	if err == nil {
		return pos, nil
	}
	np := models.NewPosition(book, "")
	if err := m.positionRepo.Create(ctx, np); err != nil {
		if pos2, err2 := m.positionRepo.Get(ctx, book); err2 == nil {
			return pos2, nil
		}
		return nil, err
	}
	return m.positionRepo.Get(ctx, book)
}

func (m *Manager) appendFillLedger(ctx context.Context, order *models.Order, bitsoOID string, realizedMXN float64) {
	if m.fillLedger == nil {
		return
	}
	e := &models.FillLedgerEntry{
		ID:             fmt.Sprintf("%s-%s-%d", order.ID, bitsoOID, time.Now().UnixNano()),
		Timestamp:      time.Now().UTC(),
		OrderID:        order.ID,
		BitsoOrderID:   bitsoOID,
		Book:           order.Book,
		Strategy:       order.Strategy,
		Side:           order.Side,
		Amount:         order.Amount,
		AvgPrice:       order.AveragePrice,
		RealizedPnLMXN: realizedMXN,
	}
	if err := m.fillLedger.Append(ctx, e); err != nil {
		m.logger.Debug("Fill ledger append failed", map[string]interface{}{"error": err.Error()})
	}
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
	preStatus := order.Status

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
	m.updateActiveOrderMetrics(ctx)
	m.maybePublishOrderFill(ctx, order, preStatus)

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
	m.updateActiveOrderMetrics(ctx)

	m.logger.Info("Order cancelled", map[string]interface{}{
		"order_id": orderID,
	})

	return nil
}

// GetOrder retrieves an order by ID
func (m *Manager) GetOrder(ctx context.Context, orderID string) (*models.Order, error) {
	return m.repository.Get(ctx, orderID)
}

// GetOrderByBitsoOrderID retrieves an order by its Bitso exchange order ID.
func (m *Manager) GetOrderByBitsoOrderID(ctx context.Context, bitsoOrderID string) (*models.Order, error) {
	return m.repository.GetByBitsoOrderID(ctx, bitsoOrderID)
}

// ListOrders lists orders with optional filters
func (m *Manager) ListOrders(ctx context.Context, filters *models.OrderFilters) ([]*models.Order, error) {
	return m.repository.List(ctx, filters)
}

// GetPositionSummary returns a summary of all positions (for intraday metrics and API).
func (m *Manager) GetPositionSummary(ctx context.Context) (*models.PositionSummary, error) {
	return m.positionRepo.GetSummary(ctx)
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
	realizedPnL := decimal.Zero
	if metadata != nil {
		realizedPnL = decimal.NewFromFloat(metaFloat(metadata, "realized_pnl"))
	}
	outcome := sharedMetrics.TradeOutcome{
		Book:        order.Book,
		Strategy:    order.Strategy,
		Currency:    currency,
		RealizedPnL: sharedMetrics.NewMonetaryAmount(realizedPnL, currency),
	}
	if realizedPnL.IsZero() {
		outcome.IsBreakeven = true
	} else {
		outcome.IsWin = realizedPnL.GreaterThan(decimal.Zero)
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
	if !m.repositoryActiveOrdersGauge {
		return
	}
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

	m.activeBooksMu.Lock()
	for book := range m.lastBooksWithNonZeroActive {
		if _, ok := bookCounts[book]; !ok {
			bookCounts[book] = 0
		}
	}
	m.lastBooksWithNonZeroActive = make(map[string]struct{})
	for book, count := range bookCounts {
		m.metrics.SetActiveOrders(book, float64(count))
		if count > 0 {
			m.lastBooksWithNonZeroActive[book] = struct{}{}
		}
	}
	m.activeBooksMu.Unlock()
}
