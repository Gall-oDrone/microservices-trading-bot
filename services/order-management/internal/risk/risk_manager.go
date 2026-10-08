package risk

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/order-management/internal/config"
	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/repository"
	sharedrisk "bitso-trading-platform/shared/pkg/risk"
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

	// shared is the effective limit set and halt files (shared_policy.go).
	shared   *SharedPolicy
	policyMu sync.RWMutex

	// rate counts accepted orders in the trailing minute (per book and
	// firm-wide) for the max_orders_per_minute limits.
	rate *rateWindow
	now  func() time.Time
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
		rate:         newRateWindow(time.Minute),
		now:          time.Now,
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

	// One limit set (plan §6.4.6): the effective shared policy, from
	// TRADING_RISK_POLICY or built from the env limits, plus the operator
	// halt files. Position, notional, open orders and orders per minute are
	// all in it, per book and firm-wide.
	for _, f := range rm.checkShared(ctx, order) {
		result.AddCritical("shared_policy:"+f.Rule, f.Message, f.Value, f.Limit)
		rm.metrics.RecordRiskViolation(violationType(f.Rule))
	}

	// Check concentration risk
	if err := rm.CheckConcentrationRisk(ctx, order); err != nil {
		result.AddWarning("concentration", err.Error(), 0, 0)
		// Warning only, don't fail
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

	// Accepted: it counts towards the orders-per-minute limits.
	rm.rate.record(order.Book, rateKey(order), rm.now())
	return nil
}

// violationType keeps the risk_violations_total labels the dashboards and
// alerts already use: the limits that used to be order-management's own
// env checks keep their old names; halts and the other policy rules are
// "shared_policy" (alert OrderSharedPolicyBlockAtOMS).
func violationType(rule string) string {
	switch rule {
	case sharedrisk.RuleMaxPositionBTC:
		return "position_limits"
	case sharedrisk.RuleMaxOrderNotional, sharedrisk.RuleMaxOpenOrders, sharedrisk.RulePortfolioMaxOpenOrders:
		return "order_limits"
	case sharedrisk.RuleMaxOrdersPerMinute, sharedrisk.RulePortfolioMaxOrdersPerMinute:
		return "rate_limit"
	}
	return "shared_policy"
}

// blockedBy runs the effective policy and returns an error listing the
// blocking findings whose rule is in rules (nil if none).
func (rm *Manager) blockedBy(ctx context.Context, order *models.Order, rules ...string) error {
	want := map[string]bool{}
	for _, r := range rules {
		want[r] = true
	}
	var msgs []string
	for _, f := range rm.evaluate(ctx, order) {
		if want[f.Rule] {
			msgs = append(msgs, f.Message)
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(msgs, "; "))
}

// CheckPositionLimits checks if the order exceeds position limits (the
// effective policy's max_position_btc for the book).
func (rm *Manager) CheckPositionLimits(ctx context.Context, order *models.Order) error {
	defer rm.metrics.RecordRiskCheck("position_limits", "checked")
	return rm.blockedBy(ctx, order, sharedrisk.RuleMaxPositionBTC)
}

// CheckOrderLimits checks open orders (book and firm-wide), order size and
// notional against the effective policy.
func (rm *Manager) CheckOrderLimits(ctx context.Context, order *models.Order) error {
	defer rm.metrics.RecordRiskCheck("order_limits", "checked")
	return rm.blockedBy(ctx, order, sharedrisk.RuleMaxOpenOrders, sharedrisk.RulePortfolioMaxOpenOrders,
		sharedrisk.RuleMaxOrderNotional, sharedrisk.RuleMaxOrderBTC)
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

// CheckRateLimit reports whether one more order would exceed the effective
// orders-per-minute limits for book (firm-wide and per book). It does not
// count anything: CheckRisk records each accepted order.
func (rm *Manager) CheckRateLimit(ctx context.Context, book string) error {
	pol := rm.EffectivePolicy()
	bookN, total := rm.rate.count(book, rm.now())
	if l := pol.For(book).MaxOrdersPerMinute; l > 0 && bookN >= l {
		return fmt.Errorf("rate limit exceeded: %d orders on %s in the last minute (max: %d)", bookN, book, l)
	}
	if pf := pol.Portfolio; pf != nil && pf.MaxOrdersPerMinute > 0 && total >= pf.MaxOrdersPerMinute {
		return fmt.Errorf("rate limit exceeded: %d orders in the last minute (max: %d)", total, pf.MaxOrdersPerMinute)
	}
	return nil
}

// GetPositionLimits returns position limits for a book (effective policy).
func (rm *Manager) GetPositionLimits(book string) (*PositionLimits, error) {
	l := rm.EffectivePolicy().For(book)
	return &PositionLimits{
		MaxSize:  l.MaxPositionBTC,
		MaxValue: l.MaxOrderNotional * 10, // Example: 10x order value
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
	rm.rate.reset()
}
