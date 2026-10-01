package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bitso-trading-platform/strategy-executor/internal/dailyexec"
)

func TestPlanAction(t *testing.T) {
	cases := []struct {
		target string
		pos    position
		action string
		qty    float64
	}{
		{"long", flatPos(), "buy", 0.001},
		{"long", position{"long", 0.001}, "none", 0},
		{"flat", position{"long", 0.00097}, "sell", 0.00097}, // sells what it holds, not the nominal size
		{"flat", flatPos(), "none", 0},
	}
	for _, c := range cases {
		a, q := planAction(c.target, c.pos, 0.001)
		if a != c.action || q != c.qty {
			t.Fatalf("%s with %+v: got %s %v, want %s %v", c.target, c.pos, a, q, c.action, c.qty)
		}
	}
}

func TestApplyFill(t *testing.T) {
	// First stage run, btc_usd: 0.001 bought, 0.0000025 BTC fee -> 0.0009975 held.
	if p := applyFill(flatPos(), 0.0009975); p.State != "long" || p.BTC != 0.0009975 {
		t.Fatalf("net buy: %+v", p)
	}
	if p := applyFill(position{"long", 0.0009975}, -0.0009975); p.State != "flat" || p.BTC != 0 {
		t.Fatalf("full sell: %+v", p)
	}
	if p := applyFill(position{"long", 0.001}, -0.0004); p.State != "long" || p.BTC < 0.00059999 || p.BTC > 0.00060001 {
		t.Fatalf("partial sell keeps the rest: %+v", p)
	}
}

// Positions come from the executor's own stage records, latest day first,
// and ignore dry-run records and other books.
func TestLastStagePosition(t *testing.T) {
	l, _ := openLedger(filepath.Join(t.TempDir(), "l.jsonl"))
	add := func(book, date, mode string, after position) {
		r := record{Book: book, Mode: mode, Decision: decision{BarDate: date}}
		if mode == "stage" {
			r.Stage = &stageInfo{PositionAfter: after, Leg: &dailyexec.Result{}}
		}
		if err := l.append(r); err != nil {
			t.Fatal(err)
		}
	}
	if p := lastStagePosition(l, "btc_usd"); p != flatPos() {
		t.Fatalf("empty ledger: %+v", p)
	}
	add("btc_usd", "2026-09-30", "stage", position{"long", 0.001})
	add("btc_usd", "2026-10-02", "stage", position{"flat", 0})
	add("btc_usd", "2026-10-01", "stage", position{"long", 0.001})
	add("btc_mxn", "2026-10-03", "stage", position{"long", 0.002})
	add("btc_usd", "2026-10-04", "dry-run", position{})
	if p := lastStagePosition(l, "btc_usd"); p != (position{"flat", 0}) {
		t.Fatalf("want the 2026-10-02 position (flat), got %+v", p)
	}
	if p := lastStagePosition(l, "btc_mxn"); p != (position{"long", 0.002}) {
		t.Fatalf("btc_mxn: %+v", p)
	}
}

func TestLoadEnvFileRefusesLoosePermissionsAndKeepsExistingEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stage.env")
	if err := os.WriteFile(path, []byte("# comment\nDAILY_EXEC_TEST_A=\"from-file\"\nDAILY_EXEC_TEST_B=file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := loadEnvFile(path); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("a world-readable credentials file must be refused, got %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAILY_EXEC_TEST_B", "from-env")
	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("DAILY_EXEC_TEST_A") != "from-file" || os.Getenv("DAILY_EXEC_TEST_B") != "from-env" {
		t.Fatalf("A=%q B=%q", os.Getenv("DAILY_EXEC_TEST_A"), os.Getenv("DAILY_EXEC_TEST_B"))
	}
	os.Unsetenv("DAILY_EXEC_TEST_A")
	if err := loadEnvFile(filepath.Join(dir, "missing.env")); err != nil {
		t.Fatalf("a missing file is not an error: %v", err)
	}
}

func TestLockFileIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.lock")
	a, err := lockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := lockFile(path); err == nil {
		t.Fatal("a second run must not get the lock")
	}
}
