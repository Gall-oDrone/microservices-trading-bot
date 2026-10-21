package etorohealth

import (
	"path/filepath"
	"testing"
	"time"

	"bitso-trading-platform/ui-api/internal/datahealth"
)

// A cell before its first run (no ledger directory at all) is a set of
// warnings, never a failure: ui-alerts watches the ETF and rotation cells
// from the day they are scheduled, before they have run.
func TestMissingLedgerDirectoryIsOnlyWarnings(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-etf")
	checks := Evaluate(Options{LedgerPath: filepath.Join(dir, "ledger.jsonl")}, time.Date(2026, 10, 21, 16, 0, 0, 0, time.UTC))
	if len(checks) == 0 {
		t.Fatal("no checks")
	}
	for _, c := range checks {
		if c.Status == datahealth.Fail {
			t.Fatalf("%s must not fail before the first run: %s", c.ID, c.Message)
		}
	}
}
