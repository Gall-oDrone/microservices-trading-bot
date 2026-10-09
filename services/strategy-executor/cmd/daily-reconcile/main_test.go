package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/strategy-executor/internal/bitsostage"
	"bitso-trading-platform/strategy-executor/internal/reconcile"
)

type source struct{ trades map[string][]bitsostage.Trade }

func (s source) Balances() (map[string]float64, error) { return map[string]float64{"btc": 1}, nil }
func (s source) TradesByOrigin(o string) ([]bitsostage.Trade, error) {
	return s.trades[o], nil
}

func fixture(t *testing.T) []dailyledger.Record {
	recs, err := dailyledger.ReadFile(filepath.Join("..", "..", "..", "ui-api", "internal", "api", "testdata", "ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

var now = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func TestReportExitCodesAndJSON(t *testing.T) {
	ok := map[string][]bitsostage.Trade{
		"sma50-btc_usd-20260930-b-m": {{Oid: "mykZ1geAhtLuJn5X", Major: 0.001, Minor: 83.481, Fee: 2.5e-06, FeeCurrency: "btc"}},
		"sma50-btc_mxn-20260930-b-m": {{Oid: "XzYf4h0PZsSuECcU", Major: 1.2e-06, Minor: 1.81, FeeCurrency: "btc"}},
		"sma50-btc_mxn-20260930-b-t": {{Oid: "zuLFQFpzkPv9hKxr", Major: 0.00100665, Minor: 1521.04979, Fee: 7.86e-06, FeeCurrency: "btc"}},
	}
	out := filepath.Join(t.TempDir(), "reconcile.json")
	var stdout, stderr bytes.Buffer
	if code := report(source{ok}, fixture(t), now, out, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "reconcile: 2 legs, 0 leg-less days checked, 0 breaks") {
		t.Fatalf("stdout %s", stdout.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var rep reconcile.Report
	if err := json.Unmarshal(b, &rep); err != nil || !rep.OK || len(rep.Legs) != 2 {
		t.Fatalf("json %v %s", err, b)
	}

	// A missing fill is a break: exit 3, and the diff is printed.
	delete(ok, "sma50-btc_mxn-20260930-b-t")
	stdout.Reset()
	if code := report(source{ok}, fixture(t), now, "", &stdout, &stderr); code != 3 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout.String(), "btc_mxn  2026-09-30 buy  mismatch") || !strings.Contains(stdout.String(), "filled: ledger 0.00100785, Bitso 0.00000120") {
		t.Fatalf("stdout %s", stdout.String())
	}
}

// Without credentials the command stops before any request.
func TestRunNeedsCredentials(t *testing.T) {
	t.Setenv("STAGE_BITSO_API_KEY", "")
	t.Setenv("STAGE_BITSO_API_SECRET", "")
	t.Setenv("STAGE_BITSO_APISECRET", "")
	var stdout, stderr bytes.Buffer
	ledger := filepath.Join("..", "..", "..", "ui-api", "internal", "api", "testdata", "ledger.jsonl")
	if code := run(ledger, "", "", &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "API key and secret are required") {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
}
