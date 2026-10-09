package risk

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	sharedrisk "bitso-trading-platform/shared/pkg/risk"
)

// The stage figures in k8s/overlays/development/order-management-risk-capital.yaml
// must parse the way start-up parses them, keep capital equal to the
// policy's max order notional per book, and keep each limit below capital
// with the policy's full position inside it (plan §6.4.10).
func TestStageRiskCapitalOverlay(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "k8s", "overlays", "development", "order-management-risk-capital.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	re := regexp.MustCompile(`(?m)- name: (RISK_[A-Z_]+)\n\s+value: "([^"]*)"`)
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		env[m[1]] = m[2]
	}
	for _, k := range []string{EnvCapital, EnvVaRLimits, EnvStressLimits} {
		if env[k] == "" {
			t.Fatalf("%s missing from %s", k, path)
		}
	}
	c, err := LoadPortfolioConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	pol := sharedrisk.DefaultPolicy()
	for book, ccy := range map[string]string{"btc_mxn": "MXN", "btc_usd": "USD"} {
		l := pol.For(book)
		capital := c.Capital[ccy]
		if capital != l.MaxOrderNotional {
			t.Errorf("%s capital %v, policy max order notional %v", ccy, capital, l.MaxOrderNotional)
		}
		if v, s := c.VaRLimits[ccy], c.StressLimits[ccy]; !(v > 0 && v < s && s < capital) {
			t.Errorf("%s: want 0 < VaR limit %v < stress limit %v < capital %v", ccy, v, s, capital)
		}
		// Capital bounds exposure at entry only if a full position fits in
		// one order: then its notional is capped by max_order_notional.
		if l.MaxPositionBTC > l.MaxOrderBTC {
			t.Errorf("%s: max position %v above max order %v; capital no longer bounds exposure", book, l.MaxPositionBTC, l.MaxOrderBTC)
		}
	}
}
