package risk

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bitso-trading-platform/order-management/internal/models"
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
	sp, err := LoadSharedPolicy(envMap(nil))
	if err != nil || sp != nil {
		t.Fatalf("unset: want nil gate, got %+v %v", sp, err)
	}
	sp, err = LoadSharedPolicy(envMap(map[string]string{EnvHaltFiles: " /a/risk-state.json, "}))
	if err != nil || sp == nil || sp.Policy != nil || len(sp.HaltFiles) != 1 {
		t.Fatalf("halt only: %+v %v", sp, err)
	}
	bad := writeSharedFile(t, "policy.json", `{"version":"x","nope":1}`)
	if _, err := LoadSharedPolicy(envMap(map[string]string{EnvSharedPolicy: bad})); err == nil {
		t.Fatal("unknown policy field accepted")
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
	sp, err := LoadSharedPolicy(envMap(map[string]string{EnvSharedPolicy: path}))
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
