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

	// Position-based brokers (eToro CFDs, added 2026-10-10 for the index-CFD
	// port). On those books Order.QtyBTC carries the instrument's units and
	// MaxOrderNotional is in the account currency.
	//
	// MaxLeverage caps Order.Leverage on orders that add exposure (porting
	// plan D4: x1).
	MaxLeverage int `json:"max_leverage,omitempty"`
	// MaxExposurePctEquity caps the account's total open exposure after the
	// order (State.Exposure + notional) as a fraction of State.Equity.
	// Needs State.Equity; a caller that leaves it 0 never trips it.
	MaxExposurePctEquity float64 `json:"max_exposure_pct_equity,omitempty"`
	// RequireMarketOpen blocks every order, closes included, while
	// State.MarketClosed is set (the executor acts in the cash session only).
	RequireMarketOpen bool `json:"require_market_open,omitempty"`

	// Protection (added 2026-10-19, docs/etoro/ETORO-ASSISTED-TRADE-PROTECTION-PLAN-2026-10-19.md).
	// These apply to orders that add exposure and carry Order.StopLoss /
	// Order.TakeProfit; closes are never trapped by them. Fractions of the
	// order price; zero disables.
	//
	// RequireStopLoss blocks an open that carries no stop loss.
	RequireStopLoss bool `json:"require_stop_loss,omitempty"`
	// MinStopLossPct / MaxStopLossPct bound the stop's distance from the
	// order price: too tight sits in the spread, too wide is not a stop.
	MinStopLossPct float64 `json:"min_stop_loss_pct,omitempty"`
	MaxStopLossPct float64 `json:"max_stop_loss_pct,omitempty"`
	// MaxLossPctEquity caps the loss at the stop, qty x (price - stop), as
	// a fraction of State.Equity. Needs State.Equity.
	MaxLossPctEquity float64 `json:"max_loss_pct_equity,omitempty"`

	// Real-account canary (added 2026-10-22, docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md
	// §6). On a leveraged CFD the order notional is the exposure (units x
	// price); the money the operator puts up is invested = exposure /
	// leverage. These apply to orders that add exposure; closes are never
	// trapped by them. Zero disables.
	//
	// MaxInvestedUSD caps invested (account currency).
	MaxInvestedUSD float64 `json:"max_invested_usd,omitempty"`
	// MaxPositions caps the account's open positions after the order,
	// across every instrument (State.OpenPositions + 1). Needs
	// State.OpenPositions; a caller that leaves it 0 never trips it.
	MaxPositions int `json:"max_positions,omitempty"`
	// Leverages is the set of leverage values an exposure-adding order may
	// carry; empty means any up to MaxLeverage.
	Leverages []int `json:"leverages,omitempty"`
	// MinInvestedUSD and DefaultInvestedUSD size the console's form on this
	// book (the venue's minimum and the figure the form pre-fills); they are
	// not enforced by Check (an order below the venue minimum is refused by
	// the venue). A book without them keeps the console's x1 defaults.
	MinInvestedUSD     float64 `json:"min_invested_usd,omitempty"`
	DefaultInvestedUSD float64 `json:"default_invested_usd,omitempty"`
}

// AllowsLeverage reports whether an exposure-adding order may carry lev:
// in Leverages when the set is given, else at most MaxLeverage (0: any).
func (l BookLimits) AllowsLeverage(lev int) bool {
	if len(l.Leverages) > 0 {
		for _, x := range l.Leverages {
			if x == lev {
				return true
			}
		}
		return false
	}
	return l.MaxLeverage <= 0 || lev <= l.MaxLeverage
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

// EtoroDemoPolicy is the built-in policy of the eToro index-CFD executor
// (cmd/etoro-daily-executor) on the demo account: x1 only, one leg per
// instrument per trading day, an order notional cap a little above the
// 1,100 USD forward-test size, total exposure at most 5 % of equity, the
// cash session only, and the same 10 % fat-finger guard as the default.
//
// It carries no stop-loss rule: the executor's rule positions are opened
// without one until the protection plan's phase C. Use
// EtoroDemoProtectedPolicy where a stop is required.
func EtoroDemoPolicy() Policy {
	cfd := BookLimits{
		MaxOrderNotional: 1500, MaxOrdersPerDay: 1, MaxPriceDeviationBps: 1000,
		DrawdownWarn: 0.20, CostWarnBps: 40,
		MaxLeverage: 1, MaxExposurePctEquity: 0.05, RequireMarketOpen: true,
	}
	return Policy{
		Version: "etoro-demo-2026-10-10",
		Books:   map[string]BookLimits{"nsdq100": cfd, "spx500": cfd},
		// Anything else on the account is refused outright.
		Default: BookLimits{MaxOrderNotional: 1, MaxOrdersPerDay: 1, MaxLeverage: 1, RequireMarketOpen: true},
	}
}

// EtoroDemoProtectedPolicy is EtoroDemoPolicy plus the protection rules
// decided on 2026-10-19 (plan §0, §2): an open must carry a stop loss, at
// most 10 % from the price and losing at most 0.5 % of equity at the stop;
// no minimum distance here (the venue's minStopLossPercentage and the
// noise floor live in shared/pkg/stops). The console's assisted opens use
// it from the plan's phase B; the executor from phase C with -protect.
func EtoroDemoProtectedPolicy() Policy {
	p := EtoroDemoPolicy()
	p.Version = "etoro-demo-2026-10-19"
	for _, b := range p.BookNames() {
		l := p.Books[b]
		l.RequireStopLoss, l.MaxStopLossPct, l.MaxLossPctEquity = true, 0.10, 0.005
		p.Books[b] = l
	}
	p.Default.RequireStopLoss = true
	return p
}

// EtoroDemoETFPolicy is the policy of the eToro ETF cells
// (FORWARD-TEST-PREREGISTRATION-SMA50-ETF-2026-10-21.md and
// FORWARD-TEST-PREREGISTRATION-DUAL-MOMENTUM-ETF-2026-10-21.md): the same
// per-leg limits as EtoroDemoPolicy on the books qqq and spy (x1 real
// asset, 1,100 USD), no stop-loss requirement (those cells carry no
// protection by design; a stop overlay would come with its own addendum
// and policy version), and everything else on the account refused.
// The executor selects it when every instrument of a run is an ETF; the
// CFD policies above are unchanged.
func EtoroDemoETFPolicy() Policy {
	etf := BookLimits{
		MaxOrderNotional: 1500, MaxOrdersPerDay: 1, MaxPriceDeviationBps: 1000,
		DrawdownWarn: 0.20, CostWarnBps: 40,
		MaxLeverage: 1, MaxExposurePctEquity: 0.05, RequireMarketOpen: true,
	}
	return Policy{
		Version: "etoro-demo-etf-2026-10-21",
		Books:   map[string]BookLimits{"qqq": etf, "spy": etf},
		Default: BookLimits{MaxOrderNotional: 1, MaxOrdersPerDay: 1, MaxLeverage: 1, RequireMarketOpen: true},
	}
}

// EtoroRealCanaryVersion is the version string the real-account
// acknowledgement must name (etoro.RealAckPolicyPrefix + date).
const EtoroRealCanaryVersion = "etoro-real-canary-2026-10-22"

// EtoroRealCanaryPolicy is the policy of the measurement-only canary on the
// REAL eToro account (docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md §6,
// decision D1 = B): NSDQ100 only, x20 and nothing else, 50 to 60 USD
// invested (1,000 to 1,200 USD of exposure: eToro's 1,000 USD minimum
// exposure reached from a 60 USD budget), one position on the whole account,
// one order per day, the cash session, the 10 % fat-finger guard, a stop
// required and held between 2.3 % and 2.45 % of the price. At x20 eToro caps
// the stop at 50 % of the invested amount, i.e. 2.5 % of the price; the
// policy stops just inside that so a market fill a few bps above the ask
// cannot push the stop past the venue's cap (an order the venue would
// refuse): the most the position can lose at the stop is 23 to 24.5 USD on
// 50 USD invested. MaxExposurePctEquity and MaxLossPctEquity are off: the
// account's equity is the canary and either cap would block every order.
//
// It is loaded only by a process started with -etoro-real-canary that
// passed the six-part acknowledgement (shared/pkg/etoro/realack.go) and, on
// the demo account, by -canary-rehearsal (EtoroRealCanaryRehearsalPolicy).
// It is not a path to leverage in the strategy: the demo policies above are
// unchanged.
func EtoroRealCanaryPolicy() Policy {
	canary := BookLimits{
		MaxOrderNotional: 1200, MaxOrdersPerDay: 1, MaxPriceDeviationBps: 1000,
		DrawdownWarn: 0.20, CostWarnBps: 40,
		MaxLeverage: 20, Leverages: []int{20}, RequireMarketOpen: true,
		RequireStopLoss: true, MinStopLossPct: 0.023, MaxStopLossPct: 0.0245,
		MaxInvestedUSD: 60, MinInvestedUSD: 50, DefaultInvestedUSD: 50, MaxPositions: 1,
	}
	return Policy{
		Version: EtoroRealCanaryVersion,
		Books:   map[string]BookLimits{"nsdq100": canary},
		// Anything else on the account is refused outright.
		Default: BookLimits{MaxOrderNotional: 1, MaxOrdersPerDay: 1, MaxLeverage: 1, RequireMarketOpen: true, RequireStopLoss: true},
	}
}

// EtoroRealCanaryRehearsalPolicy is the canary policy for the rehearsal on
// the DEMO account (plan §7, before any real order): the same book limits
// on nsdq100 and spx500 (so the rehearsal can use whichever index the demo
// cell is flat on) without the account-wide one-position cap, because the
// demo account also holds the forward-test cells' positions. Its version
// does not carry the etoro-real-canary- prefix, so the acknowledgement
// check can never accept it for the real account.
func EtoroRealCanaryRehearsalPolicy() Policy {
	p := EtoroRealCanaryPolicy()
	p.Version = "etoro-canary-rehearsal-2026-10-22"
	l := p.Books["nsdq100"]
	l.MaxPositions = 0
	l.Leverages = append([]int(nil), l.Leverages...)
	p.Books = map[string]BookLimits{"nsdq100": l, "spx500": l}
	return p
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
			l.MaxOpenOrders < 0, l.MaxOrdersPerMinute < 0, l.MaxLeverage < 0:
			return fmt.Errorf("%s: limits must be >= 0", name)
		case l.DrawdownWarn < 0 || l.DrawdownWarn >= 1:
			return fmt.Errorf("%s: drawdown_warn must be in [0, 1)", name)
		case l.MaxExposurePctEquity < 0:
			return fmt.Errorf("%s: max_exposure_pct_equity must be >= 0", name)
		case l.MaxPositionBTC > 0 && l.MaxOrderBTC > l.MaxPositionBTC:
			return fmt.Errorf("%s: max_order_btc %v exceeds max_position_btc %v", name, l.MaxOrderBTC, l.MaxPositionBTC)
		case l.MinStopLossPct < 0 || l.MinStopLossPct >= 1 || l.MaxStopLossPct < 0 || l.MaxStopLossPct > 1:
			return fmt.Errorf("%s: min_stop_loss_pct must be in [0, 1) and max_stop_loss_pct in [0, 1]", name)
		case l.MaxStopLossPct > 0 && l.MinStopLossPct > l.MaxStopLossPct:
			return fmt.Errorf("%s: min_stop_loss_pct %v exceeds max_stop_loss_pct %v", name, l.MinStopLossPct, l.MaxStopLossPct)
		case l.MaxLossPctEquity < 0 || l.MaxLossPctEquity >= 1:
			return fmt.Errorf("%s: max_loss_pct_equity must be in [0, 1)", name)
		case l.MaxInvestedUSD < 0 || l.MinInvestedUSD < 0 || l.DefaultInvestedUSD < 0 || l.MaxPositions < 0:
			return fmt.Errorf("%s: invested and position limits must be >= 0", name)
		case l.MaxInvestedUSD > 0 && l.MinInvestedUSD > l.MaxInvestedUSD:
			return fmt.Errorf("%s: min_invested_usd %v exceeds max_invested_usd %v", name, l.MinInvestedUSD, l.MaxInvestedUSD)
		case l.DefaultInvestedUSD > 0 && (l.DefaultInvestedUSD < l.MinInvestedUSD || (l.MaxInvestedUSD > 0 && l.DefaultInvestedUSD > l.MaxInvestedUSD)):
			return fmt.Errorf("%s: default_invested_usd %v is outside [min, max] invested", name, l.DefaultInvestedUSD)
		}
		for _, lev := range l.Leverages {
			if lev < 1 || (l.MaxLeverage > 0 && lev > l.MaxLeverage) {
				return fmt.Errorf("%s: leverages must be >= 1 and <= max_leverage (got x%d)", name, lev)
			}
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
