package alerts

import (
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/ui-api/internal/datahealth"
	"bitso-trading-platform/ui-api/internal/etorohealth"
)

func TestFromEtoro(t *testing.T) {
	checks := []etorohealth.Check{
		{ID: etorohealth.IDLedger, Label: "eToro ledger", Status: datahealth.OK, Message: "6 records"},
		{ID: etorohealth.IDCoverage, Label: "eToro ledger coverage", Status: datahealth.Fail, Message: "no ledger line for spx500 2026-10-15"},
		{ID: etorohealth.IDReconcile, Label: "eToro reconcile", Status: datahealth.Warn, Message: "2026-10-15: clean with warnings: other_instruments"},
		{ID: etorohealth.IDHalt, Label: "eToro trading halted", Status: datahealth.Warn, Message: "drill (by diego at 2026-10-15T12:00:00Z, halt file)"},
	}
	got := FromEtoro("etoro-demo", checks)
	if len(got) != 3 {
		t.Fatalf("want 3 alerts (ok is not one), got %+v", got)
	}
	byKey := map[string]Alert{}
	for _, a := range got {
		byKey[a.Key] = a
	}
	if a := byKey["etoro-demo/etoro.coverage"]; a.Severity != Critical || a.Title != "eToro ledger coverage (etoro-demo)" {
		t.Errorf("coverage: %+v", a)
	}
	if a := byKey["etoro-demo/etoro.reconcile"]; a.Severity != Warning {
		t.Errorf("reconcile: %+v", a)
	}
	// The halt keys like FromRisk's, so the subject names every halted ledger.
	if _, ok := byKey["etoro-demo/risk.halted"]; !ok {
		t.Fatalf("halt key: %+v", got)
	}
	all := Merge(got, []Alert{{Key: "stage/risk.halted", Severity: Warning, Title: "Trading halted (stage)", Message: "kill switch"}})
	n, _ := Diff(State{}, all, time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC), 0)
	subject, body := Render(n, "")
	if !strings.Contains(subject, "Trading halted (etoro-demo, stage)") {
		t.Errorf("subject: %q", subject)
	}
	if !strings.Contains(body, "Halted ledgers now: etoro-demo, stage") || !strings.Contains(body, "[CRITICAL] eToro ledger coverage (etoro-demo)") {
		t.Errorf("body:\n%s", body)
	}
	// A corrupt halt file is critical and keeps its own key: it is not a halt.
	bad := FromEtoro("etoro-demo", []etorohealth.Check{{ID: etorohealth.IDHalt, Label: "eToro halt file", Status: datahealth.Fail, Message: "halted needs reason"}})
	if len(bad) != 1 || bad[0].Key != "etoro-demo/etoro.halted" || bad[0].Severity != Critical {
		t.Errorf("corrupt halt: %+v", bad)
	}
}
