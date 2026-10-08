package execution

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPositionReadsExposure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/risk/exposure" || r.URL.Query().Get("book") != "btc_mxn" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"book":"btc_mxn","position_size":0.0042,"open_orders":1}`))
	}))
	defer srv.Close()

	pos, err := NewOrderManagementRiskProvider(srv.URL).Position(context.Background(), "btc_mxn")
	if err != nil || pos != 0.0042 {
		t.Fatalf("got %v, %v", pos, err)
	}
}

func TestPositionErrors(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"status": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
		"body":   func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`not json`)) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			if _, err := NewOrderManagementRiskProvider(srv.URL).Position(context.Background(), "btc_mxn"); err == nil {
				t.Fatal("want error")
			}
		})
	}
}
