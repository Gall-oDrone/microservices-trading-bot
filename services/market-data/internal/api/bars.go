package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"bitso-trading-platform/market-data/internal/bars"
	"bitso-trading-platform/shared/pkg/models"
)

// GetBars returns OHLCV candles aggregated from recent trades (GET /api/v1/bars).
// Query: book (default btc_mxn), interval (default 1m), limit (default 30, max 500).
func (h *Handler) GetBars(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	book := r.URL.Query().Get("book")
	if book == "" {
		book = "btc_mxn"
	}
	intervalStr := r.URL.Query().Get("interval")
	if intervalStr == "" {
		intervalStr = "1m"
	}
	limit := 30
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if l, err := strconv.Atoi(ls); err == nil && l > 0 {
			if l > 500 {
				l = 500
			}
			limit = l
		}
	}

	interval, err := bars.ParseInterval(intervalStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	end := time.Now().UTC()
	start := bars.WindowStart(end, interval, limit)

	var tradeList []*models.TradeEvent
	if h.storage != nil {
		tradeList, err = h.storage.GetTradesByTimeRange(ctx, book, start, end)
		if err != nil {
			h.logger.Printf("GetBars storage time range: %v", err)
			tradeList = nil
		}
	}
	if len(tradeList) == 0 && h.cache != nil {
		// Fallback: recent trades list (filtered to window in cache implementation).
		fetch := limit * 200
		if fetch < 500 {
			fetch = 500
		}
		if fetch > 2000 {
			fetch = 2000
		}
		tradeList, err = h.cache.GetRecentTrades(ctx, book, fetch)
		if err != nil {
			h.logger.Printf("GetBars cache recent: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		filtered := tradeList[:0]
		for _, t := range tradeList {
			if t != nil && !t.Timestamp.Before(start) && !t.Timestamp.After(end) {
				filtered = append(filtered, t)
			}
		}
		tradeList = filtered
	}

	candles := bars.BuildFromTrades(tradeList, interval, limit)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"book":      book,
		"interval":  intervalStr,
		"count":     len(candles),
		"bars":      candles,
		"timestamp": end,
	})
}
