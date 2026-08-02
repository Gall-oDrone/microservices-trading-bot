package health_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bitso-trading-platform/data-collector/internal/clock"
	"bitso-trading-platform/data-collector/internal/health"
)

func TestHealthzStalenessWithFakeClock(t *testing.T) {
	start := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	clk := &clock.FakeClock{T: start}
	c := health.NewChecker(clk, 5*time.Minute)

	healthy, reason, _ := c.Status()
	if !healthy || reason != "waiting for first trade" {
		t.Fatalf("cold start: healthy=%v reason=%s", healthy, reason)
	}

	c.RecordTrade(clk.Now())
	clk.Advance(4 * time.Minute)
	healthy, _, age := c.Status()
	if !healthy || age != 4*time.Minute {
		t.Fatalf("within window: healthy=%v age=%s", healthy, age)
	}

	clk.Advance(2 * time.Minute) // 6m since last trade
	healthy, reason, age = c.Status()
	if healthy || age != 6*time.Minute {
		t.Fatalf("stale: healthy=%v reason=%s age=%s", healthy, reason, age)
	}
}

func TestHealthzNoTradesAfterStaleWindow(t *testing.T) {
	clk := &clock.FakeClock{T: time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)}
	c := health.NewChecker(clk, 5*time.Minute)
	clk.Advance(6 * time.Minute)
	healthy, reason, _ := c.Status()
	if healthy || reason == "" {
		t.Fatalf("expected unhealthy, got healthy=%v reason=%s", healthy, reason)
	}
}

func TestHealthzHTTPStatus(t *testing.T) {
	clk := &clock.FakeClock{T: time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)}
	c := health.NewChecker(clk, time.Minute)
	c.RecordTrade(clk.Now())
	clk.Advance(2 * time.Minute)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	c.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rr.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "unhealthy" {
		t.Fatalf("body=%v", body)
	}
}
