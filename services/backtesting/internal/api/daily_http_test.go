package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"bitso-trading-platform/backtesting/internal/engine"
	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/manager"
	"bitso-trading-platform/backtesting/internal/storage"
)

// TestDailyStrategyOverHTTP posts the request documented in
// docs/OPERATIONS-ENV-VARS.md through the real handler, manager, engine and
// file storage, and expects the registered 2025 btc_mxn taker result.
func TestDailyStrategyOverHTTP(t *testing.T) {
	log := logger.NewDefault()
	store := storage.NewFileStorage(t.TempDir(), log)
	eng := engine.NewEngine(nil, store, log, nil)
	eng.SetDailyBarsDir(filepath.Join("..", "..", "..", "..", "docs", "backtest-readiness", "evidence-2026-09-27"))
	mgr := manager.NewBacktestManager(eng, store, 1, log, nil)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Stop() }()
	h := NewHandler(mgr, nil, log, nil)

	body := []byte(`{"name":"sma50 2025","book":"btc_mxn","start_date":"2025-01-01T00:00:00Z","end_date":"2025-12-31T00:00:00Z",
	 "initial_balance":100000,"strategy":"sma50_daily","data_source":"file",
	 "taker_fee":0.0078,"slippage_model":"percentage","slippage_value":0.001}`)
	rec := httptest.NewRecorder()
	h.HandleBacktests(rec, httptest.NewRequest(http.MethodPost, "/api/v1/backtests", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Data.ID == "" {
		t.Fatalf("create body %s", rec.Body.String())
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		res, err := mgr.GetBacktestResult(created.Data.ID)
		if err == nil && res != nil && res.Status == "completed" {
			// Registered in evidence-2026-09-28/btc-mxn-taker-same-windows.json.
			if res.Summary.TotalReturnPercent != -23.52507123070574 || res.Summary.TotalTrades != 16 {
				t.Fatalf("return %v trips %d", res.Summary.TotalReturnPercent, res.Summary.TotalTrades)
			}
			if len(res.EquityCurve) != 365 || res.Config == nil || res.Config.Strategy != "sma50_daily" {
				t.Fatalf("equity points %d config %+v", len(res.EquityCurve), res.Config)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no completed result: %+v %v", res, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
