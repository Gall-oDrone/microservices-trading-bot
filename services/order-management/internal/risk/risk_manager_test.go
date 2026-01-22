package risk

import (
	"context"
	"testing"

	"bitso-trading-platform/order-management/internal/config"
	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/repository"
)

func TestCheckRisk(t *testing.T) {
	manager := setupRiskManager()
	ctx := context.Background()

	// Valid order
	order := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
	err := manager.CheckRisk(ctx, order)
	if err != nil {
		t.Errorf("CheckRisk() failed for valid order: %v", err)
	}
}

func TestCheckPositionLimits(t *testing.T) {
	manager := setupRiskManager()
	ctx := context.Background()

	tests := []struct {
		name     string
		amount   float64
		existing float64
		wantErr  bool
	}{
		{"within limits", 0.1, 0.0, false},
		{"at limit", 0.5, 0.5, false},
		{"exceeds limit", 0.6, 0.5, true},
		{"new position within limit", 0.8, 0.0, false},
		{"new position exceeds limit", 1.5, 0.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup existing position if needed
			if tt.existing > 0 {
				position := models.NewPosition("btc_mxn", "long")
				position.Size = tt.existing
				manager.positionRepo.Create(ctx, position)
				defer manager.positionRepo.Delete(ctx, "btc_mxn")
			}

			order := models.NewOrder("signal", "btc_mxn", "buy", "limit", "basic", 500000.0, tt.amount)
			err := manager.CheckPositionLimits(ctx, order)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckPositionLimits() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckOrderLimits(t *testing.T) {
	manager := setupRiskManager()
	ctx := context.Background()

	// Test max open orders
	t.Run("max open orders", func(t *testing.T) {
		// Create max open orders
		for i := 0; i < 10; i++ {
			order := models.NewOrder("signal", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
			order.UpdateStatus(models.OrderStatusAccepted)
			manager.orderRepo.Create(ctx, order)
		}

		// Try to add one more
		newOrder := models.NewOrder("signal-new", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
		err := manager.CheckOrderLimits(ctx, newOrder)
		if err == nil {
			t.Error("Expected error when exceeding max open orders, got nil")
		}

		// Cleanup
		orders, _ := manager.orderRepo.GetActiveOrders(ctx)
		for _, o := range orders {
			manager.orderRepo.Delete(ctx, o.ID)
		}
	})

	// Test max order value
	t.Run("max order value", func(t *testing.T) {
		// Create order exceeding max value
		order := models.NewOrder("signal", "btc_mxn", "buy", "limit", "basic", 5000000.0, 100.0)
		err := manager.CheckOrderLimits(ctx, order)
		if err == nil {
			t.Error("Expected error when exceeding max order value, got nil")
		}
	})

	// Test valid order
	t.Run("valid order", func(t *testing.T) {
		order := models.NewOrder("signal", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
		err := manager.CheckOrderLimits(ctx, order)
		if err != nil {
			t.Errorf("Expected no error for valid order, got: %v", err)
		}
	})
}

func TestCheckConcentrationRisk(t *testing.T) {
	manager := setupRiskManager()
	ctx := context.Background()

	// Create existing position
	position := models.NewPosition("btc_mxn", "long")
	position.Size = 0.1
	position.CurrentPrice = 500000.0
	manager.positionRepo.Create(ctx, position)
	defer manager.positionRepo.Delete(ctx, "btc_mxn")

	tests := []struct {
		name    string
		amount  float64
		price   float64
		wantErr bool
	}{
		{"small order", 0.01, 500000.0, false},
		{"medium order", 0.05, 500000.0, false},
		{"large order - concentration risk", 1.0, 500000.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := models.NewOrder("signal", "eth_mxn", "buy", "limit", "basic", tt.price, tt.amount)
			err := manager.CheckConcentrationRisk(ctx, order)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckConcentrationRisk() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckRateLimit(t *testing.T) {
	manager := setupRiskManager()
	ctx := context.Background()

	// Test rate limiting
	successCount := 0
	for i := 0; i < 65; i++ {
		err := manager.CheckRateLimit(ctx)
		if err == nil {
			successCount++
		}
	}

	// Should allow up to MaxOrdersPerMinute
	if successCount > manager.config.MaxOrdersPerMinute {
		t.Errorf("Rate limit not enforced: allowed %d orders (max: %d)", successCount, manager.config.MaxOrdersPerMinute)
	}

	if successCount != manager.config.MaxOrdersPerMinute {
		t.Errorf("Expected exactly %d successful orders, got %d", manager.config.MaxOrdersPerMinute, successCount)
	}

	// Reset and test again
	manager.ResetRateLimitForTesting()
	err := manager.CheckRateLimit(ctx)
	if err != nil {
		t.Error("Expected rate limit to be reset")
	}
}

func TestGetPositionLimits(t *testing.T) {
	manager := setupRiskManager()

	limits, err := manager.GetPositionLimits("btc_mxn")
	if err != nil {
		t.Fatalf("GetPositionLimits() failed: %v", err)
	}

	if limits.MaxSize != manager.config.MaxPositionSize {
		t.Errorf("Expected max size %f, got %f", manager.config.MaxPositionSize, limits.MaxSize)
	}
}

func TestGetCurrentExposure(t *testing.T) {
	manager := setupRiskManager()
	ctx := context.Background()

	// Create position
	position := models.NewPosition("btc_mxn", "long")
	position.Size = 0.1
	position.CurrentPrice = 500000.0
	manager.positionRepo.Create(ctx, position)
	defer manager.positionRepo.Delete(ctx, "btc_mxn")

	// Create some orders
	order1 := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
	order1.UpdateStatus(models.OrderStatusAccepted)
	manager.orderRepo.Create(ctx, order1)
	defer manager.orderRepo.Delete(ctx, order1.ID)

	exposure, err := manager.GetCurrentExposure(ctx, "btc_mxn")
	if err != nil {
		t.Fatalf("GetCurrentExposure() failed: %v", err)
	}

	if exposure.TotalSize != 0.1 {
		t.Errorf("Expected total size 0.1, got %f", exposure.TotalSize)
	}

	if exposure.OpenOrders != 1 {
		t.Errorf("Expected 1 open order, got %d", exposure.OpenOrders)
	}
}

// Helper functions

var (
	testRiskLogger       *logger.Logger
	testRiskMetrics      *metrics.MetricsCollector
	testRiskOrderRepo    repository.OrderRepository
	testRiskPositionRepo repository.PositionRepository
)

func init() {
	testRiskLogger = logger.DefaultLogger()
	testRiskMetrics = metrics.NewMetricsCollector("risk-test")
	testRiskOrderRepo = repository.NewInMemoryOrderRepository(testRiskLogger, testRiskMetrics)
	testRiskPositionRepo = repository.NewInMemoryPositionRepository(testRiskLogger, testRiskMetrics)
}

func setupRiskManager() *Manager {
	cfg := &config.RiskConfig{
		MaxOpenOrders:        10,
		MaxOrderValue:        100000.0,
		MinOrderSize:         0.001,
		MaxPositionSize:      1.0,
		EnableDuplicateCheck: true,
		MaxOrdersPerMinute:   60,
	}

	return NewRiskManager(cfg, testRiskLogger, testRiskOrderRepo, testRiskPositionRepo, testRiskMetrics)
}
