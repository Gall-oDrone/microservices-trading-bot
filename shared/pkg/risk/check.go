package risk

import (
	"fmt"
	"math"
)

// Severity of a finding. Block findings must stop the order; Warn findings
// are shown to the operator and logged but never stop anything.
type Severity string

const (
	Block Severity = "block"
	Warn  Severity = "warn"
)

// Rule identifiers, stable for logs, ledgers and the UI.
const (
	RuleHalted           = "halted"
	RuleInvalidOrder     = "invalid_order"
	RuleMaxOrderBTC      = "max_order_btc"
	RuleMaxPositionBTC   = "max_position_btc"
	RuleMaxOrderNotional = "max_order_notional"
	RuleMaxOrdersPerDay  = "max_orders_per_day"
	RulePriceDeviation   = "max_price_deviation_bps"
	RuleDrawdownWarn     = "drawdown_warn"
	RuleCostWarn         = "cost_warn_bps"
)

// Finding is one limit that was hit (or nearly hit, for warnings).
type Finding struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Limit    float64  `json:"limit"`
	Value    float64  `json:"value"`
	Message  string   `json:"message"`
}

// Order is what the executor is about to send. Side is "buy" or "sell".
// Price is the price the order is expected to fill near (e.g. best bid/ask);
// RefPrice is an independent reference (e.g. the decision bar's close) used
// by the price-deviation guard. Zero RefPrice skips that guard.
type Order struct {
	Book     string  `json:"book"`
	Side     string  `json:"side"`
	QtyBTC   float64 `json:"qty_btc"`
	Price    float64 `json:"price"`
	RefPrice float64 `json:"ref_price"`
}

// State is what the executor holds before the order.
type State struct {
	PositionBTC float64 `json:"position_btc"`
	OrdersToday int     `json:"orders_today"`
}

// Decision is the outcome of Check.
type Decision struct {
	Allowed  bool      `json:"allowed"`
	Findings []Finding `json:"findings"`
}

// Check evaluates an order against the policy. It is pure: no I/O, no clock.
// Sells that only reduce the position are never blocked by size, position or
// notional limits, so a limit can never trap the executor in a position;
// only the halt and the price guard apply to them.
func Check(p Policy, o Order, s State) Decision {
	l := p.For(o.Book)
	var f []Finding
	block := func(rule string, limit, value float64, format string, args ...any) {
		f = append(f, Finding{Rule: rule, Severity: Block, Limit: limit, Value: value, Message: fmt.Sprintf(format, args...)})
	}

	if p.Halted {
		msg := "trading is halted"
		if p.HaltReason != "" {
			msg += ": " + p.HaltReason
		}
		block(RuleHalted, 0, 1, "%s", msg)
	}
	if (o.Side != "buy" && o.Side != "sell") || o.QtyBTC <= 0 || math.IsNaN(o.QtyBTC) || o.Price < 0 {
		block(RuleInvalidOrder, 0, o.QtyBTC, "invalid order: side %q qty %v price %v", o.Side, o.QtyBTC, o.Price)
		return Decision{Allowed: false, Findings: f}
	}
	reducing := o.Side == "sell" && o.QtyBTC <= s.PositionBTC+1e-12

	if !reducing {
		if l.MaxOrderBTC > 0 && o.QtyBTC > l.MaxOrderBTC+1e-12 {
			block(RuleMaxOrderBTC, l.MaxOrderBTC, o.QtyBTC, "order %.8f BTC exceeds max %.8f BTC", o.QtyBTC, l.MaxOrderBTC)
		}
		after := s.PositionBTC
		if o.Side == "buy" {
			after += o.QtyBTC
		} else {
			after -= o.QtyBTC
		}
		if l.MaxPositionBTC > 0 && math.Abs(after) > l.MaxPositionBTC+1e-12 {
			block(RuleMaxPositionBTC, l.MaxPositionBTC, math.Abs(after), "position after fill %.8f BTC exceeds max %.8f BTC", after, l.MaxPositionBTC)
		}
		if n := o.QtyBTC * o.Price; l.MaxOrderNotional > 0 && n > l.MaxOrderNotional {
			block(RuleMaxOrderNotional, l.MaxOrderNotional, n, "notional %.2f exceeds max %.2f", n, l.MaxOrderNotional)
		}
		if l.MaxOrdersPerDay > 0 && s.OrdersToday >= l.MaxOrdersPerDay {
			block(RuleMaxOrdersPerDay, float64(l.MaxOrdersPerDay), float64(s.OrdersToday+1), "would be order %d today, max %d", s.OrdersToday+1, l.MaxOrdersPerDay)
		}
	}
	if dev, ok := DeviationBps(o.Price, o.RefPrice); ok && l.MaxPriceDeviationBps > 0 && dev > l.MaxPriceDeviationBps {
		block(RulePriceDeviation, l.MaxPriceDeviationBps, dev, "price %.2f is %.0f bps from reference %.2f (max %.0f)", o.Price, dev, o.RefPrice, l.MaxPriceDeviationBps)
	}
	return Decision{Allowed: len(f) == 0, Findings: f}
}

// DeviationBps is |price/ref - 1| in bps. ok is false when either is not
// positive.
func DeviationBps(price, ref float64) (float64, bool) {
	if price <= 0 || ref <= 0 {
		return 0, false
	}
	return math.Abs(price/ref-1) * 1e4, true
}

// AssessDrawdown returns a warning when the observed max drawdown (fraction)
// is above the book's review threshold. Never blocks.
func AssessDrawdown(p Policy, book, label string, maxDrawdown float64) *Finding {
	l := p.For(book)
	if l.DrawdownWarn <= 0 || maxDrawdown <= l.DrawdownWarn {
		return nil
	}
	return &Finding{
		Rule: RuleDrawdownWarn, Severity: Warn, Limit: l.DrawdownWarn, Value: maxDrawdown,
		Message: fmt.Sprintf("%s max drawdown %.1f%% is above the %.0f%% review level (stopping needs a written reason)",
			label, maxDrawdown*100, l.DrawdownWarn*100),
	}
}

// AssessCost returns a warning when a filled leg cost more than the book's
// threshold, in bps of notional.
func AssessCost(p Policy, book, label string, costBps float64) *Finding {
	l := p.For(book)
	if l.CostWarnBps <= 0 || costBps <= l.CostWarnBps {
		return nil
	}
	return &Finding{
		Rule: RuleCostWarn, Severity: Warn, Limit: l.CostWarnBps, Value: costBps,
		Message: fmt.Sprintf("%s cost %.0f bps, above the %.0f bps warning level", label, costBps, l.CostWarnBps),
	}
}

// Utilization is value/limit, or 0 when the limit is disabled.
func Utilization(value, limit float64) float64 {
	if limit <= 0 {
		return 0
	}
	return value / limit
}
