package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"bitso-trading-platform/api-gateway/internal/config"
	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
)

// newRouteTestMux registers the real gateway routes. Sub-handlers are nil:
// the cases below must be rejected before any of them is reached.
func newRouteTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	cfg := &config.Config{}
	cfg.Service.Name = "api-gateway-test"
	h := NewHandler(cfg, logger.New(&logger.Config{Level: "error", Format: "json"}),
		metrics.NewMetricsCollector("api_gateway_route_test"), nil, nil, nil, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

// TestPublicRoutesAreReadOnly pins plan §7 items 3-4: no strategy control,
// signal publishing, order cancel or order placement through the gateway.
func TestPublicRoutesAreReadOnly(t *testing.T) {
	mux := newRouteTestMux(t)
	cases := []struct {
		method, path string
		want         int
	}{
		// Strategy control sub-paths are gone.
		{http.MethodPost, "/api/v1/strategies/sma50/start", http.StatusNotFound},
		{http.MethodPost, "/api/v1/strategies/sma50/stop", http.StatusNotFound},
		{http.MethodGet, "/api/v1/strategies/sma50/start", http.StatusNotFound},
		{http.MethodPut, "/api/v1/strategies/sma50/config", http.StatusNotFound},
		// strategy-executor internals are not strategy names.
		{http.MethodPost, "/api/v1/strategies/process", http.StatusNotFound},
		{http.MethodGet, "/api/v1/strategies/process", http.StatusNotFound},
		{http.MethodPost, "/api/v1/strategies/order-fill", http.StatusNotFound},
		{http.MethodGet, "/api/v1/strategies/.hidden", http.StatusNotFound},
		{http.MethodGet, "/api/v1/strategies/", http.StatusNotFound},
		// A valid name with a write method is 405, not forwarded.
		{http.MethodPost, "/api/v1/strategies/sma50", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/api/v1/strategies/sma50", http.StatusMethodNotAllowed},
		// Order and position routes no longer exist.
		{http.MethodPost, "/api/v1/orders", http.StatusNotFound},
		{http.MethodPost, "/api/v1/orders/1/cancel", http.StatusNotFound},
		{http.MethodDelete, "/api/v1/orders/1", http.StatusNotFound},
		{http.MethodGet, "/api/v1/positions", http.StatusNotFound},
		// Test signal injection was never proxied and must stay that way.
		{http.MethodPost, "/api/v1/test/signals", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.want {
				t.Fatalf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
			}
			if tc.want == http.StatusMethodNotAllowed && rec.Header().Get("Allow") != http.MethodGet {
				t.Errorf("Allow = %q, want GET", rec.Header().Get("Allow"))
			}
		})
	}
}

func TestStrategyNameRe(t *testing.T) {
	for _, ok := range []string{"sma50", "SMA_50", "trend-follow.v2", "a"} {
		if !strategyNameRe.MatchString(ok) {
			t.Errorf("%q should be a valid name", ok)
		}
	}
	for _, bad := range []string{"", ".", "..", "-x", "a b", "a/b", "a%2Fb"} {
		if strategyNameRe.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
