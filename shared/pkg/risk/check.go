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

	RuleMaxOpenOrders               = "max_open_orders"
	RuleMaxOrdersPerMinute          = "max_orders_per_minute"
	RulePortfolioMaxOpenOrders      = "portfolio_max_open_orders"
	RulePortfolioMaxOrdersPerMinute = "portfolio_max_orders_per_minute"

	RuleMaxLeverage          = "max_leverage"
	RuleMaxExposurePctEquity = "max_exposure_pct_equity"
	RuleMarketClosed         = "market_closed"
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
	QtyBTC   float64 `json:"qty_btc"` // base quantity: BTC on Bitso, units on a CFD book
	Price    float64 `json:"price"`
	RefPrice float64 `json:"ref_price"`
	// Leverage of a position-based order (0: not applicable).
	Leverage int `json:"leverage,omitempty"`
}

// State is what the executor holds before the order. The fields added on
// 2026-10-08 are omitempty so ledger lines written by callers that do not
// track them (the daily-executor) are unchanged.
type State struct {
	PositionBTC float64 `json:"position_btc"`
	OrdersToday int     `json:"orders_today"`
	// OpenOrders is the book's resting orders before this one.
	OpenOrders int `json:"open_orders,omitempty"`
	// OrdersLastMinute is the book's accepted orders in the trailing 60 s.
	OrdersLastMinute int `json:"orders_last_minute,omitempty"`
	// PortfolioOpenOrders / PortfolioOrdersLastMinute are the same across
	// every book.
	PortfolioOpenOrders       int `json:"portfolio_open_orders,omitempty"`
	PortfolioOrdersLastMinute int `json:"portfolio_orders_last_minute,omitempty"`
	// Position-based brokers (2026-10-10): account equity and total open
	// exposure before the order (account currency), and whether the market
	// the book trades in is closed now.
	Equity       float64 `json:"equity,omitempty"`
	Exposure     float64 `json:"exposure,omitempty"`
	MarketClosed bool    `json:"market_closed,omitempty"`
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
	if l.RequireMarketOpen && s.MarketClosed {
		block(RuleMarketClosed, 0, 1, "the market for %s is closed", o.Book)
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
		count := func(rule string, limit, have int, what string) {
			if limit > 0 && have >= limit {
				block(rule, float64(limit), float64(have+1), "would be %s %d, max %d", what, have+1, limit)
			}
		}
		count(RuleMaxOpenOrders, l.MaxOpenOrders, s.OpenOrders, "open order")
		count(RuleMaxOrdersPerMinute, l.MaxOrdersPerMinute, s.OrdersLastMinute, "order in the last minute")
		if pf := p.Portfolio; pf != nil {
			count(RulePortfolioMaxOpenOrders, pf.MaxOpenOrders, s.PortfolioOpenOrders, "open order across all books")
			count(RulePortfolioMaxOrdersPerMinute, pf.MaxOrdersPerMinute, s.PortfolioOrdersLastMinute, "order in the last minute across all books")
		}
		if l.MaxLeverage > 0 && o.Leverage > l.MaxLeverage {
			block(RuleMaxLeverage, float64(l.MaxLeverage), float64(o.Leverage), "leverage x%d exceeds max x%d", o.Leverage, l.MaxLeverage)
		}
		if l.MaxExposurePctEquity > 0 && s.Equity > 0 {
			lev := float64(o.Leverage)
			if lev < 1 {
				lev = 1
			}
			after := s.Exposure + o.QtyBTC*o.Price*lev
			if pct := after / s.Equity; pct > l.MaxExposurePctEquity {
				block(RuleMaxExposurePctEquity, l.MaxExposurePctEquity, pct, "exposure after the order %.2f is %.1f%% of equity %.2f (max %.1f%%)",
					after, pct*100, s.Equity, l.MaxExposurePctEquity*100)
			}
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
