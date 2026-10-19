package risk

import (
	"encoding/json"
	"strings"
	"testing"
)

// Protection rules (2026-10-19): a buy on the protected eToro policy must
// carry a stop within the policy's distance and loss caps; closes and the
// unprotected policy are unchanged.

func protectedBuy(sl, tp float64) Order {
	// 1,100 USD of NSDQ100 at 30,000: 0.036667 units.
	return Order{Book: "nsdq100", Side: "buy", QtyBTC: 1100.0 / 30000, Price: 30000, RefPrice: 30000, Leverage: 1, StopLoss: sl, TakeProfit: tp}
}

func TestProtectedPolicyRequiresStop(t *testing.T) {
	p := EtoroDemoProtectedPolicy()
	if p.Version != "etoro-demo-2026-10-19" || p.Validate() != nil {
		t.Fatalf("policy %+v", p)
	}
	s := State{Equity: 100000, Exposure: 0}
	if d := Check(p, protectedBuy(0, 0), s); d.Allowed || !has(d, RuleStopLossRequired) {
		t.Fatalf("open without a stop must be blocked: %+v", d)
	}
	if d := Check(EtoroDemoPolicy(), protectedBuy(0, 0), s); !d.Allowed {
		t.Fatalf("the unprotected policy is unchanged: %+v", d)
	}
	// 2.1% below with a 4.2% target: allowed.
	if d := Check(p, protectedBuy(29370, 31260), s); !d.Allowed {
		t.Fatalf("a protected open must pass: %+v", d)
	}
	// Stop only, no target: allowed (TP is optional).
	if d := Check(p, protectedBuy(29370, 0), s); !d.Allowed {
		t.Fatalf("stop-only open: %+v", d)
	}
}

func TestProtectionBounds(t *testing.T) {
	p := EtoroDemoProtectedPolicy()
	s := State{Equity: 100000}
	// Too wide: 12% > 10%.
	if d := Check(p, protectedBuy(26400, 0), s); d.Allowed || !has(d, RuleStopLossTooWide) {
		t.Fatalf("12%% stop: %+v", d)
	}
	// Exactly 10% passes.
	if d := Check(p, protectedBuy(27000, 0), s); !d.Allowed {
		t.Fatalf("10%% stop: %+v", d)
	}
	// Too tight when a minimum is set.
	l := p.Books["nsdq100"]
	l.MinStopLossPct = 0.01
	p.Books["nsdq100"] = l
	if d := Check(p, protectedBuy(29850, 0), s); d.Allowed || !has(d, RuleStopLossTooTight) {
		t.Fatalf("0.5%% stop under a 1%% minimum: %+v", d)
	}
	// On the wrong side of the price.
	if d := Check(p, protectedBuy(30100, 0), s); d.Allowed || !has(d, RuleStopLossInvalid) {
		t.Fatalf("stop above a buy: %+v", d)
	}
	if d := Check(p, protectedBuy(29370, 29900), s); d.Allowed || !has(d, RuleTakeProfitInvalid) {
		t.Fatalf("target below a buy: %+v", d)
	}
	// Loss at the stop vs equity: 2.1% of 1,100 is 23 USD; with 2,000 USD of
	// equity that is 1.15% > 0.5%.
	if d := Check(p, protectedBuy(29370, 0), State{Equity: 2000}); d.Allowed || !has(d, RuleMaxLossPctEquity) {
		t.Fatalf("loss cap: %+v", d)
	}
	// No equity known: the equity cap cannot trip.
	if d := Check(p, protectedBuy(29370, 0), State{}); !d.Allowed {
		t.Fatalf("no equity: %+v", d)
	}
}

func TestProtectionNeverTrapsACloseAndIsOmitted(t *testing.T) {
	p := EtoroDemoProtectedPolicy()
	units := 1100.0 / 30000
	close := Order{Book: "nsdq100", Side: "sell", QtyBTC: units, Price: 30000, RefPrice: 30000, Leverage: 1}
	if d := Check(p, close, State{PositionBTC: units, Equity: 100000}); !d.Allowed {
		t.Fatalf("a close needs no stop: %+v", d)
	}
	// The new fields are omitempty on orders without them.
	b, _ := json.Marshal(Order{Book: "btc_mxn", Side: "buy", QtyBTC: 0.001, Price: 1, RefPrice: 1})
	for _, k := range []string{"stop_loss", "take_profit"} {
		if strings.Contains(string(b), `"`+k+`"`) {
			t.Errorf("%s present in a plain order: %s", k, b)
		}
	}
	lb, _ := json.Marshal(EtoroDemoPolicy().For("nsdq100"))
	for _, k := range []string{"require_stop_loss", "min_stop_loss_pct", "max_stop_loss_pct", "max_loss_pct_equity"} {
		if strings.Contains(string(lb), `"`+k+`"`) {
			t.Errorf("%s present in the unprotected policy JSON: %s", k, lb)
		}
	}
	// Validation of the new limits.
	bad := EtoroDemoProtectedPolicy()
	l := bad.Books["nsdq100"]
	l.MinStopLossPct, l.MaxStopLossPct = 0.2, 0.1
	bad.Books["nsdq100"] = l
	if bad.Validate() == nil {
		t.Fatal("min above max accepted")
	}
	l.MinStopLossPct, l.MaxStopLossPct, l.MaxLossPctEquity = 0, 0.1, 1
	bad.Books["nsdq100"] = l
	if bad.Validate() == nil {
		t.Fatal("max_loss_pct_equity 1 accepted")
	}
}
