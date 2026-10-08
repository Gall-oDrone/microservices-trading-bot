// Package risk holds execution-side risk limits and a pure pre-trade check.
//
// Scope. The forward tests in docs/backtest-readiness/ freeze the trading
// rule (signal, timing, all-in/all-out). Nothing here may change a signal:
// these limits only decide whether an order the rule asks for is safe to
// send (size, notional, fat-finger price, order count, halt), and they report
// exposure and drawdown so an operator can see them. Stopping a forward test
// on bad results needs a written reason (pre-registration §2); drawdown
// thresholds here therefore only warn.
//
// The package is stdlib-only and Go 1.21 compatible so every module in the
// repo can import it through its `replace bitso-trading-platform/shared`.
package risk

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// BookLimits are the per-book limits. A zero value disables that limit,
// except where noted.
type BookLimits struct {
	// MaxOrderBTC caps the base quantity of a single order.
	MaxOrderBTC float64 `json:"max_order_btc"`
	// MaxPositionBTC caps what the executor may hold after a fill.
	MaxPositionBTC float64 `json:"max_position_btc"`
	// MaxOrderNotional caps quantity x price, in the book's quote currency.
	MaxOrderNotional float64 `json:"max_order_notional"`
	// MaxOrdersPerDay caps legs (not exchange orders) per Mexico City day.
	MaxOrdersPerDay int `json:"max_orders_per_day"`
	// MaxPriceDeviationBps rejects an order whose expected price is further
	// than this from the reference price (fat-finger / bad-data guard).
	MaxPriceDeviationBps float64 `json:"max_price_deviation_bps"`
	// DrawdownWarn is a fraction (0.25 = 25%). Above it the forward test is
	// flagged for review. Warn only: it never blocks an order.
	DrawdownWarn float64 `json:"drawdown_warn"`
	// CostWarnBps flags a filled leg whose realized cost (fees + slippage vs
	// the reference price) exceeds this many bps. Warn only.
	CostWarnBps float64 `json:"cost_warn_bps"`
	// MaxOpenOrders caps the book's resting (unfilled, uncancelled) orders,
	// counting the new one. Needs State.OpenOrders: only order-management
	// knows it; other callers leave it 0 and never trip it. Added 2026-10-08.
	MaxOpenOrders int `json:"max_open_orders,omitempty"`
	// MaxOrdersPerMinute caps accepted orders in the trailing 60 s (runaway
	// algorithm guard). Needs State.OrdersLastMinute (order-management).
	MaxOrdersPerMinute int `json:"max_orders_per_minute,omitempty"`
}

// PortfolioLimits apply across every book (firm-wide). Zero disables.
// Only order-management sees cross-book state, so only it fills the
// matching State fields. Added 2026-10-08 (order-management's former
// MAX_OPEN_ORDERS / MAX_ORDERS_PER_MINUTE env limits).
type PortfolioLimits struct {
	MaxOpenOrders      int `json:"max_open_orders,omitempty"`
	MaxOrdersPerMinute int `json:"max_orders_per_minute,omitempty"`
}

// Policy is the full set of limits plus the global halt.
type Policy struct {
	// Version is free text so a ledger line can say which policy it ran under.
	Version string `json:"version"`
	// Halted blocks every new order when true.
	Halted     bool   `json:"halted"`
	HaltReason string `json:"halt_reason,omitempty"`
	// Books maps a book (e.g. "btc_mxn") to its limits. Books not listed use
	// Default.
	Books   map[string]BookLimits `json:"books"`
	Default BookLimits            `json:"default"`
	// Portfolio holds firm-wide limits; nil means none.
	Portfolio *PortfolioLimits `json:"portfolio,omitempty"`
}

// DefaultPolicy mirrors what the daily-executor already enforces on stage
// (maxSize 0.01 BTC, one leg per book per day) and adds notional, price and
// review thresholds sized for the current 0.001 BTC forward-test legs.
func DefaultPolicy() Policy {
	return Policy{
		Version: "default-2026-10-03",
		Books: map[string]BookLimits{
			"btc_mxn": {
				MaxOrderBTC: 0.01, MaxPositionBTC: 0.01, MaxOrderNotional: 25000,
				MaxOrdersPerDay: 1, MaxPriceDeviationBps: 1500,
				DrawdownWarn: 0.25, CostWarnBps: 140,
			},
			"btc_usd": {
				MaxOrderBTC: 0.01, MaxPositionBTC: 0.01, MaxOrderNotional: 1500,
				MaxOrdersPerDay: 1, MaxPriceDeviationBps: 1500,
				DrawdownWarn: 0.25, CostWarnBps: 80,
			},
		},
		Default: BookLimits{
			MaxOrderBTC: 0.001, MaxPositionBTC: 0.001, MaxOrdersPerDay: 1,
			MaxPriceDeviationBps: 1000, DrawdownWarn: 0.20,
		},
	}
}

// For returns the limits that apply to book.
func (p Policy) For(book string) BookLimits {
	if l, ok := p.Books[strings.ToLower(book)]; ok {
		return l
	}
	return p.Default
}

// BookNames returns the configured books, sorted.
func (p Policy) BookNames() []string {
	out := make([]string, 0, len(p.Books))
	for b := range p.Books {
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

// Validate rejects negative limits and nonsensical fractions.
func (p Policy) Validate() error {
	check := func(name string, l BookLimits) error {
		switch {
		case l.MaxOrderBTC < 0, l.MaxPositionBTC < 0, l.MaxOrderNotional < 0,
			l.MaxOrdersPerDay < 0, l.MaxPriceDeviationBps < 0, l.CostWarnBps < 0,
			l.MaxOpenOrders < 0, l.MaxOrdersPerMinute < 0:
			return fmt.Errorf("%s: limits must be >= 0", name)
		case l.DrawdownWarn < 0 || l.DrawdownWarn >= 1:
			return fmt.Errorf("%s: drawdown_warn must be in [0, 1)", name)
		case l.MaxPositionBTC > 0 && l.MaxOrderBTC > l.MaxPositionBTC:
			return fmt.Errorf("%s: max_order_btc %v exceeds max_position_btc %v", name, l.MaxOrderBTC, l.MaxPositionBTC)
		}
		return nil
	}
	if err := check("default", p.Default); err != nil {
		return err
	}
	if pf := p.Portfolio; pf != nil && (pf.MaxOpenOrders < 0 || pf.MaxOrdersPerMinute < 0) {
		return fmt.Errorf("portfolio: limits must be >= 0")
	}
	for _, b := range p.BookNames() {
		if err := check(b, p.Books[b]); err != nil {
			return err
		}
	}
	return nil
}

// LoadPolicy reads a JSON policy file. An empty path returns DefaultPolicy.
// Fields missing from the file keep their zero value (limit disabled), so a
// file should be complete; start from `DefaultPolicy()` marshalled to JSON.
func LoadPolicy(path string) (Policy, error) {
	if path == "" {
		return DefaultPolicy(), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("risk policy: %w", err)
	}
	var p Policy
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Policy{}, fmt.Errorf("risk policy %s: %w", path, err)
	}
	if p.Books == nil {
		p.Books = map[string]BookLimits{}
	}
	if err := p.Validate(); err != nil {
		return Policy{}, fmt.Errorf("risk policy %s: %w", path, err)
	}
	return p, nil
}
