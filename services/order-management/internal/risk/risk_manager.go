package risk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"bitso-trading-platform/order-management/internal/config"
	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/repository"
)

// RiskManager manages risk checks for orders
type RiskManager interface {
	CheckRisk(ctx context.Context, order *models.Order) error
	CheckPositionLimits(ctx context.Context, order *models.Order) error
	CheckOrderLimits(ctx context.Context, order *models.Order) error
	GetPositionLimits(book string) (*PositionLimits, error)
	GetCurrentExposure(ctx context.Context, book string) (*Exposure, error)
}

// Manager implements RiskManager
type Manager struct {
	logger       *logger.Logger
	config       *config.RiskConfig
	orderRepo    repository.OrderRepository
	positionRepo repository.PositionRepository
	metrics      *metrics.MetricsCollector

	// Rate limiting
	orderCounts map[string]int // minute -> count
	countsMutex sync.RWMutex
}

// PositionLimits defines position limits for a book
type PositionLimits struct {
	MaxSize  float64
	MaxValue float64
}

// Exposure represents current exposure for a book
type Exposure struct {
	TotalSize  float64
	TotalValue float64
	OpenOrders int
}

// NewRiskManager creates a new risk manager
func NewRiskManager(
	config *config.RiskConfig,
	logger *logger.Logger,
	orderRepo repository.OrderRepository,
	positionRepo repository.PositionRepository,
	metrics *metrics.MetricsCollector,
) *Manager {
	return &Manager{
		logger:       logger,
		config:       config,
		orderRepo:    orderRepo,
		positionRepo: positionRepo,
		metrics:      metrics,
		orderCounts:  make(map[string]int),
	}
}

// CheckRisk performs comprehensive risk checks on an order
func (rm *Manager) CheckRisk(ctx context.Context, order *models.Order) error {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		rm.logger.Debug("Risk check completed", map[string]interface{}{
			"order_id": order.ID,
			"duration": duration,
		})
	}()

	result := models.NewRiskCheckResult()

	// Check position limits
	if err := rm.CheckPositionLimits(ctx, order); err != nil {
		result.AddError("position_limits", err.Error(), 0, 0)
		rm.metrics.RecordRiskViolation("position_limits")
	}

	// Check order limits
	if err := rm.CheckOrderLimits(ctx, order); err != nil {
		result.AddError("order_limits", err.Error(), 0, 0)
		rm.metrics.RecordRiskViolation("order_limits")
	}

	// Check concentration risk
	if err := rm.CheckConcentrationRisk(ctx, order); err != nil {
		result.AddWarning("concentration", err.Error(), 0, 0)
		// Warning only, don't fail
	}

	// Check rate limits
	if err := rm.CheckRateLimit(ctx); err != nil {
		result.AddCritical("rate_limit", err.Error(), 0, 0)
		rm.metrics.RecordRiskViolation("rate_limit")
	}

	// Record metrics
	if result.Passed {
		rm.metrics.RecordRiskCheck("comprehensive", "passed")
	} else {
		rm.metrics.RecordRiskCheck("comprehensive", "failed")
	}

	if result.HasViolations() {
		return fmt.Errorf("risk check failed: %s", result.Error())
	}

	return nil
}

// CheckPositionLimits checks if the order exceeds position limits
func (rm *Manager) CheckPositionLimits(ctx context.Context, order *models.Order) error {
	start := time.Now()
	defer rm.metrics.RecordRiskCheck("position_limits", "checked")

	// Get current position
	position, err := rm.positionRepo.Get(ctx, order.Book)
	if err != nil {
		// No position yet, create one for checking
		position = models.NewPosition(order.Book, "long")
	}

	// Calculate new position size if order fills
	newSize := position.Size
	if order.Side == "buy" {
		newSize += order.Amount
	} else {
		newSize -= order.Amount
	}

	// Check against max position size
	if newSize > rm.config.MaxPositionSize {
		rm.logger.Warn("Position limit exceeded", map[string]interface{}{
			"book":     order.Book,
			"new_size": newSize,
			"max_size": rm.config.MaxPositionSize,
			"duration": time.Since(start),
		})
		return fmt.Errorf("position size %f would exceed limit %f", newSize, rm.config.MaxPositionSize)
	}

	return nil
}

// CheckOrderLimits checks if the order exceeds order limits
func (rm *Manager) CheckOrderLimits(ctx context.Context, order *models.Order) error {
	start := time.Now()
	defer rm.metrics.RecordRiskCheck("order_limits", "checked")

	// Check max open orders
	activeOrders, err := rm.orderRepo.GetActiveOrders(ctx)
	if err != nil {
		return fmt.Errorf("failed to get active orders: %w", err)
	}

	if len(activeOrders) >= rm.config.MaxOpenOrders {
		rm.logger.Warn("Max open orders exceeded", map[string]interface{}{
			"active_orders": len(activeOrders),
			"max_orders":    rm.config.MaxOpenOrders,
			"duration":      time.Since(start),
		})
		return fmt.Errorf("max open orders limit reached: %d/%d", len(activeOrders), rm.config.MaxOpenOrders)
	}

	// Check order value
	orderValue := order.Amount * order.Price
	if orderValue > rm.config.MaxOrderValue {
		rm.logger.Warn("Order value limit exceeded", map[string]interface{}{
			"order_value": orderValue,
			"max_value":   rm.config.MaxOrderValue,
			"duration":    time.Since(start),
		})
		return fmt.Errorf("order value %f exceeds limit %f", orderValue, rm.config.MaxOrderValue)
	}

	return nil
}

// CheckConcentrationRisk checks if the order creates concentration risk
func (rm *Manager) CheckConcentrationRisk(ctx context.Context, order *models.Order) error {
	// Get all positions
	positions, err := rm.positionRepo.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	if len(positions) == 0 {
		return nil // No concentration risk with no positions
	}

	// Calculate total portfolio value
	totalValue := 0.0
	for _, pos := range positions {
		totalValue += pos.Size * pos.CurrentPrice
	}

	// Calculate this order's value
	orderValue := order.Amount * order.Price

	// Check if single order is more than 50% of portfolio
	if totalValue > 0 && orderValue > totalValue*0.5 {
		return fmt.Errorf("order represents >50%% of portfolio value")
	}

	return nil
}

// CheckRateLimit checks if order creation rate is within limits
func (rm *Manager) CheckRateLimit(ctx context.Context) error {
	rm.countsMutex.Lock()
	defer rm.countsMutex.Unlock()

	// Get current minute
	currentMinute := time.Now().Format("2006-01-02-15-04")

	// Clean old entries
	for key := range rm.orderCounts {
		if key != currentMinute {
			delete(rm.orderCounts, key)
		}
	}

	// Check current count
	count := rm.orderCounts[currentMinute]
	if count >= rm.config.MaxOrdersPerMinute {
		rm.metrics.RecordRiskViolation("rate_limit")
		return fmt.Errorf("rate limit exceeded: %d orders in current minute (max: %d)",
			count, rm.config.MaxOrdersPerMinute)
	}

	// Increment count
	rm.orderCounts[currentMinute] = count + 1

	return nil
}

// GetPositionLimits returns position limits for a book
func (rm *Manager) GetPositionLimits(book string) (*PositionLimits, error) {
	return &PositionLimits{
		MaxSize:  rm.config.MaxPositionSize,
		MaxValue: rm.config.MaxOrderValue * 10, // Example: 10x order value
	}, nil
}

// GetCurrentExposure returns current exposure for a book
func (rm *Manager) GetCurrentExposure(ctx context.Context, book string) (*Exposure, error) {
	// Get position
	position, err := rm.positionRepo.Get(ctx, book)
	if err != nil {
		// No position yet
		position = models.NewPosition(book, "long")
	}

	// Get active orders
	activeOrders, err := rm.orderRepo.GetOrdersByBook(ctx, book)
	if err != nil {
		return nil, fmt.Errorf("failed to get orders: %w", err)
	}

	// Count only active orders
	openOrderCount := 0
	for _, order := range activeOrders {
		if order.IsActive() {
			openOrderCount++
		}
	}

	exposure := &Exposure{
		TotalSize:  position.Size,
		TotalValue: position.Size * position.CurrentPrice,
		OpenOrders: openOrderCount,
	}

	return exposure, nil
}

// ResetRateLimitForTesting resets rate limit counters (for testing only)
func (rm *Manager) ResetRateLimitForTesting() {
	rm.countsMutex.Lock()
	defer rm.countsMutex.Unlock()
	rm.orderCounts = make(map[string]int)
}
