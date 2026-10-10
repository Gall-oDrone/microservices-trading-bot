package risk

import (
	"encoding/json"
	"strings"
	"testing"
)

// A 1,100 USD x1 open on NSDQ100 at 30,900 with the demo account's equity.
func cfdOpen() (Order, State) {
	return Order{Book: "nsdq100", Side: "buy", QtyBTC: 1100.0 / 30900, Price: 30900, RefPrice: 30893.3, Leverage: 1},
		State{Equity: 99_993.93}
}

func TestEtoroDemoPolicyAllowsForwardTestLeg(t *testing.T) {
	p := EtoroDemoPolicy()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	o, s := cfdOpen()
	if d := Check(p, o, s); !d.Allowed || len(d.Findings) != 0 {
		t.Fatalf("want allowed, got %+v", d)
	}
	// The second instrument with the first already open: 2.2 % of equity.
	o.Book = "spx500"
	s.Exposure = 1100
	if d := Check(p, o, s); !d.Allowed {
		t.Fatalf("second leg: %+v", d)
	}
}

func TestEtoroDemoPolicyBlocks(t *testing.T) {
	p := EtoroDemoPolicy()
	cases := []struct {
		name string
		mod  func(*Order, *State)
		rule string
	}{
		{"leverage x2", func(o *Order, s *State) { o.Leverage = 2 }, RuleMaxLeverage},
		{"exposure above 5% of equity", func(o *Order, s *State) { s.Exposure = 4000 }, RuleMaxExposurePctEquity},
		{"small account", func(o *Order, s *State) { s.Equity = 10_000 }, RuleMaxExposurePctEquity},
		{"market closed", func(o *Order, s *State) { s.MarketClosed = true }, RuleMarketClosed},
		{"notional", func(o *Order, s *State) { o.QtyBTC = 2000.0 / 30900 }, RuleMaxOrderNotional},
		{"second leg today", func(o *Order, s *State) { s.OrdersToday = 1 }, RuleMaxOrdersPerDay},
		{"unknown instrument", func(o *Order, s *State) { o.Book = "ger40" }, RuleMaxOrderNotional},
		{"fat finger", func(o *Order, s *State) { o.RefPrice = 25000 }, RulePriceDeviation},
	}
	for _, c := range cases {
		o, s := cfdOpen()
		c.mod(&o, &s)
		d := Check(p, o, s)
		if d.Allowed || !has(d, c.rule) {
			t.Errorf("%s: want blocked by %s, got %+v", c.name, c.rule, d)
		}
	}
}

func TestEtoroCloseIgnoresSizeLimitsButNotMarketOrHalt(t *testing.T) {
	p := EtoroDemoPolicy()
	units := 1100.0 / 30900
	close := Order{Book: "nsdq100", Side: "sell", QtyBTC: units, Price: 30800, RefPrice: 30893.3, Leverage: 1}
	// Over the exposure cap and the order count: a close only reduces.
	s := State{PositionBTC: units, Equity: 1000, Exposure: 5000, OrdersToday: 3}
	if d := Check(p, close, s); !d.Allowed {
		t.Fatalf("close must not be trapped by size limits: %+v", d)
	}
	s.MarketClosed = true
	if d := Check(p, close, s); d.Allowed || !has(d, RuleMarketClosed) {
		t.Fatalf("close with the market closed: %+v", d)
	}
	h := p
	h.Halted, h.HaltReason = true, "drill"
	if d := Check(h, close, State{PositionBTC: units}); d.Allowed || !has(d, RuleHalted) {
		t.Fatalf("close while halted: %+v", d)
	}
}

// The new fields are omitempty: a Bitso order and state marshal exactly as
// before, so existing ledger lines and their golden tests are unchanged.
func TestNewFieldsOmittedForBitso(t *testing.T) {
	b, _ := json.Marshal(struct {
		O Order
		S State
		L BookLimits
	}{Order{Book: "btc_mxn", Side: "buy", QtyBTC: 0.001, Price: 1, RefPrice: 1}, State{}, DefaultPolicy().For("btc_mxn")})
	for _, k := range []string{"leverage", "equity", "exposure", "market_closed", "max_leverage", "max_exposure_pct_equity", "require_market_open"} {
		if strings.Contains(string(b), `"`+k+`"`) {
			t.Errorf("%s present in Bitso JSON: %s", k, b)
		}
	}
	bad := EtoroDemoPolicy()
	l := bad.Books["nsdq100"]
	l.MaxExposurePctEquity = -1
	bad.Books["nsdq100"] = l
	if bad.Validate() == nil {
		t.Fatal("negative exposure cap accepted")
	}
}
