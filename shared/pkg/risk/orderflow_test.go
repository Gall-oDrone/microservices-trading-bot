package risk

import (
	"encoding/json"
	"strings"
	"testing"
)

// Limits added 2026-10-08 for order-management (open orders, orders per
// minute, firm-wide). They must be inert for callers that do not track them.

func TestOrderFlowLimits(t *testing.T) {
	p := Policy{
		Version: "t",
		Books: map[string]BookLimits{
			"btc_mxn": {MaxOpenOrders: 2, MaxOrdersPerMinute: 3},
		},
		Portfolio: &PortfolioLimits{MaxOpenOrders: 5, MaxOrdersPerMinute: 10},
	}
	buy := Order{Book: "btc_mxn", Side: "buy", QtyBTC: 0.001, Price: 1}
	cases := []struct {
		name string
		s    State
		rule string // "" = allowed
	}{
		{"under every limit", State{OpenOrders: 1, OrdersLastMinute: 2, PortfolioOpenOrders: 4, PortfolioOrdersLastMinute: 9}, ""},
		{"book open orders", State{OpenOrders: 2}, RuleMaxOpenOrders},
		{"book rate", State{OrdersLastMinute: 3}, RuleMaxOrdersPerMinute},
		{"portfolio open orders", State{PortfolioOpenOrders: 5}, RulePortfolioMaxOpenOrders},
		{"portfolio rate", State{PortfolioOrdersLastMinute: 10}, RulePortfolioMaxOrdersPerMinute},
	}
	for _, c := range cases {
		d := Check(p, buy, c.s)
		if c.rule == "" {
			if !d.Allowed {
				t.Errorf("%s: want allowed, got %+v", c.name, d.Findings)
			}
			continue
		}
		if d.Allowed || !has(d, c.rule) {
			t.Errorf("%s: want %s, got %+v", c.name, c.rule, d)
		}
	}
}

func TestOrderFlowLimitsNeverTrapAReducingSell(t *testing.T) {
	p := Policy{
		Books:     map[string]BookLimits{"btc_mxn": {MaxOpenOrders: 1, MaxOrdersPerMinute: 1}},
		Portfolio: &PortfolioLimits{MaxOpenOrders: 1, MaxOrdersPerMinute: 1},
	}
	s := State{PositionBTC: 0.01, OpenOrders: 9, OrdersLastMinute: 9, PortfolioOpenOrders: 9, PortfolioOrdersLastMinute: 9}
	if d := Check(p, Order{Book: "btc_mxn", Side: "sell", QtyBTC: 0.01, Price: 1}, s); !d.Allowed {
		t.Fatalf("reducing sell blocked: %+v", d.Findings)
	}
}

func TestOrderFlowLimitsDisabledByDefault(t *testing.T) {
	// Callers that do not know open orders or rates (daily-executor,
	// trading-engine) pass zero State fields: never a block.
	busy := State{OpenOrders: 1000, OrdersLastMinute: 1000, PortfolioOpenOrders: 1000, PortfolioOrdersLastMinute: 1000}
	if d := Check(DefaultPolicy(), Order{Book: "btc_mxn", Side: "buy", QtyBTC: 0.001, Price: 1}, busy); !d.Allowed {
		t.Fatalf("default policy must not enforce order-flow limits: %+v", d.Findings)
	}
}

func TestNewFieldsDoNotChangeExistingJSON(t *testing.T) {
	// The daily-executor records policy versions and risk states in its
	// ledger; the additive fields must not appear when unset.
	b, err := json.Marshal(DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	st, _ := json.Marshal(State{PositionBTC: 0.001, OrdersToday: 1})
	for _, k := range []string{"max_open_orders", "max_orders_per_minute", "portfolio"} {
		if strings.Contains(string(b), k) {
			t.Errorf("DefaultPolicy JSON gained %q: %s", k, b)
		}
	}
	if string(st) != `{"position_btc":0.001,"orders_today":1}` {
		t.Errorf("State JSON changed: %s", st)
	}
}

func TestValidateRejectsNegativeOrderFlowLimits(t *testing.T) {
	bad := []Policy{
		{Default: BookLimits{MaxOpenOrders: -1}},
		{Books: map[string]BookLimits{"btc_mxn": {MaxOrdersPerMinute: -1}}},
		{Portfolio: &PortfolioLimits{MaxOpenOrders: -1}},
	}
	for i, p := range bad {
		if p.Validate() == nil {
			t.Errorf("case %d: want error", i)
		}
	}
}
