package risk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The canary's day-0 order (plan §8.2): 50 USD invested at x20 on NSDQ100
// at 25,000 = 1,000 USD of exposure, 0.04 units, the stop 2.4 % below
// (24 USD at risk on a 60 USD account), the account holding nothing.
func canaryOpen() (Order, State) {
	return Order{Book: "nsdq100", Side: "buy", QtyBTC: 50 * 20 / 25000.0, Price: 25000, RefPrice: 24980, Leverage: 20, StopLoss: 25000 * (1 - 0.024)},
		State{Equity: 60, Exposure: 0, OpenPositions: 0}
}

func TestEtoroRealCanaryPolicyAllowsTheProbe(t *testing.T) {
	p := EtoroRealCanaryPolicy()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.Version != EtoroRealCanaryVersion || !strings.HasPrefix(p.Version, "etoro-real-canary-") {
		t.Fatalf("version %q", p.Version)
	}
	o, s := canaryOpen()
	if d := Check(p, o, s); !d.Allowed || len(d.Findings) != 0 {
		t.Fatalf("want allowed, got %+v", d)
	}
	// The budget's top: 60 USD at x20 is 1,200 USD of exposure, exactly the
	// notional cap (units recomputed through the price must not trip it).
	o.QtyBTC = 60 * 20 / 25000.0
	if d := Check(p, o, s); !d.Allowed {
		t.Fatalf("60 USD at x20: %+v", d)
	}
	// A 24.5 % max loss on 50 USD is 2.45 % of the price: the upper stop bound.
	o, s = canaryOpen()
	o.StopLoss = 25000 * (1 - 0.0245)
	if d := Check(p, o, s); !d.Allowed {
		t.Fatalf("stop at the policy's maximum distance: %+v", d)
	}
	if l := p.For("nsdq100"); l.MinInvestedUSD != 50 || l.MaxInvestedUSD != 60 || l.DefaultInvestedUSD != 50 || len(l.Leverages) != 1 || l.Leverages[0] != 20 || !l.AllowsLeverage(20) || l.AllowsLeverage(1) {
		t.Fatalf("sizing %+v", l)
	}
	if d := p.For("spx500"); d.MaxOrderNotional != 1 || !d.RequireStopLoss {
		t.Fatalf("anything but nsdq100 must be refused outright: %+v", d)
	}
}

func TestEtoroRealCanaryPolicyBlocks(t *testing.T) {
	p := EtoroRealCanaryPolicy()
	cases := []struct {
		name string
		mod  func(*Order, *State)
		rule string
	}{
		{"x1 (not in the leverage set)", func(o *Order, s *State) { o.Leverage = 1 }, RuleLeverageNotAllowed},
		{"x10 (not in the leverage set)", func(o *Order, s *State) { o.Leverage = 10 }, RuleLeverageNotAllowed},
		{"x30 (over the max)", func(o *Order, s *State) { o.Leverage = 30 }, RuleMaxLeverage},
		{"61 USD invested", func(o *Order, s *State) { o.QtyBTC = 61 * 20 / 25000.0 }, RuleMaxInvested},
		{"61 USD invested is also over the notional cap", func(o *Order, s *State) { o.QtyBTC = 61 * 20 / 25000.0 }, RuleMaxOrderNotional},
		{"the account already holds a position", func(o *Order, s *State) { s.OpenPositions = 1 }, RuleMaxPositions},
		{"second order today", func(o *Order, s *State) { s.OrdersToday = 1 }, RuleMaxOrdersPerDay},
		{"no stop", func(o *Order, s *State) { o.StopLoss = 0 }, RuleStopLossRequired},
		{"stop tighter than 2.3 %", func(o *Order, s *State) { o.StopLoss = 25000 * (1 - 0.02) }, RuleStopLossTooTight},
		{"stop wider than 2.45 % (past the venue's 50 % of invested)", func(o *Order, s *State) { o.StopLoss = 25000 * (1 - 0.025) }, RuleStopLossTooWide},
		{"market closed", func(o *Order, s *State) { s.MarketClosed = true }, RuleMarketClosed},
		{"fat finger", func(o *Order, s *State) { o.RefPrice = 20000 }, RulePriceDeviation},
		{"spx500 is not a canary book", func(o *Order, s *State) { o.Book = "spx500" }, RuleMaxOrderNotional},
	}
	for _, c := range cases {
		o, s := canaryOpen()
		c.mod(&o, &s)
		d := Check(p, o, s)
		if d.Allowed || !has(d, c.rule) {
			t.Errorf("%s: want blocked by %s, got %+v", c.name, c.rule, d)
		}
	}
	// Equity caps are off: a 24 USD loss on a 60 USD account is the design.
	o, s := canaryOpen()
	s.Equity = 55
	if d := Check(p, o, s); !d.Allowed {
		t.Fatalf("equity caps must be off for the canary: %+v", d)
	}
	// A close is never trapped: over the position cap, the order count, x20.
	units := 50 * 20 / 25000.0
	close := Order{Book: "nsdq100", Side: "sell", QtyBTC: units, Price: 24500, RefPrice: 25000, Leverage: 20}
	if d := Check(p, close, State{PositionBTC: units, OpenPositions: 1, OrdersToday: 1, Equity: 36}); !d.Allowed {
		t.Fatalf("close trapped: %+v", d)
	}
}

func TestEtoroRealCanaryRehearsalPolicy(t *testing.T) {
	p := EtoroRealCanaryRehearsalPolicy()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(p.Version, "etoro-real-canary-") {
		t.Fatalf("the rehearsal version %q must not pass as the real canary's", p.Version)
	}
	// Same per-order limits, on both indices, without the account-wide cap:
	// the demo account holds the cells' positions.
	for _, book := range []string{"nsdq100", "spx500"} {
		o, s := canaryOpen()
		o.Book, s.OpenPositions, s.Equity = book, 3, 100_000
		if d := Check(p, o, s); !d.Allowed {
			t.Fatalf("%s rehearsal open: %+v", book, d)
		}
		o.Leverage = 1
		if d := Check(p, o, s); d.Allowed || !has(d, RuleLeverageNotAllowed) {
			t.Fatalf("%s rehearsal x1: %+v", book, d)
		}
	}
	// The real policy is untouched by building the rehearsal one.
	if r := EtoroRealCanaryPolicy(); r.For("nsdq100").MaxPositions != 1 || len(r.Books) != 1 {
		t.Fatalf("real canary policy changed: %+v", r)
	}
}

// The canary policy as JSON is frozen in testdata so a change to its values
// is a visible diff (the acknowledgement names the version; the numbers are
// the plan's §6 as built).
func TestEtoroRealCanaryPolicyGolden(t *testing.T) {
	got, err := json.MarshalIndent(EtoroRealCanaryPolicy(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "etoro-real-canary-policy.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with UPDATE_GOLDEN=1 to write it)", err)
	}
	if string(want) != string(got) {
		t.Fatalf("golden %s differs from EtoroRealCanaryPolicy():\n%s", path, got)
	}
	// Round trip through LoadPolicy's strict decoder.
	var back Policy
	dec := json.NewDecoder(strings.NewReader(string(got)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&back); err != nil {
		t.Fatal(err)
	}
	if err := back.Validate(); err != nil {
		t.Fatal(err)
	}
}

// The demo policies' JSON is unchanged by the new fields (all omitempty).
func TestCanaryFieldsOmittedForDemoPolicies(t *testing.T) {
	for _, p := range []Policy{EtoroDemoPolicy(), EtoroDemoProtectedPolicy(), EtoroDemoETFPolicy(), DefaultPolicy()} {
		b, _ := json.Marshal(p)
		for _, k := range []string{"max_invested_usd", "max_positions", "leverages", "min_invested_usd", "default_invested_usd", "open_positions"} {
			if strings.Contains(string(b), `"`+k+`"`) {
				t.Errorf("%s: %s present in %s", p.Version, k, b)
			}
		}
	}
	b, _ := json.Marshal(State{Equity: 1})
	if strings.Contains(string(b), "open_positions") {
		t.Fatalf("open_positions must be omitempty: %s", b)
	}
}

func TestCanaryFieldsValidation(t *testing.T) {
	bad := func(mod func(*BookLimits)) error {
		p := EtoroRealCanaryPolicy()
		l := p.Books["nsdq100"]
		mod(&l)
		p.Books["nsdq100"] = l
		return p.Validate()
	}
	cases := map[string]func(*BookLimits){
		"negative invested":          func(l *BookLimits) { l.MaxInvestedUSD = -1 },
		"negative positions":         func(l *BookLimits) { l.MaxPositions = -1 },
		"min over max invested":      func(l *BookLimits) { l.MinInvestedUSD = 70 },
		"default outside the bounds": func(l *BookLimits) { l.DefaultInvestedUSD = 10 },
		"leverage zero":              func(l *BookLimits) { l.Leverages = []int{0} },
		"leverage over the max":      func(l *BookLimits) { l.Leverages = []int{50} },
	}
	for name, mod := range cases {
		if err := bad(mod); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
