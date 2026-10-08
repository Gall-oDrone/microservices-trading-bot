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

// pendingSettleWait bounds how long pre-trade validation waits for the
// trading.signals consumer to finish validating and risk-checking a row it has
// just created (status pending). Until then risk has not been applied, so the
// row cannot vouch for the order (risk step R5: trading-engine must never place
// an order that order-management rejects). A var so tests can shorten it.
var pendingSettleWait = 2 * time.Second

const pendingSettlePoll = 25 * time.Millisecond

// resolvePreTradeDuplicate implements idempotent pre-trade validation when order-management has
// already created a Redis row for this signal_id (trading.signals consumer) and trading-engine
// calls POST /api/v1/orders/validate with a temporary order id.
// Returns idempotentOK=true when the existing row matches proposed economics, is not yet placed on
// Bitso, and has passed the consumer's validation and risk check. A row still pending is waited on
// (up to pendingSettleWait): rejected or still pending means an error, never an approval.
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

	if err := preTradeRowConflict(order, existing); err != nil {
		return false, err
	}

	if !preTradeEconomicsMatch(existing, order) {
		return false, fmt.Errorf(
			"signal %s conflict: proposed economics differ from existing order %s",
			order.SignalID, existing.ID,
		)
	}

	if existing.Status == models.OrderStatusPending {
		settled, err := v.waitPendingSettled(ctx, existing)
		if err != nil {
			return false, err
		}
		if err := preTradeRowConflict(order, settled); err != nil {
			return false, err
		}
	}

	return true, nil
}

// preTradeRowConflict reports why an existing row for the signal rules the proposed order out:
// already placed on the exchange, terminal (with the rejection reason), or an unexpected status.
func preTradeRowConflict(order, existing *models.Order) error {
	if metaBitsoOrderID(existing) != "" {
		return fmt.Errorf(
			"duplicate order for signal %s (already placed on exchange, existing order: %s)",
			order.SignalID, existing.ID,
		)
	}

	if terminalPreTradeConflict(existing.Status) {
		reason := ""
		if existing.Status == models.OrderStatusRejected && existing.RejectionReason != "" {
			reason = ": " + existing.RejectionReason
		}
		return fmt.Errorf(
			"duplicate order for signal %s (existing order %s in terminal status %s%s)",
			order.SignalID, existing.ID, existing.Status, reason,
		)
	}

	if !prePlacementStatus(existing.Status) {
		return fmt.Errorf(
			"duplicate order for signal %s (existing order %s unexpected status %s)",
			order.SignalID, existing.ID, existing.Status,
		)
	}
	return nil
}

// waitPendingSettled re-reads a pending row until the consumer moves it on (validated, rejected,
// …) or pendingSettleWait passes. Still pending at the deadline is an error: fail closed.
func (v *Validator) waitPendingSettled(ctx context.Context, existing *models.Order) (*models.Order, error) {
	deadline := time.Now().Add(pendingSettleWait)
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("signal %s: waiting for order-management's risk check: %w", existing.SignalID, ctx.Err())
		case <-time.After(pendingSettlePoll):
		}
		cur, err := v.repo.Get(ctx, existing.ID)
		if err != nil {
			return nil, fmt.Errorf("signal %s: re-read order %s: %w", existing.SignalID, existing.ID, err)
		}
		if cur.Status != models.OrderStatusPending {
			return cur, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf(
				"signal %s: order-management has not finished validating order %s (still pending after %s); not approving",
				existing.SignalID, existing.ID, pendingSettleWait,
			)
		}
	}
}
