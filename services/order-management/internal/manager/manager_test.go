package manager

import (
	"context"
	"testing"

	"bitso-trading-platform/order-management/internal/config"
	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/repository"
	"bitso-trading-platform/order-management/internal/risk"
	"bitso-trading-platform/order-management/internal/validator"
	sharedModels "bitso-trading-platform/shared/pkg/models"
)

func TestProcessSignal(t *testing.T) {
	manager := setupManager()
	defer manager.Stop()

	ctx := context.Background()

	signal := &sharedModels.TradeSignalEvent{
		EventID:   "signal-1",
		Timestamp: 1698160000000,
		Book:      "btc_mxn",
		Strategy:  "basic",
		Signal:    "BUY",
		Price:     500000.0,
		Amount:    0.01,
		Metadata:  make(map[string]interface{}),
	}

	order, err := manager.ProcessSignal(ctx, signal)
	if err != nil {
		t.Fatalf("ProcessSignal() failed: %v", err)
	}

	if order == nil {
		t.Fatal("ProcessSignal() returned nil order")
	}

	if order.Status != models.OrderStatusValidated {
		t.Errorf("Expected status %s, got %s", models.OrderStatusValidated, order.Status)
	}

	if order.Book != "btc_mxn" {
		t.Errorf("Expected book btc_mxn, got %s", order.Book)
	}

	if order.Side != "buy" {
		t.Errorf("Expected side buy, got %s", order.Side)
	}
}

func TestProcessSignalInvalid(t *testing.T) {
	manager := setupManager()
	defer manager.Stop()

	ctx := context.Background()

	// Invalid signal (missing required fields)
	signal := &sharedModels.TradeSignalEvent{
		EventID: "",
		Book:    "",
		Signal:  "INVALID",
		Price:   0,
		Amount:  0,
	}

	order, err := manager.ProcessSignal(ctx, signal)
	if err == nil {
		t.Error("Expected error for invalid signal, got nil")
	}

	if order != nil {
		t.Error("Expected nil order for invalid signal")
	}
}

func TestCreateOrder(t *testing.T) {
	manager := setupManager()
	defer manager.Stop()

	signal := &sharedModels.TradeSignalEvent{
		EventID:   "signal-1",
		Timestamp: 1698160000000,
		Book:      "btc_mxn",
		Strategy:  "basic",
		Signal:    "BUY",
		Price:     500000.0,
		Amount:    0.01,
		Metadata:  map[string]interface{}{"reason": "test"},
	}

	order, err := manager.CreateOrder(signal)
	if err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}

	if order.SignalID != signal.EventID {
		t.Errorf("Expected signal ID %s, got %s", signal.EventID, order.SignalID)
	}

	if order.Book != signal.Book {
		t.Errorf("Expected book %s, got %s", signal.Book, order.Book)
	}

	if order.Status != models.OrderStatusPending {
		t.Errorf("Expected status %s, got %s", models.OrderStatusPending, order.Status)
	}

	// Verify metadata was copied
	if order.Metadata["signal_timestamp"] != signal.Timestamp {
		t.Error("Signal timestamp not copied to order metadata")
	}
}

func TestCreateOrderSellSignal(t *testing.T) {
	manager := setupManager()
	defer manager.Stop()

	signal := &sharedModels.TradeSignalEvent{
		EventID:  "signal-2",
		Book:     "eth_mxn",
		Strategy: "trend",
		Signal:   "SELL",
		Price:    25000.0,
		Amount:   0.5,
	}

	order, err := manager.CreateOrder(signal)
	if err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}

	if order.Side != "sell" {
		t.Errorf("Expected side sell for SELL signal, got %s", order.Side)
	}
}

func TestUpdateOrderStatus(t *testing.T) {
	manager := setupManager()
	defer manager.Stop()

	ctx := context.Background()

	// Create an order
	signal := &sharedModels.TradeSignalEvent{
		EventID:  "signal-1",
		Book:     "btc_mxn",
		Strategy: "basic",
		Signal:   "BUY",
		Price:    500000.0,
		Amount:   0.01,
	}

	order, _ := manager.CreateOrder(signal)

	// Update to validated
	err := manager.UpdateOrderStatus(ctx, order.ID, models.OrderStatusValidated, nil)
	if err != nil {
		t.Errorf("UpdateOrderStatus() failed: %v", err)
	}

	// Verify update
	updated, _ := manager.GetOrder(ctx, order.ID)
	if updated.Status != models.OrderStatusValidated {
		t.Errorf("Expected status %s, got %s", models.OrderStatusValidated, updated.Status)
	}

	// Try invalid transition
	err = manager.UpdateOrderStatus(ctx, order.ID, models.OrderStatusFilled, nil)
	if err == nil {
		t.Error("Expected error for invalid transition, got nil")
	}
}

func TestCancelOrder(t *testing.T) {
	manager := setupManager()
	defer manager.Stop()

	ctx := context.Background()

	// Create an order in accepted state
	signal := &sharedModels.TradeSignalEvent{
		EventID:  "signal-1",
		Book:     "btc_mxn",
		Strategy: "basic",
		Signal:   "BUY",
		Price:    500000.0,
		Amount:   0.01,
	}

	order, _ := manager.CreateOrder(signal)
	manager.UpdateOrderStatus(ctx, order.ID, models.OrderStatusValidated, nil)
	manager.UpdateOrderStatus(ctx, order.ID, models.OrderStatusSubmitted, nil)
	manager.UpdateOrderStatus(ctx, order.ID, models.OrderStatusAccepted, nil)

	// Cancel order
	err := manager.CancelOrder(ctx, order.ID)
	if err != nil {
		t.Errorf("CancelOrder() failed: %v", err)
	}

	// Verify cancellation
	cancelled, _ := manager.GetOrder(ctx, order.ID)
	if cancelled.Status != models.OrderStatusCancelled {
		t.Errorf("Expected status %s, got %s", models.OrderStatusCancelled, cancelled.Status)
	}
}

func TestCancelOrderInvalidStatus(t *testing.T) {
	manager := setupManager()
	defer manager.Stop()

	ctx := context.Background()

	// Create an order in pending state (cannot cancel)
	signal := &sharedModels.TradeSignalEvent{
		EventID:  "signal-1",
		Book:     "btc_mxn",
		Strategy: "basic",
		Signal:   "BUY",
		Price:    500000.0,
		Amount:   0.01,
	}

	order, _ := manager.CreateOrder(signal)

	// Try to cancel from pending (should fail)
	err := manager.CancelOrder(ctx, order.ID)
	if err == nil {
		t.Error("Expected error when cancelling from pending state, got nil")
	}
}

func TestGetOrder(t *testing.T) {
	manager := setupManager()
	defer manager.Stop()

	ctx := context.Background()

	signal := &sharedModels.TradeSignalEvent{
		EventID:  "signal-1",
		Book:     "btc_mxn",
		Strategy: "basic",
		Signal:   "BUY",
		Price:    500000.0,
		Amount:   0.01,
	}

	created, _ := manager.CreateOrder(signal)

	// Get order
	retrieved, err := manager.GetOrder(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetOrder() failed: %v", err)
	}

	if retrieved.ID != created.ID {
		t.Errorf("Expected ID %s, got %s", created.ID, retrieved.ID)
	}
}

func TestListOrders(t *testing.T) {
	manager := setupManager()
	defer manager.Stop()

	ctx := context.Background()

	// Create multiple orders
	signals := []*sharedModels.TradeSignalEvent{
		{EventID: "s1", Book: "btc_mxn", Strategy: "basic", Signal: "BUY", Price: 500000.0, Amount: 0.01},
		{EventID: "s2", Book: "eth_mxn", Strategy: "trend", Signal: "SELL", Price: 25000.0, Amount: 0.1},
		{EventID: "s3", Book: "btc_mxn", Strategy: "basic", Signal: "BUY", Price: 500000.0, Amount: 0.02},
	}

	for _, sig := range signals {
		manager.CreateOrder(sig)
	}

	// List all orders
	filters := models.NewOrderFilters()
	orders, err := manager.ListOrders(ctx, filters)
	if err != nil {
		t.Fatalf("ListOrders() failed: %v", err)
	}

	if len(orders) != 3 {
		t.Errorf("Expected 3 orders, got %d", len(orders))
	}

	// List by book
	filters.Book = "btc_mxn"
	orders, err = manager.ListOrders(ctx, filters)
	if err != nil {
		t.Fatalf("ListOrders() failed: %v", err)
	}

	if len(orders) != 2 {
		t.Errorf("Expected 2 orders for btc_mxn, got %d", len(orders))
	}
}

// Helper functions

var (
	testManagerLogger  *logger.Logger
	testManagerMetrics *metrics.MetricsCollector
	testManagerConfig  *config.Config
)

func init() {
	testManagerLogger = logger.DefaultLogger()
	testManagerMetrics = metrics.NewMetricsCollector("manager-test")

	testManagerConfig = &config.Config{
		Risk: config.RiskConfig{
			MaxOpenOrders:        10,
			MaxOrderValue:        100000.0,
			MinOrderSize:         0.001,
			MaxPositionSize:      1.0,
			EnableDuplicateCheck: true,
			MaxOrdersPerMinute:   60,
		},
	}
}

func setupManager() *Manager {
	// Create fresh repositories for each test
	orderRepo := repository.NewInMemoryOrderRepository(testManagerLogger, testManagerMetrics)
	positionRepo := repository.NewInMemoryPositionRepository(testManagerLogger, testManagerMetrics)

	// Create validator
	v := validator.NewOrderValidator(
		&testManagerConfig.Risk,
		testManagerLogger,
		orderRepo,
		testManagerMetrics,
	)

	// Create risk manager
	rm := risk.NewRiskManager(
		&testManagerConfig.Risk,
		testManagerLogger,
		orderRepo,
		positionRepo,
		testManagerMetrics,
	)

	// Create manager
	return NewOrderManager(
		testManagerConfig,
		testManagerLogger,
		v,
		rm,
		orderRepo,
		testManagerMetrics,
	)
}
