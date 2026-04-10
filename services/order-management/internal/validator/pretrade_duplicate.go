package validator

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"bitso-trading-platform/order-management/internal/models"
)

// preTradePriceEpsilon is max absolute price difference (quote, e.g. MXN) for idempotent match.
const preTradePriceEpsilon = 0.01

// preTradeAmountEpsilon is max absolute base-asset difference for idempotent match.
const preTradeAmountEpsilon = 1e-12

func metaBitsoOrderID(o *models.Order) string {
	if o == nil || o.Metadata == nil {
		return ""
	}
	s, _ := o.Metadata["bitso_order_id"].(string)
	return strings.TrimSpace(s)
}

func terminalPreTradeConflict(s models.OrderStatus) bool {
	switch s {
	case models.OrderStatusFilled, models.OrderStatusCancelled, models.OrderStatusRejected:
		return true
	default:
		return false
	}
}

// prePlacementStatus is true while the order may still be awaiting or undergoing exchange placement
// without a completed lifecycle (no Bitso OID required yet — checked separately).
func prePlacementStatus(s models.OrderStatus) bool {
	switch s {
	case models.OrderStatusPending, models.OrderStatusValidated, models.OrderStatusSubmitted,
		models.OrderStatusAccepted, models.OrderStatusPartiallyFilled:
		return true
	default:
		return false
	}
}

func preTradeEconomicsMatch(existing, proposed *models.Order) bool {
	if existing == nil || proposed == nil {
		return false
	}
	if existing.Book != proposed.Book {
		return false
	}
	if existing.Side != proposed.Side {
		return false
	}
	es := strings.TrimSpace(existing.Strategy)
	ps := strings.TrimSpace(proposed.Strategy)
	if es != "" && ps != "" && es != ps {
		return false
	}
	if !nearlyEqualFloat(existing.Price, proposed.Price, preTradePriceEpsilon) {
		return false
	}
	if !nearlyEqualFloat(existing.Amount, proposed.Amount, preTradeAmountEpsilon) {
		return false
	}
	return true
}

func nearlyEqualFloat(a, b, absEps float64) bool {
	if a == b {
		return true
	}
	return math.Abs(a-b) <= absEps
}

// resolvePreTradeDuplicate implements idempotent pre-trade validation when order-management has
// already created a Redis row for this signal_id (trading.signals consumer) and trading-engine
// calls POST /api/v1/orders/validate with a temporary order id.
// Returns idempotentOK=true when the existing row matches proposed economics and is not yet placed on Bitso.
func (v *Validator) resolvePreTradeDuplicate(order *models.Order) (idempotentOK bool, err error) {
	if order.SignalID == "" {
		return false, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	existing, getErr := v.repo.GetBySignalID(ctx, order.SignalID)
	if getErr != nil || existing == nil {
		return false, nil
	}
	if existing.ID == order.ID {
		return false, nil
	}

	if metaBitsoOrderID(existing) != "" {
		return false, fmt.Errorf(
			"duplicate order for signal %s (already placed on exchange, existing order: %s)",
			order.SignalID, existing.ID,
		)
	}

	if terminalPreTradeConflict(existing.Status) {
		return false, fmt.Errorf(
			"duplicate order for signal %s (existing order %s in terminal status %s)",
			order.SignalID, existing.ID, existing.Status,
		)
	}

	if !prePlacementStatus(existing.Status) {
		return false, fmt.Errorf(
			"duplicate order for signal %s (existing order %s unexpected status %s)",
			order.SignalID, existing.ID, existing.Status,
		)
	}

	if !preTradeEconomicsMatch(existing, order) {
		return false, fmt.Errorf(
			"signal %s conflict: proposed economics differ from existing order %s",
			order.SignalID, existing.ID,
		)
	}

	return true, nil
}
