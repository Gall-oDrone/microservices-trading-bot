package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

func newLifecycleHandler(t *testing.T) *StrategyHandler {
	t.Helper()
	svc := indicators.NewService(nil, indicators.NewInMemoryIndicatorStore(), indicators.NewMockDataProvider(), nil)
	reg := strategies.NewEnhancedRegistry(svc)
	if _, err := reg.CreateAndRegister(strategies.StrategyConfig{Name: "mr", Type: "mean_reversion", Enabled: true, Book: "btc_mxn"}); err != nil {
		t.Fatal(err)
	}
	return &StrategyHandler{registry: reg}
}

func doLifecycle(t *testing.T, h *StrategyHandler, method, path, body string) (int, map[string]interface{}) {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	if path == "/api/v1/strategies" {
		h.HandleStrategies(rec, req)
	} else {
		h.HandleStrategy(rec, req)
	}
	out := map[string]interface{}{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestStrategyLifecycleHTTP_PlainStartStopUnchanged(t *testing.T) {
	h := newLifecycleHandler(t)

	// No body: what strategy-router and start-organic-trading.sh send.
	code, out := doLifecycle(t, h, http.MethodPost, "/api/v1/strategies/mr/start", "")
	if code != http.StatusOK || out["status"] != "started" || out["name"] != "mr" {
		t.Fatalf("start = %d %v", code, out)
	}
	code, out = doLifecycle(t, h, http.MethodPost, "/api/v1/strategies/mr/start", "")
	if code != http.StatusConflict || out["code"] != "already_running" {
		t.Fatalf("second start = %d %v", code, out)
	}
	code, out = doLifecycle(t, h, http.MethodPost, "/api/v1/strategies/mr/stop", "")
	if code != http.StatusOK || out["status"] != "stopped" {
		t.Fatalf("stop = %d %v", code, out)
	}
	code, out = doLifecycle(t, h, http.MethodPost, "/api/v1/strategies/mr/stop", "")
	if code != http.StatusConflict || out["code"] != "not_running" {
		t.Fatalf("second stop = %d %v", code, out)
	}
	code, out = doLifecycle(t, h, http.MethodPost, "/api/v1/strategies/ghost/start", "")
	if code != http.StatusNotFound || out["code"] != "not_found" {
		t.Fatalf("unknown = %d %v", code, out)
	}
	code, _ = doLifecycle(t, h, http.MethodPost, "/api/v1/strategies/mr/start", "{bad")
	if code != http.StatusBadRequest {
		t.Fatalf("bad body = %d", code)
	}
}

func TestStrategyLifecycleHTTP_HoldAndRelease(t *testing.T) {
	h := newLifecycleHandler(t)
	if code, _ := doLifecycle(t, h, http.MethodPost, "/api/v1/strategies/mr/start", ""); code != http.StatusOK {
		t.Fatal("start failed")
	}

	code, out := doLifecycle(t, h, http.MethodPost, "/api/v1/strategies/mr/stop",
		`{"hold":true,"by":" diego ","reason":"fee drift"}`)
	if code != http.StatusOK || out["held"] != true || out["was_running"] != true {
		t.Fatalf("hold stop = %d %v", code, out)
	}
	hold, _ := out["hold"].(map[string]interface{})
	if hold["by"] != "diego" || hold["reason"] != "fee drift" {
		t.Fatalf("hold entry = %v", hold)
	}

	// Holding an already-stopped strategy is allowed (idempotent operator stop).
	code, out = doLifecycle(t, h, http.MethodPost, "/api/v1/strategies/mr/stop", `{"hold":true}`)
	if code != http.StatusOK || out["was_running"] != false {
		t.Fatalf("hold while stopped = %d %v", code, out)
	}
	if hold, _ := out["hold"].(map[string]interface{}); hold["by"] != "api" {
		t.Fatalf("default by = %v", hold)
	}

	// The router / script cannot start it.
	code, out = doLifecycle(t, h, http.MethodPost, "/api/v1/strategies/mr/start", "")
	if code != http.StatusConflict || out["code"] != "held" {
		t.Fatalf("plain start while held = %d %v", code, out)
	}

	// The list shows the hold on the strategy and in the top-level map.
	code, out = doLifecycle(t, h, http.MethodGet, "/api/v1/strategies", "")
	if code != http.StatusOK {
		t.Fatalf("list = %d", code)
	}
	holds, _ := out["holds"].(map[string]interface{})
	if _, ok := holds["mr"]; !ok {
		t.Fatalf("holds = %v", out["holds"])
	}
	list, _ := out["strategies"].([]interface{})
	first, _ := list[0].(map[string]interface{})
	if first["hold"] == nil || first["running"] != false {
		t.Fatalf("strategy entry = %v", first)
	}

	// An operator start releases the hold.
	code, out = doLifecycle(t, h, http.MethodPost, "/api/v1/strategies/mr/start",
		`{"release_hold":true,"by":"diego","reason":"fixed"}`)
	if code != http.StatusOK || out["status"] != "started" || out["released_hold"] == nil {
		t.Fatalf("release start = %d %v", code, out)
	}
	code, out = doLifecycle(t, h, http.MethodGet, "/api/v1/strategies", "")
	if holds, _ := out["holds"].(map[string]interface{}); code != http.StatusOK || len(holds) != 0 {
		t.Fatalf("holds after release = %v", out["holds"])
	}
}
