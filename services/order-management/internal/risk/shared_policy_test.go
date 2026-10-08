package risk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/repository"
)

func writeSharedFile(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func envMap(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadSharedPolicy(t *testing.T) {
	cfg := setupRiskManager().config
	sp, err := LoadSharedPolicy(envMap(nil), cfg)
	if err != nil || sp == nil || sp.Source != "env" || sp.Policy == nil || sp.Policy.Version != EnvPolicyVersion {
		t.Fatalf("unset: want env-built policy, got %+v %v", sp, err)
	}
	if d := sp.Policy.Default; d.MaxPositionBTC != cfg.MaxPositionSize || d.MaxOrderNotional != cfg.MaxOrderValue {
		t.Fatalf("env default limits wrong: %+v", d)
	}
	if pf := sp.Policy.Portfolio; pf == nil || pf.MaxOpenOrders != cfg.MaxOpenOrders || pf.MaxOrdersPerMinute != cfg.MaxOrdersPerMinute {
		t.Fatalf("env portfolio limits wrong: %+v", pf)
	}
	sp, err = LoadSharedPolicy(envMap(map[string]string{EnvHaltFiles: " /a/risk-state.json, "}), cfg)
	if err != nil || sp == nil || sp.Policy == nil || len(sp.HaltFiles) != 1 {
		t.Fatalf("halt only: %+v %v", sp, err)
	}
	bad := writeSharedFile(t, "policy.json", `{"version":"x","nope":1}`)
	if _, err := LoadSharedPolicy(envMap(map[string]string{EnvSharedPolicy: bad}), cfg); err == nil {
		t.Fatal("unknown policy field accepted")
	}

	// A file without a portfolio section keeps the env runaway guards.
	noPF := writeSharedFile(t, "policy.json", `{"version":"te","books":{},"default":{"max_order_btc":0.01}}`)
	sp, err = LoadSharedPolicy(envMap(map[string]string{EnvSharedPolicy: noPF}), cfg)
	if err != nil || !sp.PortfolioFromEnv || sp.Policy.Portfolio == nil || sp.Policy.Portfolio.MaxOrdersPerMinute != 60 {
		t.Fatalf("portfolio fallback: %+v %v", sp, err)
	}
	if sp.Policy.Default.MaxPositionBTC != 0 {
		t.Fatalf("file is the source of truth: env MAX_POSITION_SIZE leaked in: %+v", sp.Policy.Default)
	}
	// A file with a portfolio section wins, even where it disables a limit.
	withPF := writeSharedFile(t, "policy.json", `{"version":"om","books":{},"default":{},"portfolio":{"max_open_orders":3}}`)
	sp, err = LoadSharedPolicy(envMap(map[string]string{EnvSharedPolicy: withPF}), cfg)
	if err != nil || sp.PortfolioFromEnv || sp.Policy.Portfolio.MaxOpenOrders != 3 || sp.Policy.Portfolio.MaxOrdersPerMinute != 0 {
		t.Fatalf("file portfolio: %+v %v", sp.Policy.Portfolio, err)
	}
}

func TestPerBookOpenOrderLimit(t *testing.T) {
	ctx := context.Background()
	log := testRiskLogger
	orders := repository.NewInMemoryOrderRepository(log, testRiskMetrics)
	m := NewRiskManager(setupRiskManager().config, log, orders, repository.NewInMemoryPositionRepository(log, testRiskMetrics), testRiskMetrics)
	path := writeSharedFile(t, "policy.json",
		`{"version":"om-books","books":{"btc_mxn":{"max_open_orders":2}},"default":{},"portfolio":{"max_open_orders":5}}`)
	sp, err := LoadSharedPolicy(envMap(map[string]string{EnvSharedPolicy: path}), m.config)
	if err != nil {
		t.Fatal(err)
	}
	m.SetSharedPolicy(sp)
	rest := func(book string) {
		o := models.NewOrder("rest-"+book+fmt.Sprint(time.Now().UnixNano()), book, "buy", "limit", "basic", 1, 0.001)
		o.UpdateStatus(models.OrderStatusAccepted)
		if err := orders.Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	rest("btc_mxn")
	rest("btc_mxn")
	rest("btc_usd")

	next := models.NewOrder("next-mxn", "btc_mxn", "buy", "limit", "basic", 1, 0.001)
	if err := m.CheckRisk(ctx, next); err == nil || !strings.Contains(err.Error(), "shared_policy:max_open_orders") {
		t.Fatalf("btc_mxn third open order: want max_open_orders, got %v", err)
	}
	if err := m.CheckRisk(ctx, models.NewOrder("next-usd", "btc_usd", "buy", "limit", "basic", 1, 0.001)); err != nil {
		t.Fatalf("btc_usd is under its (default, unlimited) book limit: %v", err)
	}
	rest("btc_usd")
	rest("eth_mxn")
	if err := m.CheckRisk(ctx, models.NewOrder("next-eth", "eth_mxn", "buy", "limit", "basic", 1, 0.001)); err == nil ||
		!strings.Contains(err.Error(), "portfolio_max_open_orders") {
		t.Fatalf("sixth open order firm-wide: want portfolio_max_open_orders, got %v", err)
	}
}

func TestCheckRiskSharedHalt(t *testing.T) {
	ctx := context.Background()
	m := setupRiskManager()
	halt := writeSharedFile(t, "risk-state.json",
		`{"halted":true,"reason":"HALT ALL drill","by":"diego","at":"2026-10-08T18:00:00Z"}`)
	m.SetSharedPolicy(&SharedPolicy{HaltFiles: []string{halt}})

	order := models.NewOrder("signal-h", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
	err := m.CheckRisk(ctx, order)
	if err == nil || !strings.Contains(err.Error(), "HALT ALL drill") {
		t.Fatalf("halted OM accepted an order: %v", err)
	}

	m.SetSharedPolicy(&SharedPolicy{HaltFiles: []string{writeSharedFile(t, "risk-state.json", `{"halted":false}`)}})
	if err := m.CheckRisk(ctx, order); err != nil {
		t.Fatalf("cleared halt still blocks: %v", err)
	}

	m.SetSharedPolicy(&SharedPolicy{HaltFiles: []string{writeSharedFile(t, "risk-state.json", `{"halted":true}`)}})
	if err := m.CheckRisk(ctx, order); err == nil {
		t.Fatal("invalid halt file did not fail closed")
	}
}

func TestCheckRiskSharedPolicyLimits(t *testing.T) {
	ctx := context.Background()
	m := setupRiskManager()
	path := writeSharedFile(t, "policy.json",
		`{"version":"om-test","books":{"btc_mxn":{"max_order_btc":0.005,"max_order_notional":5000}},"default":{}}`)
	sp, err := LoadSharedPolicy(envMap(map[string]string{EnvSharedPolicy: path}), m.config)
	if err != nil {
		t.Fatal(err)
	}
	m.SetSharedPolicy(sp)

	small := models.NewOrder("signal-s", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.002)
	if err := m.CheckRisk(ctx, small); err != nil {
		t.Fatalf("small order rejected: %v", err)
	}
	big := models.NewOrder("signal-b", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
	err = m.CheckRisk(ctx, big)
	if err == nil || !strings.Contains(err.Error(), "shared_policy:max_order_btc") ||
		!strings.Contains(err.Error(), "om-test") {
		t.Fatalf("want shared max_order_btc rejection, got %v", err)
	}

	// A sell that only reduces the held position is not trapped by the size cap.
	pos := models.NewPosition("btc_mxn", "long")
	pos.Size = 0.01
	_ = m.positionRepo.Create(ctx, pos)
	defer m.positionRepo.Delete(ctx, "btc_mxn")
	sell := models.NewOrder("signal-r", "btc_mxn", "sell", "limit", "basic", 500000.0, 0.01)
	if err := m.CheckRisk(ctx, sell); err != nil {
		t.Fatalf("reducing sell rejected: %v", err)
	}
}
