package validator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bitso-trading-platform/order-management/internal/config"
	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/repository"
	sharedModels "bitso-trading-platform/shared/pkg/models"
)

// OrderValidator validates orders and signals
type OrderValidator interface {
	ValidateSignal(signal *sharedModels.TradeSignalEvent) error
	ValidateOrder(order *models.Order) error
	// PreTradeValidateOrder validates a proposed order for trading-engine pre-trade checks.
	// When idempotentMatched is true, the signal row was already created by the trading.signals consumer
	// with matching economics — callers should skip re-running risk (already applied at ingest).
	PreTradeValidateOrder(order *models.Order) (idempotentMatched bool, err error)
	ValidateOrderSize(order *models.Order) error
	ValidateOrderValue(order *models.Order) error
}

// Validator implements OrderValidator
type Validator struct {
	logger  *logger.Logger
	config  *config.RiskConfig
	repo    repository.OrderRepository
	metrics *metrics.MetricsCollector
}

// NewOrderValidator creates a new order validator
func NewOrderValidator(
	config *config.RiskConfig,
	logger *logger.Logger,
	repo repository.OrderRepository,
	metrics *metrics.MetricsCollector,
) *Validator {
	return &Validator{
		logger:  logger,
		config:  config,
		repo:    repo,
		metrics: metrics,
	}
}

// ValidateSignal validates a trading signal
func (v *Validator) ValidateSignal(signal *sharedModels.TradeSignalEvent) error {
	start := time.Now()
	defer func() {
		v.metrics.RecordValidationDuration("signal", time.Since(start))
	}()

	result := models.NewValidationResult()

	// Validate signal fields
	if signal.EventID == "" {
		result.AddRequiredError("event_id")
	}

	if signal.Book == "" {
		result.AddRequiredError("book")
	}

	if signal.Signal == "" {
		result.AddRequiredError("signal")
	}

	// Validate signal type
	validSignals := map[string]bool{"BUY": true, "SELL": true, "HOLD": true}
	if !validSignals[signal.Signal] {
		result.AddFieldError("signal", fmt.Sprintf("invalid signal type: %s (must be BUY, SELL, or HOLD)", signal.Signal))
	}

	// Validate price
	if signal.Price <= 0 {
		result.AddFieldError("price", fmt.Sprintf("price must be positive: %f", signal.Price))
	}

	// Validate amount
	if signal.Amount <= 0 {
		result.AddFieldError("amount", fmt.Sprintf("amount must be positive: %f", signal.Amount))
	}

	// Validate book format (e.g., "btc_mxn")
	if err := v.ValidateBook(signal.Book); err != nil {
		result.AddFieldError("book", err.Error())
	}

	// Record metrics
	if result.Valid {
		v.metrics.RecordValidation("signal", "success")
	} else {
		v.metrics.RecordValidation("signal", "failed")
	}

	if result.HasErrors() {
		return fmt.Errorf("signal validation failed: %w", fmt.Errorf("%s", result.Error()))
	}

	return nil
}

// validateOrderStructure runs structural/risk-bounds checks (not duplicate / pre-trade idempotency).
func (v *Validator) validateOrderStructure(order *models.Order) *models.ValidationResult {
	result := models.NewValidationResult()

	if err := order.Validate(); err != nil {
		result.AddFieldError("order", err.Error())
	}

	if err := v.ValidateBook(order.Book); err != nil {
		result.AddFieldError("book", err.Error())
	}

	if err := v.ValidateSide(order.Side); err != nil {
		result.AddFieldError("side", err.Error())
	}

	if err := v.ValidateType(order.Type); err != nil {
		result.AddFieldError("type", err.Error())
	}

	if err := v.ValidateOrderSize(order); err != nil {
		result.AddFieldError("amount", err.Error())
	}

	if err := v.ValidateOrderValue(order); err != nil {
		result.AddFieldError("value", err.Error())
	}

	return result
}

// ValidateOrder validates an order (full duplicate check for non–pre-trade callers).
func (v *Validator) ValidateOrder(order *models.Order) error {
	start := time.Now()
	defer func() {
		v.metrics.RecordValidationDuration("order", time.Since(start))
	}()

	result := v.validateOrderStructure(order)

	if v.config.EnableDuplicateCheck {
		if err := v.CheckDuplicates(order); err != nil {
			result.AddError("duplicate", err.Error(), models.ErrorCodeDuplicate)
		}
	}

	if result.Valid {
		v.metrics.RecordValidation("order", "success")
	} else {
		v.metrics.RecordValidation("order", "failed")
	}

	if result.HasErrors() {
		return fmt.Errorf("order validation failed: %w", fmt.Errorf("%s", result.Error()))
	}

	return nil
}

// PreTradeValidateOrder validates trading-engine pre-trade requests; allows idempotent approval when
// the trading.signals consumer already created the canonical order row for this signal_id.
func (v *Validator) PreTradeValidateOrder(order *models.Order) (idempotentMatched bool, err error) {
	start := time.Now()
	defer func() {
		v.metrics.RecordValidationDuration("order_pre_trade", time.Since(start))
	}()

	result := v.validateOrderStructure(order)
	if result.HasErrors() {
		v.metrics.RecordValidation("order_pre_trade", "failed")
		return false, fmt.Errorf("order validation failed: %w", fmt.Errorf("%s", result.Error()))
	}

	if !v.config.EnableDuplicateCheck {
		v.metrics.RecordValidation("order_pre_trade", "success")
		return false, nil
	}

	idemp, resolveErr := v.resolvePreTradeDuplicate(order)
	if resolveErr != nil {
		result.AddError("duplicate", resolveErr.Error(), models.ErrorCodeDuplicate)
		v.metrics.RecordValidation("order_pre_trade", "failed")
		return false, fmt.Errorf("order validation failed: %w", fmt.Errorf("%s", result.Error()))
	}
	if idemp {
		v.metrics.RecordValidation("pre_trade_idempotent", "ok")
		return true, nil
	}

	if err := v.CheckDuplicates(order); err != nil {
		result.AddError("duplicate", err.Error(), models.ErrorCodeDuplicate)
		v.metrics.RecordValidation("order_pre_trade", "failed")
		return false, fmt.Errorf("order validation failed: %w", fmt.Errorf("%s", result.Error()))
	}

	v.metrics.RecordValidation("order_pre_trade", "success")
	return false, nil
}

// ValidateOrderSize validates the order size against limits
func (v *Validator) ValidateOrderSize(order *models.Order) error {
	if order.Amount < v.config.MinOrderSize {
		return fmt.Errorf("order size %f below minimum %f", order.Amount, v.config.MinOrderSize)
	}

	return nil
}

// ValidateOrderValue validates the order value against limits
func (v *Validator) ValidateOrderValue(order *models.Order) error {
	orderValue := order.Amount * order.Price

	if orderValue > v.config.MaxOrderValue {
		return fmt.Errorf("order value %f exceeds maximum %f", orderValue, v.config.MaxOrderValue)
	}

	return nil
}

// ValidateBook validates the trading book format
func (v *Validator) ValidateBook(book string) error {
	if book == "" {
		return fmt.Errorf("book is required")
	}

	// Check format: should be "major_minor" (e.g., "btc_mxn")
	parts := strings.Split(book, "_")
	if len(parts) != 2 {
		return fmt.Errorf("invalid book format: %s (expected format: major_minor)", book)
	}

	// Validate each part is not empty
	if parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid book format: %s (both major and minor currencies required)", book)
	}

	return nil
}

// ValidateSide validates the order side
func (v *Validator) ValidateSide(side string) error {
	validSides := map[string]bool{"buy": true, "sell": true}
	if !validSides[side] {
		return fmt.Errorf("invalid side: %s (must be 'buy' or 'sell')", side)
	}
	return nil
}

// ValidateType validates the order type
func (v *Validator) ValidateType(orderType string) error {
	validTypes := map[string]bool{"market": true, "limit": true}
	if !validTypes[orderType] {
		return fmt.Errorf("invalid type: %s (must be 'market' or 'limit')", orderType)
	}
	return nil
}

// CheckDuplicates checks if an order with the same signal ID already exists
func (v *Validator) CheckDuplicates(order *models.Order) error {
	if order.SignalID == "" {
		return nil // No signal ID to check
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check if order exists for this signal
	existing, err := v.repo.GetBySignalID(ctx, order.SignalID)
	if err == nil && existing != nil && existing.ID != order.ID {
		return fmt.Errorf("duplicate order for signal %s (existing order: %s)", order.SignalID, existing.ID)
	}

	return nil
}

// ValidateBatch validates a batch of orders
func (v *Validator) ValidateBatch(orders []*models.Order) map[string]error {
	errors := make(map[string]error)

	for _, order := range orders {
		if err := v.ValidateOrder(order); err != nil {
			errors[order.ID] = err
		}
	}

	return errors
}

// ValidateStateTransition validates if a state transition is allowed
func (v *Validator) ValidateStateTransition(from, to models.OrderStatus) error {
	// This will be used by the state machine
	// For now, basic validation
	if from == to {
		return fmt.Errorf("cannot transition to same state: %s", from)
	}

	// Cannot transition from final states
	finalStates := map[models.OrderStatus]bool{
		models.OrderStatusFilled:    true,
		models.OrderStatusCancelled: true,
		models.OrderStatusRejected:  true,
	}

	if finalStates[from] {
		return fmt.Errorf("cannot transition from final state: %s", from)
	}

	return nil
}
