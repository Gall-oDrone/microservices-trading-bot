package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

type noTrades struct{}

func (noTrades) GetRecentTrades(context.Context, string, int) ([]indicators.Trade, error) {
	return nil, nil
}

func serve(t *testing.T, s *Server, method, path string) (int, http.Header, map[string]interface{}) {
	t.Helper()
	var body *strings.Reader
	if method == http.MethodPost {
		body = strings.NewReader(`{"book":"btc_mxn","strategy":"momentum"}`)
	} else {
		body = strings.NewReader("")
	}
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(method, path, body))
	out := map[string]interface{}{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, rec.Header(), out
}

func TestBacktestRoutesAreGone(t *testing.T) {
	t.Setenv(LegacyBacktestsEnv, "")
	// With and without a trade source wired, the routes answer 410.
	for _, opts := range []*ServerOptions{nil, {BacktestTradeSource: noTrades{}}} {
		s := NewWithOptions(&Config{Host: "127.0.0.1", Port: 0}, nil, nil, opts)
		for _, c := range []struct{ method, path string }{
			{http.MethodPost, "/api/v1/backtests"},
			{http.MethodGet, "/api/v1/backtests"},
			{http.MethodGet, "/api/v1/backtests/bt-1/report"},
		} {
			code, hdr, body := serve(t, s, c.method, c.path)
			if code != http.StatusGone {
				t.Fatalf("%s %s = %d, want 410", c.method, c.path, code)
			}
			if hdr.Get("Deprecation") != "true" || !strings.Contains(body["successor"].(string), "services/backtesting") {
				t.Fatalf("%s %s: headers %v body %v", c.method, c.path, hdr, body)
			}
		}
	}
}

func TestBacktestRoutesLegacyOptIn(t *testing.T) {
	t.Setenv(LegacyBacktestsEnv, "1")
	s := NewWithOptions(&Config{Host: "127.0.0.1", Port: 0}, nil, nil, &ServerOptions{BacktestTradeSource: noTrades{}})
	code, _, body := serve(t, s, http.MethodGet, "/api/v1/backtests")
	if code != http.StatusOK || body["count"] != float64(0) {
		t.Fatalf("legacy list = %d %v", code, body)
	}
	// Without a trade source the legacy handler cannot run: still 410.
	s = NewWithOptions(&Config{Host: "127.0.0.1", Port: 0}, nil, nil, nil)
	if code, _, _ := serve(t, s, http.MethodGet, "/api/v1/backtests"); code != http.StatusGone {
		t.Fatalf("legacy without source = %d, want 410", code)
	}
}
