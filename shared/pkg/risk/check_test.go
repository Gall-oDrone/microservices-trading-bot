package risk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func has(d Decision, rule string) bool {
	for _, f := range d.Findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func TestCheckAllowsForwardTestLeg(t *testing.T) {
	p := DefaultPolicy()
	d := Check(p, Order{Book: "btc_mxn", Side: "buy", QtyBTC: 0.001, Price: 1_511_000, RefPrice: 1_493_130}, State{})
	if !d.Allowed || len(d.Findings) != 0 {
		t.Fatalf("want allowed, got %+v", d)
	}
}

func TestCheckBlocks(t *testing.T) {
	p := DefaultPolicy()
	cases := []struct {
		name string
		o    Order
		s    State
		p    func(*Policy)
		rule string
	}{
		{"size", Order{Book: "btc_mxn", Side: "buy", QtyBTC: 0.02, Price: 1}, State{}, nil, RuleMaxOrderBTC},
		{"position", Order{Book: "btc_mxn", Side: "buy", QtyBTC: 0.005, Price: 1}, State{PositionBTC: 0.006}, nil, RuleMaxPositionBTC},
		{"notional", Order{Book: "btc_usd", Side: "buy", QtyBTC: 0.01, Price: 200_000}, State{}, nil, RuleMaxOrderNotional},
		{"per day", Order{Book: "btc_mxn", Side: "buy", QtyBTC: 0.001, Price: 1}, State{OrdersToday: 1}, nil, RuleMaxOrdersPerDay},
		{"fat finger", Order{Book: "btc_mxn", Side: "buy", QtyBTC: 0.001, Price: 2_000_000, RefPrice: 1_500_000}, State{}, nil, RulePriceDeviation},
		{"halt", Order{Book: "btc_mxn", Side: "buy", QtyBTC: 0.001, Price: 1}, State{}, func(p *Policy) { p.Halted = true; p.HaltReason = "test" }, RuleHalted},
		{"bad side", Order{Book: "btc_mxn", Side: "short", QtyBTC: 0.001, Price: 1}, State{}, nil, RuleInvalidOrder},
		{"zero qty", Order{Book: "btc_mxn", Side: "buy", QtyBTC: 0, Price: 1}, State{}, nil, RuleInvalidOrder},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pp := p
			if c.p != nil {
				c.p(&pp)
			}
			d := Check(pp, c.o, c.s)
			if d.Allowed || !has(d, c.rule) {
				t.Fatalf("want block on %s, got %+v", c.rule, d)
			}
		})
	}
}

func TestReducingSellIsNeverTrappedBySizeLimits(t *testing.T) {
	p := DefaultPolicy()
	// Position above every size limit (e.g. limits were tightened after entry).
	d := Check(p, Order{Book: "btc_usd", Side: "sell", QtyBTC: 0.05, Price: 100_000}, State{PositionBTC: 0.05, OrdersToday: 3})
	if !d.Allowed {
		t.Fatalf("reducing sell must pass, got %+v", d)
	}
	// But the halt still applies.
	p.Halted = true
	if Check(p, Order{Book: "btc_usd", Side: "sell", QtyBTC: 0.001, Price: 1}, State{PositionBTC: 0.001}).Allowed {
		t.Fatal("halt must block sells too")
	}
}

func TestUnknownBookUsesDefault(t *testing.T) {
	p := DefaultPolicy()
	if d := Check(p, Order{Book: "eth_mxn", Side: "buy", QtyBTC: 0.002, Price: 1}, State{}); d.Allowed {
		t.Fatalf("default max_order_btc 0.001 should block, got %+v", d)
	}
}

func TestAssessments(t *testing.T) {
	p := DefaultPolicy()
	if AssessDrawdown(p, "btc_mxn", "paper", 0.10) != nil {
		t.Fatal("10% is below the 25% review level")
	}
	f := AssessDrawdown(p, "btc_mxn", "paper", 0.30)
	if f == nil || f.Severity != Warn {
		t.Fatalf("want warn, got %+v", f)
	}
	if AssessCost(p, "btc_usd", "leg", 50) != nil {
		t.Fatal("50 bps is below 80")
	}
	if c := AssessCost(p, "btc_usd", "leg", 95); c == nil || c.Severity != Warn {
		t.Fatalf("want warn, got %+v", c)
	}
}

func TestLoadPolicyRoundTripAndValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	b, _ := json.MarshalIndent(DefaultPolicy(), "", "  ")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.For("btc_mxn").MaxOrderBTC != 0.01 {
		t.Fatalf("round trip lost limits: %+v", p.For("btc_mxn"))
	}

	if err := os.WriteFile(path, []byte(`{"books":{"x":{"max_order_btc":-1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPolicy(path); err == nil {
		t.Fatal("negative limit must fail validation")
	}
	if err := os.WriteFile(path, []byte(`{"typo_field":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPolicy(path); err == nil {
		t.Fatal("unknown field must fail")
	}
	if p, err := LoadPolicy(""); err != nil || p.Version == "" {
		t.Fatalf("empty path should give defaults, got %+v %v", p, err)
	}
}
