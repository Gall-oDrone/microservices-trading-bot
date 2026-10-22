package etoro

import (
	"strings"
	"testing"
)

// The route table is real-aware since 2026-10-10 but, until the real-account
// canary (docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md, C1), no code path
// ever built a real client. This pins every real path to the portal's
// per-route OpenAPI of 2026-10-10 (trading--real) and asserts that none of
// them carries a demo segment, so a client built with EnvReal can never
// reach a /demo/ route by accident.
func TestRealPathsMatchOpenAPIAndCarryNoDemoSegment(t *testing.T) {
	real, err := NewClient(Config{PublicKey: "pub", PrivateKey: "priv", Env: EnvReal})
	if err != nil {
		t.Fatal(err)
	}
	demo, err := NewClient(Config{PublicKey: "pub", PrivateKey: "priv", Env: EnvDemo})
	if err != nil {
		t.Fatal(err)
	}
	if real.Environment() != EnvReal || demo.Environment() != EnvDemo || real.isDemo() || !demo.isDemo() {
		t.Fatalf("environments: real=%s demo=%s", real.Environment(), demo.Environment())
	}
	cases := []struct {
		name string
		path func(c *Client) string
		real string
		demo string
	}{
		{"orders", func(c *Client) string { return c.ordersPath() },
			"/api/v2/trading/execution/orders", "/api/v2/trading/execution/demo/orders"},
		{"orders:lookup", func(c *Client) string { return c.orderLookupPath() },
			"/api/v2/trading/info/orders:lookup", "/api/v2/trading/info/demo/orders:lookup"},
		{"costs", func(c *Client) string { return c.costsPath() },
			"/api/v2/trading/info/costs", "/api/v2/trading/info/demo/costs"},
		{"eligibility", func(c *Client) string { return c.eligibilityPath() },
			"/api/v2/trading/info/eligibility", "/api/v2/trading/info/demo/eligibility"},
		{"close position", func(c *Client) string { return c.closePositionPath(42) },
			"/api/v1/trading/execution/market-close-orders/positions/42", "/api/v1/trading/execution/demo/market-close-orders/positions/42"},
		{"close order info", func(c *Client) string { return c.closeOrderInfoPath(7) },
			"/api/v1/trading/info/real/close-orders/7", "/api/v1/trading/info/demo/close-orders/7"},
		{"update position", func(c *Client) string { return c.updatePositionPath(42) },
			"/api/v2/trading/positions/42", "/api/v2/trading/demo/positions/42"},
		{"pnl", func(c *Client) string { return c.pnlPath() },
			"/api/v1/trading/info/real/pnl", "/api/v1/trading/info/demo/pnl"},
		{"history", func(c *Client) string { return c.historyPath() },
			"/api/v1/trading/info/trade/history", "/api/v1/trading/info/trade/demo/history"},
	}
	for _, tc := range cases {
		got := tc.path(real)
		if got != tc.real {
			t.Errorf("%s real: got %s want %s", tc.name, got, tc.real)
		}
		if strings.Contains(got, "/demo/") || strings.HasSuffix(got, "/demo") {
			t.Errorf("%s real path %s carries a demo segment", tc.name, got)
		}
		if d := tc.path(demo); d != tc.demo {
			t.Errorf("%s demo: got %s want %s", tc.name, d, tc.demo)
		} else if !strings.Contains(d, "/demo") {
			t.Errorf("%s demo path %s carries no demo segment", tc.name, d)
		}
	}
	// Market-data routes are shared by both environments and never carry one.
	for _, p := range []string{pathSearch, pathRates, pathCandlesFmt, pathHistoryFmt, pathClosingFull} {
		if strings.Contains(p, "demo") || strings.Contains(p, "real") {
			t.Errorf("market-data path %s names an environment", p)
		}
	}
}
