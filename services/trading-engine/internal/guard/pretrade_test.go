package guard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bitso-trading-platform/shared/pkg/risk"
)

func writeFile(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func buy(qty, price float64) risk.Order {
	return risk.Order{Book: "btc_mxn", Side: "buy", QtyBTC: qty, Price: price, RefPrice: price}
}

func TestDefaultPolicyIsValid(t *testing.T) {
	if err := DefaultPolicy().Validate(); err != nil {
		t.Fatalf("default policy invalid: %v", err)
	}
}

func TestPreTradeAllowsSmallOrder(t *testing.T) {
	g := &PreTrade{Policy: DefaultPolicy()}
	if _, err := g.Allow(context.Background(), buy(0.001, 1_000_000)); err != nil {
		t.Fatalf("small order blocked: %v", err)
	}
}

func TestPreTradeBlocksOversizedOrderAndNotional(t *testing.T) {
	g := &PreTrade{Policy: DefaultPolicy()}
	_, err := g.Allow(context.Background(), buy(0.5, 1_000_000))
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("want ErrBlocked, got %v", err)
	}
	for _, rule := range []string{risk.RuleMaxOrderBTC, risk.RuleMaxOrderNotional} {
		if !strings.Contains(err.Error(), rule) {
			t.Errorf("error %q does not name %s", err, rule)
		}
	}
	// 0.05 BTC at 1,000,000 MXN is 50,000 MXN: under the size cap, over notional.
	if _, err := g.Allow(context.Background(), buy(0.05, 1_000_000)); err == nil ||
		!strings.Contains(err.Error(), risk.RuleMaxOrderNotional) {
		t.Fatalf("want notional block, got %v", err)
	}
}

func TestPreTradeBlocksFatFinger(t *testing.T) {
	g := &PreTrade{Policy: DefaultPolicy()}
	o := buy(0.001, 1_100_000)
	o.RefPrice = 1_000_000 // 1000 bps away; limit 500
	if _, err := g.Allow(context.Background(), o); err == nil ||
		!strings.Contains(err.Error(), risk.RulePriceDeviation) {
		t.Fatalf("want price deviation block, got %v", err)
	}
}

func TestPreTradeHaltFile(t *testing.T) {
	halted := writeFile(t, "risk-state.json",
		`{"halted":true,"reason":"kill switch drill","by":"diego","at":"2026-10-08T18:00:00Z"}`)
	clear := writeFile(t, "risk-state.json", `{"halted":false}`)
	missing := filepath.Join(t.TempDir(), "risk-state.json")

	g := &PreTrade{Policy: DefaultPolicy(), HaltFiles: []string{missing, clear, halted}}
	_, err := g.Allow(context.Background(), buy(0.001, 1_000_000))
	if !errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), "kill switch drill") {
		t.Fatalf("want halt with reason, got %v", err)
	}
	// A halt also stops a sell that would reduce the position.
	g.Position = func(context.Context, string) (float64, error) { return 1, nil }
	sell := buy(0.5, 1_000_000)
	sell.Side = "sell"
	if _, err := g.Allow(context.Background(), sell); !errors.Is(err, ErrBlocked) {
		t.Fatalf("halted engine sent a sell: %v", err)
	}

	g = &PreTrade{Policy: DefaultPolicy(), HaltFiles: []string{missing, clear}}
	if _, err := g.Allow(context.Background(), buy(0.001, 1_000_000)); err != nil {
		t.Fatalf("missing / clear halt files blocked: %v", err)
	}
}

func TestPreTradeInvalidHaltFileFailsClosed(t *testing.T) {
	for name, body := range map[string]string{
		"garbage":       `{not json`,
		"unexplained":   `{"halted":true}`,
		"unknown field": `{"halted":false,"oops":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			g := &PreTrade{Policy: DefaultPolicy(), HaltFiles: []string{writeFile(t, "risk-state.json", body)}}
			d, err := g.Allow(context.Background(), buy(0.001, 1_000_000))
			if !errors.Is(err, ErrBlocked) {
				t.Fatalf("want ErrBlocked, got %v", err)
			}
			if len(d.Findings) != 1 || d.Findings[0].Rule != risk.RuleHalted {
				t.Fatalf("want one halted finding, got %+v", d.Findings)
			}
		})
	}
}

func TestPreTradeReducingSellNotTrapped(t *testing.T) {
	g := &PreTrade{Policy: DefaultPolicy(),
		Position: func(context.Context, string) (float64, error) { return 0.5, nil }}
	sell := buy(0.5, 1_000_000)
	sell.Side = "sell"
	if _, err := g.Allow(context.Background(), sell); err != nil {
		t.Fatalf("reducing sell blocked: %v", err)
	}
	// Without the position the same sell is an oversized short.
	g.Position = func(context.Context, string) (float64, error) { return 0, nil }
	if _, err := g.Allow(context.Background(), sell); !errors.Is(err, ErrBlocked) {
		t.Fatalf("opening 0.5 BTC short allowed: %v", err)
	}
}

func TestPreTradeUnknownPositionFailsClosed(t *testing.T) {
	g := &PreTrade{Policy: DefaultPolicy(),
		Position: func(context.Context, string) (float64, error) { return 0, errors.New("connection refused") }}
	_, err := g.Allow(context.Background(), buy(0.001, 1_000_000))
	if !errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("want fail-closed block, got %v", err)
	}
}

func TestLoadPolicyFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	p, src, err := LoadPolicy(env(nil))
	if err != nil || src != "built-in" || p.Version != DefaultPolicy().Version {
		t.Fatalf("unset: %v %q %q", err, src, p.Version)
	}

	path := writeFile(t, "policy.json",
		`{"version":"stage-test","books":{"btc_mxn":{"max_order_btc":0.002}},"default":{"max_order_btc":0.001}}`)
	p, src, err = LoadPolicy(env(map[string]string{EnvRiskPolicy: path}))
	if err != nil || src != path || p.Version != "stage-test" || p.For("btc_mxn").MaxOrderBTC != 0.002 {
		t.Fatalf("file: %v %q %+v", err, src, p)
	}

	bad := writeFile(t, "policy.json", `{"version":"x","max_order":1}`)
	if _, _, err := LoadPolicy(env(map[string]string{EnvRiskPolicy: bad})); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, _, err := LoadPolicy(env(map[string]string{EnvRiskPolicy: "/nonexistent/policy.json"})); err == nil {
		t.Fatal("missing policy file accepted")
	}
}

func TestHaltFilesParsesList(t *testing.T) {
	got := HaltFiles(func(string) string { return " /a/risk-state.json, ,/b/risk-state.json," })
	if len(got) != 2 || got[0] != "/a/risk-state.json" || got[1] != "/b/risk-state.json" {
		t.Fatalf("got %q", got)
	}
	if HaltFiles(func(string) string { return "" }) != nil {
		t.Fatal("empty env should give no files")
	}
}
