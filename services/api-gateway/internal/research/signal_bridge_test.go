package research

import "testing"

func TestDecisionToSignal(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"BUY", "BUY"},
		{"Strong SELL recommendation", "SELL"},
		{"hold position", "HOLD"},
		{"unknown", "HOLD"},
	}
	for _, tc := range tests {
		if got := DecisionToSignal(tc.in); got != tc.want {
			t.Fatalf("DecisionToSignal(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildTradeSignalEvent(t *testing.T) {
	memo := map[string]interface{}{
		"run_id":   "run-1",
		"ticker":   "AAPL",
		"decision": "BUY",
	}
	evt, result, err := BuildTradeSignalEvent(memo, ApproveRequest{OperatorID: "op-1", Amount: 50})
	if err != nil {
		t.Fatal(err)
	}
	if evt.Signal != "BUY" || evt.Book != "AAPL" || evt.Amount != 50 {
		t.Fatalf("unexpected event: %+v", evt)
	}
	if result.AuditID == "" || result.SignalEventID == "" {
		t.Fatalf("missing audit metadata: %+v", result)
	}
	if evt.Metadata["audit_id"] != result.AuditID {
		t.Fatalf("audit_id not in metadata")
	}
}

func TestBuildTradeSignalEventRejectsHold(t *testing.T) {
	memo := map[string]interface{}{
		"run_id":   "run-2",
		"ticker":   "AAPL",
		"decision": "HOLD",
	}
	_, _, err := BuildTradeSignalEvent(memo, ApproveRequest{OperatorID: "op-1"})
	if err == nil {
		t.Fatal("expected error for HOLD decision")
	}
}
