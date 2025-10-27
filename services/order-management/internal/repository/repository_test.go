package repository

import (
	"context"
	"testing"

	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
)

func TestOrderRepository_Create(t *testing.T) {
	repo := setupOrderRepo()
	defer repo.Close()

	ctx := context.Background()
	order := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)

	err := repo.Create(ctx, order)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Verify order was created
	retrieved, err := repo.Get(ctx, order.ID)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	if retrieved.ID != order.ID {
		t.Errorf("Expected ID %s, got %s", order.ID, retrieved.ID)
	}

	if retrieved.Book != order.Book {
		t.Errorf("Expected Book %s, got %s", order.Book, retrieved.Book)
	}
}

func TestOrderRepository_CreateDuplicate(t *testing.T) {
	repo := setupOrderRepo()
	defer repo.Close()

	ctx := context.Background()
	order := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)

	err := repo.Create(ctx, order)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Try to create duplicate
	err = repo.Create(ctx, order)
	if err == nil {
		t.Error("Expected error when creating duplicate order, got nil")
	}
}

func TestOrderRepository_Update(t *testing.T) {
	repo := setupOrderRepo()
	defer repo.Close()

	ctx := context.Background()
	order := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)

	err := repo.Create(ctx, order)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Update order
	order.UpdateStatus(models.OrderStatusValidated)
	err = repo.Update(ctx, order)
	if err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	// Verify update
	retrieved, err := repo.Get(ctx, order.ID)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	if retrieved.Status != models.OrderStatusValidated {
		t.Errorf("Expected status %s, got %s", models.OrderStatusValidated, retrieved.Status)
	}
}

func TestOrderRepository_Get(t *testing.T) {
	repo := setupOrderRepo()
	defer repo.Close()

	ctx := context.Background()

	// Try to get non-existent order
	_, err := repo.Get(ctx, "non-existent")
	if err == nil {
		t.Error("Expected error when getting non-existent order, got nil")
	}

	// Create and get order
	order := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
	err = repo.Create(ctx, order)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	retrieved, err := repo.Get(ctx, order.ID)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	if retrieved.ID != order.ID {
		t.Errorf("Expected ID %s, got %s", order.ID, retrieved.ID)
	}
}

func TestOrderRepository_List(t *testing.T) {
	repo := setupOrderRepo()
	defer repo.Close()

	ctx := context.Background()

	// Create multiple orders
	orders := []*models.Order{
		models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01),
		models.NewOrder("signal-2", "eth_mxn", "sell", "limit", "basic", 25000.0, 0.1),
		models.NewOrder("signal-3", "btc_mxn", "buy", "market", "trend", 500000.0, 0.02),
	}

	for _, order := range orders {
		if err := repo.Create(ctx, order); err != nil {
			t.Fatalf("Create() failed: %v", err)
		}
	}

	// List all orders
	filters := models.NewOrderFilters()
	retrieved, err := repo.List(ctx, filters)
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}

	if len(retrieved) != 3 {
		t.Errorf("Expected 3 orders, got %d", len(retrieved))
	}

	// List orders by book
	filters.Book = "btc_mxn"
	retrieved, err = repo.List(ctx, filters)
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}

	if len(retrieved) != 2 {
		t.Errorf("Expected 2 orders for btc_mxn, got %d", len(retrieved))
	}
}

func TestOrderRepository_Delete(t *testing.T) {
	repo := setupOrderRepo()
	defer repo.Close()

	ctx := context.Background()
	order := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)

	err := repo.Create(ctx, order)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Delete order
	err = repo.Delete(ctx, order.ID)
	if err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}

	// Verify deletion
	_, err = repo.Get(ctx, order.ID)
	if err == nil {
		t.Error("Expected error when getting deleted order, got nil")
	}
}

func TestOrderRepository_GetBySignalID(t *testing.T) {
	repo := setupOrderRepo()
	defer repo.Close()

	ctx := context.Background()
	order := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)

	err := repo.Create(ctx, order)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Get by signal ID
	retrieved, err := repo.GetBySignalID(ctx, "signal-1")
	if err != nil {
		t.Fatalf("GetBySignalID() failed: %v", err)
	}

	if retrieved.ID != order.ID {
		t.Errorf("Expected ID %s, got %s", order.ID, retrieved.ID)
	}
}

func TestOrderRepository_GetActiveOrders(t *testing.T) {
	repo := setupOrderRepo()
	defer repo.Close()

	ctx := context.Background()

	// Create orders with different statuses
	order1 := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
	order1.UpdateStatus(models.OrderStatusPending)
	repo.Create(ctx, order1)

	order2 := models.NewOrder("signal-2", "eth_mxn", "sell", "limit", "basic", 25000.0, 0.1)
	order2.UpdateStatus(models.OrderStatusFilled)
	repo.Create(ctx, order2)

	order3 := models.NewOrder("signal-3", "btc_mxn", "buy", "market", "trend", 500000.0, 0.02)
	order3.UpdateStatus(models.OrderStatusAccepted)
	repo.Create(ctx, order3)

	// Get active orders
	active, err := repo.GetActiveOrders(ctx)
	if err != nil {
		t.Fatalf("GetActiveOrders() failed: %v", err)
	}

	if len(active) != 2 {
		t.Errorf("Expected 2 active orders, got %d", len(active))
	}
}

func TestOrderRepository_Count(t *testing.T) {
	repo := setupOrderRepo()
	defer repo.Close()

	ctx := context.Background()

	// Create orders
	for i := 0; i < 5; i++ {
		order := models.NewOrder("signal", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
		repo.Create(ctx, order)
	}

	// Count all orders
	filters := models.NewOrderFilters()
	count, err := repo.Count(ctx, filters)
	if err != nil {
		t.Fatalf("Count() failed: %v", err)
	}

	if count != 5 {
		t.Errorf("Expected count 5, got %d", count)
	}
}

func TestOrderRepository_Exists(t *testing.T) {
	repo := setupOrderRepo()
	defer repo.Close()

	ctx := context.Background()
	order := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)

	// Check non-existent order
	exists, err := repo.Exists(ctx, order.ID)
	if err != nil {
		t.Fatalf("Exists() failed: %v", err)
	}

	if exists {
		t.Error("Expected order to not exist")
	}

	// Create order and check again
	repo.Create(ctx, order)
	exists, err = repo.Exists(ctx, order.ID)
	if err != nil {
		t.Fatalf("Exists() failed: %v", err)
	}

	if !exists {
		t.Error("Expected order to exist")
	}
}

// Position Repository Tests

func TestPositionRepository_Create(t *testing.T) {
	repo := setupPositionRepo()
	defer repo.Close()

	ctx := context.Background()
	position := models.NewPosition("btc_mxn", "long")

	err := repo.Create(ctx, position)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Verify position was created
	retrieved, err := repo.Get(ctx, position.Book)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	if retrieved.Book != position.Book {
		t.Errorf("Expected Book %s, got %s", position.Book, retrieved.Book)
	}
}

func TestPositionRepository_Update(t *testing.T) {
	repo := setupPositionRepo()
	defer repo.Close()

	ctx := context.Background()
	position := models.NewPosition("btc_mxn", "long")

	err := repo.Create(ctx, position)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Update position
	position.Size = 0.1
	position.EntryPrice = 500000.0
	err = repo.Update(ctx, position)
	if err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	// Verify update
	retrieved, err := repo.Get(ctx, position.Book)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	if retrieved.Size != 0.1 {
		t.Errorf("Expected size 0.1, got %f", retrieved.Size)
	}
}

func TestPositionRepository_GetAll(t *testing.T) {
	repo := setupPositionRepo()
	defer repo.Close()

	ctx := context.Background()

	// Create multiple positions
	positions := []*models.Position{
		models.NewPosition("btc_mxn", "long"),
		models.NewPosition("eth_mxn", "long"),
		models.NewPosition("xrp_mxn", "short"),
	}

	for _, pos := range positions {
		if err := repo.Create(ctx, pos); err != nil {
			t.Fatalf("Create() failed: %v", err)
		}
	}

	// Get all positions
	retrieved, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll() failed: %v", err)
	}

	if len(retrieved) != 3 {
		t.Errorf("Expected 3 positions, got %d", len(retrieved))
	}
}

func TestPositionRepository_GetOpenPositions(t *testing.T) {
	repo := setupPositionRepo()
	defer repo.Close()

	ctx := context.Background()

	// Create positions
	pos1 := models.NewPosition("btc_mxn", "long")
	pos1.Size = 0.1
	repo.Create(ctx, pos1)

	pos2 := models.NewPosition("eth_mxn", "long")
	pos2.Size = 0
	pos2.Close()
	repo.Create(ctx, pos2)

	// Get open positions
	open, err := repo.GetOpenPositions(ctx)
	if err != nil {
		t.Fatalf("GetOpenPositions() failed: %v", err)
	}

	if len(open) != 1 {
		t.Errorf("Expected 1 open position, got %d", len(open))
	}
}

func TestPositionRepository_GetSummary(t *testing.T) {
	repo := setupPositionRepo()
	defer repo.Close()

	ctx := context.Background()

	// Create positions
	pos1 := models.NewPosition("btc_mxn", "long")
	pos1.Size = 0.1
	pos1.UnrealizedPnL = 1000.0
	pos1.RealizedPnL = 500.0
	repo.Create(ctx, pos1)

	pos2 := models.NewPosition("eth_mxn", "long")
	pos2.Size = 1.0
	pos2.UnrealizedPnL = 2000.0
	pos2.RealizedPnL = -300.0
	repo.Create(ctx, pos2)

	// Get summary
	summary, err := repo.GetSummary(ctx)
	if err != nil {
		t.Fatalf("GetSummary() failed: %v", err)
	}

	if summary.TotalPositions != 2 {
		t.Errorf("Expected 2 total positions, got %d", summary.TotalPositions)
	}

	if summary.OpenPositions != 2 {
		t.Errorf("Expected 2 open positions, got %d", summary.OpenPositions)
	}

	expectedUnrealizedPnL := 3000.0
	if summary.TotalUnrealizedPnL != expectedUnrealizedPnL {
		t.Errorf("Expected unrealized PnL %f, got %f", expectedUnrealizedPnL, summary.TotalUnrealizedPnL)
	}

	expectedRealizedPnL := 200.0
	if summary.TotalRealizedPnL != expectedRealizedPnL {
		t.Errorf("Expected realized PnL %f, got %f", expectedRealizedPnL, summary.TotalRealizedPnL)
	}
}

func TestPositionRepository_Delete(t *testing.T) {
	repo := setupPositionRepo()
	defer repo.Close()

	ctx := context.Background()
	position := models.NewPosition("btc_mxn", "long")

	err := repo.Create(ctx, position)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Delete position
	err = repo.Delete(ctx, position.Book)
	if err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}

	// Verify deletion
	_, err = repo.Get(ctx, position.Book)
	if err == nil {
		t.Error("Expected error when getting deleted position, got nil")
	}
}

func TestPositionRepository_Exists(t *testing.T) {
	repo := setupPositionRepo()
	defer repo.Close()

	ctx := context.Background()
	position := models.NewPosition("btc_mxn", "long")

	// Check non-existent position
	exists, err := repo.Exists(ctx, position.Book)
	if err != nil {
		t.Fatalf("Exists() failed: %v", err)
	}

	if exists {
		t.Error("Expected position to not exist")
	}

	// Create position and check again
	repo.Create(ctx, position)
	exists, err = repo.Exists(ctx, position.Book)
	if err != nil {
		t.Fatalf("Exists() failed: %v", err)
	}

	if !exists {
		t.Error("Expected position to exist")
	}
}

// Helper functions

var (
	testLogger  *logger.Logger
	testMetrics *metrics.MetricsCollector
)

func init() {
	testLogger = logger.DefaultLogger()
	testMetrics = metrics.NewMetricsCollector("test")
}

func setupOrderRepo() *InMemoryOrderRepository {
	return NewInMemoryOrderRepository(testLogger, testMetrics)
}

func setupPositionRepo() *InMemoryPositionRepository {
	return NewInMemoryPositionRepository(testLogger, testMetrics)
}

// Benchmark tests

func BenchmarkOrderRepository_Create(b *testing.B) {
	repo := setupOrderRepo()
	defer repo.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		order := models.NewOrder("signal", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
		repo.Create(ctx, order)
	}
}

func BenchmarkOrderRepository_Get(b *testing.B) {
	repo := setupOrderRepo()
	defer repo.Close()

	ctx := context.Background()
	order := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
	repo.Create(ctx, order)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		repo.Get(ctx, order.ID)
	}
}

func BenchmarkOrderRepository_List(b *testing.B) {
	repo := setupOrderRepo()
	defer repo.Close()

	ctx := context.Background()

	// Create 100 orders
	for i := 0; i < 100; i++ {
		order := models.NewOrder("signal", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
		repo.Create(ctx, order)
	}

	filters := models.NewOrderFilters()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		repo.List(ctx, filters)
	}
}
