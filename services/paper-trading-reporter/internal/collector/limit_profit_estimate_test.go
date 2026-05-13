package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollect_limitProfitEstimates_bitsoOverrides(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/strategies", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"strategies": []map[string]interface{}{
				{
					"name": "lp1", "type": "limit_profit", "book": "btc_mxn", "running": true,
					"parameters": map[string]interface{}{
						"use_bitso_fees": true,
						"buy_liquidity":  "maker",
						"sell_liquidity": "taker",
						"min_profit":     float64(1),
						"min_profit_bps": float64(0),
						"fee":            float64(0),
						"fee_bps":        float64(0),
					},
					"state": map[string]interface{}{
						"has_position":  true,
						"entry_price":   float64(1366740),
						"position_size": float64(0.001),
					},
				},
			},
			"count": 1,
		})
	})
	mux.HandleFunc("/api/v1/indicators/btc_mxn/snapshot", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	opts := FeeEstimateOptions{
		OverrideBuyFeeDecimal:  0.0057,
		OverrideSellFeeDecimal: 0.00741,
	}
	snap, err := Collect(context.Background(), srv.Client(), srv.URL, "paper", opts)
	require.NoError(t, err)
	require.Len(t, snap.LimitProfitPnLEstimates, 1)
	e := snap.LimitProfitPnLEstimates[0]
	require.Equal(t, "lp1", e.StrategyName)
	require.Equal(t, "bitso_api", e.FeeModel)
	require.InDelta(t, 1384791.72, e.BreakevenExitLast, 0.02)
	require.InDelta(t, 1384792.72, e.TakeProfitThresholdLast, 0.02)
	require.InDelta(t, 18.052725, e.EstGrossPnlQuoteAtThresh, 0.0001)
	require.InDelta(t, 0.000993, e.EstNetPnlQuoteAtThresh, 1e-6)
}

func TestCollect_limitProfitEstimates_noPositionSkip(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/strategies", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"strategies": []map[string]interface{}{
				{
					"name": "lp1", "type": "limit_profit", "book": "btc_mxn", "running": true,
					"parameters": map[string]interface{}{"use_bitso_fees": true},
					"state":      map[string]interface{}{"has_position": false},
				},
			},
			"count": 1,
		})
	})
	mux.HandleFunc("/api/v1/indicators/btc_mxn/snapshot", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	snap, err := Collect(context.Background(), srv.Client(), srv.URL, "paper", FeeEstimateOptions{})
	require.NoError(t, err)
	require.Len(t, snap.LimitProfitPnLEstimates, 1)
	require.Equal(t, "no_open_position", snap.LimitProfitPnLEstimates[0].SkipReason)
}

func TestCollect_limitProfitEstimates_manualFees(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/strategies", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"strategies": []map[string]interface{}{
				{
					"name": "lpm", "type": "limit_profit", "book": "btc_mxn", "running": true,
					"parameters": map[string]interface{}{
						"use_bitso_fees": false,
						"min_profit":     float64(200),
						"fee":            float64(500),
						"fee_bps":        float64(0),
					},
					"state": map[string]interface{}{
						"has_position":  true,
						"entry_price":   float64(1_000_100),
						"position_size": float64(0.001),
					},
				},
			},
			"count": 1,
		})
	})
	mux.HandleFunc("/api/v1/indicators/btc_mxn/snapshot", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	snap, err := Collect(context.Background(), srv.Client(), srv.URL, "paper", FeeEstimateOptions{})
	require.NoError(t, err)
	require.Len(t, snap.LimitProfitPnLEstimates, 1)
	e := snap.LimitProfitPnLEstimates[0]
	require.Equal(t, "manual_estimate", e.FeeModel)
	// threshold = 1_000_100 + 200 + 500 = 1_000_800
	require.InDelta(t, 1_000_800, e.TakeProfitThresholdLast, 0.01)
	want := (1_000_800 - 1_000_100) * 0.001
	require.InDelta(t, want, e.EstGrossPnlQuoteAtThresh, 1e-9)
	require.InDelta(t, want, e.EstNetPnlQuoteAtThresh, 1e-9)
}

func TestToFloat_jsonNumber(t *testing.T) {
	n := json.Number("1366740.5")
	require.InDelta(t, 1366740.5, toFloat(n), 1e-9)
}

func TestCollect_meanReversionNoEstimateRow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/strategies", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"strategies": []map[string]interface{}{
				{"name": "mr", "type": "mean_reversion", "book": "btc_mxn", "running": true},
			},
			"count": 1,
		})
	})
	mux.HandleFunc("/api/v1/indicators/btc_mxn/snapshot", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	snap, err := Collect(context.Background(), srv.Client(), srv.URL, "paper", FeeEstimateOptions{})
	require.NoError(t, err)
	require.Empty(t, snap.LimitProfitPnLEstimates)
}
