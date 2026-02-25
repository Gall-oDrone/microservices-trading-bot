package export

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBacktestCompletionEvent_JSON(t *testing.T) {
	ev := &BacktestCompletionEvent{
		Event:       "backtest_completion",
		Outcome:     "completed",
		BacktestID:  "bt-123",
		Strategy:    "basic",
		Book:        "btc_mxn",
		TotalTrades: 10,
		SharpeRatio: 1.5,
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded BacktestCompletionEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.BacktestID != ev.BacktestID || decoded.SharpeRatio != ev.SharpeRatio {
		t.Errorf("round-trip mismatch: got %+v", decoded)
	}
}

func TestWebhookNotifier_Notify(t *testing.T) {
	var received []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json")
		}
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		received = buf[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewWebhookNotifier(server.URL)
	ev := &BacktestCompletionEvent{Event: "backtest_completion", Outcome: "completed", BacktestID: "bt-1"}
	err := n.Notify(context.Background(), ev)
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(received) == 0 {
		t.Fatal("expected body to be received")
	}
	var decoded BacktestCompletionEvent
	if err := json.Unmarshal(received, &decoded); err != nil {
		t.Fatalf("unmarshal received: %v", err)
	}
	if decoded.BacktestID != "bt-1" {
		t.Errorf("expected backtest_id bt-1, got %s", decoded.BacktestID)
	}
	if n.Name() != "webhook" {
		t.Errorf("Name() = %s, want webhook", n.Name())
	}
}
