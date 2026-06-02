package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSnapshot_PascalCaseBollinger(t *testing.T) {
	body := `{
		"book": "btc_mxn",
		"sma": {"value": 100},
		"ema": {"value": 99},
		"rsi": {"value": 55},
		"bollinger": {"Upper": 110, "Middle": 100, "Lower": 90}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/indicators/btc_mxn/snapshot" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, 0)
	snap, err := c.GetSnapshot(context.Background(), "btc_mxn")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Price != 100 {
		t.Fatalf("price = %v, want 100", snap.Price)
	}
	if snap.BBUpper != 110 || snap.BBLower != 90 {
		t.Fatalf("bands upper=%v lower=%v", snap.BBUpper, snap.BBLower)
	}
	if snap.RSI != 55 {
		t.Fatalf("rsi = %v", snap.RSI)
	}
}

func TestGetSnapshot_SnakeCaseBollinger(t *testing.T) {
	body := `{
		"book": "btc_mxn",
		"sma": {"value": 200},
		"bollinger": {"upper_band": 220, "middle_band": 200, "lower_band": 180, "current_price": 205}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/indicators/btc_mxn/snapshot" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, 0)
	snap, err := c.GetSnapshot(context.Background(), "btc_mxn")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Price != 205 {
		t.Fatalf("price = %v, want 205 from current_price", snap.Price)
	}
	if snap.BBUpper != 220 {
		t.Fatalf("upper = %v", snap.BBUpper)
	}
}
