package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollect_success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/strategies", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"strategies": []map[string]interface{}{
				{"name": "s1", "book": "btc_mxn", "running": true, "type": "mean_reversion"},
			},
			"count": 1,
		})
	})
	mux.HandleFunc("/api/v1/indicators/btc_mxn/snapshot", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"rsi_14": map[string]float64{"value": 55}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	snap, err := Collect(context.Background(), srv.Client(), srv.URL, "paper", FeeEstimateOptions{})
	require.NoError(t, err)
	require.Empty(t, snap.CollectionErrors)
	require.Len(t, snap.Strategies, 1)
	require.Equal(t, 1, snap.StrategyCount)
	raw, ok := snap.Indicators["btc_mxn"]
	require.True(t, ok)
	require.True(t, strings.Contains(string(raw), "rsi_14"))
}

func TestCollect_strategiesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	snap, err := Collect(context.Background(), srv.Client(), srv.URL, "paper", FeeEstimateOptions{})
	require.NoError(t, err)
	require.Contains(t, snap.CollectionErrors["strategies"], "500")
}

func TestCollect_indicatorErrorStillReturnsSnapshot(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/strategies", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"strategies": []map[string]interface{}{
				{"name": "s1", "book": "eth_mxn"},
			},
		})
	})
	mux.HandleFunc("/api/v1/indicators/eth_mxn/snapshot", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusBadGateway)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	snap, err := Collect(context.Background(), srv.Client(), srv.URL, "paper", FeeEstimateOptions{})
	require.NoError(t, err)
	require.Contains(t, snap.CollectionErrors, "indicators:eth_mxn")
	require.Len(t, snap.Strategies, 1)
}

func TestUniqueBooks(t *testing.T) {
	out := uniqueBooks([]map[string]interface{}{
		{"book": "btc_mxn"},
		{"book": "btc_mxn"},
		{"book": "eth_mxn"},
	})
	require.ElementsMatch(t, []string{"btc_mxn", "eth_mxn"}, out)
}
