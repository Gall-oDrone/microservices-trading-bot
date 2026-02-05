// Package managertest holds tests for the order manager (OrderManager).
// These tests live in internal/testing/ to group manager tests in one place;
// they use only the exported API of the manager package (black-box style).
package managertest

import (
	"context"
	"testing"

	"bitso-trading-platform/order-management/internal/config"
	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/manager"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/repository"
	"bitso-trading-platform/order-management/internal/risk"
	"bitso-trading-platform/order-management/internal/validator"
	sharedModels "bitso-trading-platform/shared/pkg/models"
)

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

func setupManager() *manager.Manager {
	orderRepo := repository.NewInMemoryOrderRepository(testManagerLogger, testManagerMetrics)
	positionRepo := repository.NewInMemoryPositionRepository(testManagerLogger, testManagerMetrics)

	v := validator.NewOrderValidator(
		&testManagerConfig.Risk,
		testManagerLogger,
		orderRepo,
		testManagerMetrics,
	)
	rm := risk.NewRiskManager(
		&testManagerConfig.Risk,
		testManagerLogger,
		orderRepo,
		positionRepo,
		testManagerMetrics,
	)

	return manager.NewOrderManager(
		testManagerConfig,
		testManagerLogger,
		v,
		rm,
		orderRepo,
		positionRepo,
		testManagerMetrics,
		nil,
	)
}

func TestProcessSignal(t *testing.T) {
	mgr := setupManager()
	defer mgr.Stop()

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

	order, err := mgr.ProcessSignal(ctx, signal)
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
	mgr := setupManager()
	defer mgr.Stop()

	ctx := context.Background()
	signal := &sharedModels.TradeSignalEvent{
		EventID: "",
		Book:    "",
		Signal:  "INVALID",
		Price:   0,
		Amount:  0,
	}

	order, err := mgr.ProcessSignal(ctx, signal)
	if err == nil {
		t.Error("Expected error for invalid signal, got nil")
	}
	if order != nil {
		t.Error("Expected nil order for invalid signal")
	}
}

func TestCreateOrder(t *testing.T) {
	mgr := setupManager()
	defer mgr.Stop()

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

	order, err := mgr.CreateOrder(signal)
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
	if order.Metadata["signal_timestamp"] != signal.Timestamp {
		t.Error("Signal timestamp not copied to order metadata")
	}
}

func TestCreateOrderSellSignal(t *testing.T) {
	mgr := setupManager()
	defer mgr.Stop()

	signal := &sharedModels.TradeSignalEvent{
		EventID:  "signal-2",
		Book:     "eth_mxn",
		Strategy: "trend",
		Signal:   "SELL",
		Price:    25000.0,
		Amount:   0.5,
	}

	order, err := mgr.CreateOrder(signal)
	if err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}
	if order.Side != "sell" {
		t.Errorf("Expected side sell for SELL signal, got %s", order.Side)
	}
}

func TestUpdateOrderStatus(t *testing.T) {
	mgr := setupManager()
	defer mgr.Stop()

	ctx := context.Background()
	signal := &sharedModels.TradeSignalEvent{
		EventID:  "signal-1",
		Book:     "btc_mxn",
		Strategy: "basic",
		Signal:   "BUY",
		Price:    500000.0,
		Amount:    0.01,
	}

	order, _ := mgr.CreateOrder(signal)

	err := mgr.UpdateOrderStatus(ctx, order.ID, models.OrderStatusValidated, nil)
	if err != nil {
		t.Errorf("UpdateOrderStatus() failed: %v", err)
	}
	updated, _ := mgr.GetOrder(ctx, order.ID)
	if updated.Status != models.OrderStatusValidated {
		t.Errorf("Expected status %s, got %s", models.OrderStatusValidated, updated.Status)
	}

	err = mgr.UpdateOrderStatus(ctx, order.ID, models.OrderStatusFilled, nil)
	if err == nil {
		t.Error("Expected error for invalid transition, got nil")
	}
}

func TestCancelOrder(t *testing.T) {
	mgr := setupManager()
	defer mgr.Stop()

	ctx := context.Background()
	signal := &sharedModels.TradeSignalEvent{
		EventID:  "signal-1",
		Book:     "btc_mxn",
		Strategy: "basic",
		Signal:   "BUY",
		Price:    500000.0,
		Amount:    0.01,
	}

	order, _ := mgr.CreateOrder(signal)
	_ = mgr.UpdateOrderStatus(ctx, order.ID, models.OrderStatusValidated, nil)
	_ = mgr.UpdateOrderStatus(ctx, order.ID, models.OrderStatusSubmitted, nil)
	_ = mgr.UpdateOrderStatus(ctx, order.ID, models.OrderStatusAccepted, nil)

	err := mgr.CancelOrder(ctx, order.ID)
	if err != nil {
		t.Errorf("CancelOrder() failed: %v", err)
	}
	cancelled, _ := mgr.GetOrder(ctx, order.ID)
	if cancelled.Status != models.OrderStatusCancelled {
		t.Errorf("Expected status %s, got %s", models.OrderStatusCancelled, cancelled.Status)
	}
}

func TestCancelOrderInvalidStatus(t *testing.T) {
	mgr := setupManager()
	defer mgr.Stop()

	ctx := context.Background()
	signal := &sharedModels.TradeSignalEvent{
		EventID:  "signal-1",
		Book:     "btc_mxn",
		Strategy: "basic",
		Signal:   "BUY",
		Price:    500000.0,
		Amount:    0.01,
	}

	order, _ := mgr.CreateOrder(signal)
	err := mgr.CancelOrder(ctx, order.ID)
	if err == nil {
		t.Error("Expected error when cancelling from pending state, got nil")
	}
}

func TestGetOrder(t *testing.T) {
	mgr := setupManager()
	defer mgr.Stop()

	ctx := context.Background()
	signal := &sharedModels.TradeSignalEvent{
		EventID:  "signal-1",
		Book:     "btc_mxn",
		Strategy: "basic",
		Signal:   "BUY",
		Price:    500000.0,
		Amount:    0.01,
	}

	created, _ := mgr.CreateOrder(signal)
	retrieved, err := mgr.GetOrder(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetOrder() failed: %v", err)
	}
	if retrieved.ID != created.ID {
		t.Errorf("Expected ID %s, got %s", created.ID, retrieved.ID)
	}
}

func TestListOrders(t *testing.T) {
	mgr := setupManager()
	defer mgr.Stop()

	ctx := context.Background()
	signals := []*sharedModels.TradeSignalEvent{
		{EventID: "s1", Book: "btc_mxn", Strategy: "basic", Signal: "BUY", Price: 500000.0, Amount: 0.01},
		{EventID: "s2", Book: "eth_mxn", Strategy: "trend", Signal: "SELL", Price: 25000.0, Amount: 0.1},
		{EventID: "s3", Book: "btc_mxn", Strategy: "basic", Signal: "BUY", Price: 500000.0, Amount: 0.02},
	}
	for _, sig := range signals {
		_, _ = mgr.CreateOrder(sig)
	}

	filters := models.NewOrderFilters()
	orders, err := mgr.ListOrders(ctx, filters)
	if err != nil {
		t.Fatalf("ListOrders() failed: %v", err)
	}
	if len(orders) != 3 {
		t.Errorf("Expected 3 orders, got %d", len(orders))
	}

	filters.Book = "btc_mxn"
	orders, err = mgr.ListOrders(ctx, filters)
	if err != nil {
		t.Fatalf("ListOrders() failed: %v", err)
	}
	if len(orders) != 2 {
		t.Errorf("Expected 2 orders for btc_mxn, got %d", len(orders))
	}
}

func TestGetPositionSummary_Empty(t *testing.T) {
	mgr := setupManager()
	defer mgr.Stop()

	ctx := context.Background()
	summary, err := mgr.GetPositionSummary(ctx)
	if err != nil {
		t.Fatalf("GetPositionSummary() failed: %v", err)
	}
	if summary == nil {
		t.Fatal("GetPositionSummary() returned nil")
	}
	if summary.TotalPositions != 0 || summary.OpenPositions != 0 || summary.TotalPnL != 0 {
		t.Errorf("expected empty summary: got TotalPositions=%d OpenPositions=%d TotalPnL=%f",
			summary.TotalPositions, summary.OpenPositions, summary.TotalPnL)
	}
}
