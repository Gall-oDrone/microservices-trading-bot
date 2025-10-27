package api

import (
	"net/http"
	"strconv"
	"strings"

	"bitso-trading-platform/api-gateway/internal/client"
	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
)

// MarketDataHandler handles market data related requests
type MarketDataHandler struct {
	client  client.MarketDataClient
	logger  *logger.Logger
	metrics *metrics.MetricsCollector
}

// NewMarketDataHandler creates a new market data handler
func NewMarketDataHandler(
	client client.MarketDataClient,
	logger *logger.Logger,
	metrics *metrics.MetricsCollector,
) *MarketDataHandler {
	return &MarketDataHandler{
		client:  client,
		logger:  logger.WithComponent("market-data-handler"),
		metrics: metrics,
	}
}

// HandleGetTrades handles GET /api/v1/market-data/trades
func (h *MarketDataHandler) HandleGetTrades(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Parse query parameters
	book := r.URL.Query().Get("book")
	if book == "" {
		book = "btc_mxn" // Default book
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 100 // Default limit
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
			if limit > 1000 {
				limit = 1000 // Max limit
			}
		}
	}

	h.logger.Debug("Getting recent trades", map[string]interface{}{
		"book":  book,
		"limit": limit,
	})

	// Call client
	trades, err := h.client.GetRecentTrades(r.Context(), book, limit)
	if err != nil {
		h.logger.Error("Failed to get recent trades", map[string]interface{}{
			"error": err.Error(),
			"book":  book,
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, trades)
}

// HandleGetTrade handles GET /api/v1/market-data/trades/{book}/{id}
func (h *MarketDataHandler) HandleGetTrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Extract path parameters
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/market-data/trades/")
	parts := strings.Split(path, "/")
	
	if len(parts) != 2 {
		BadRequestResponse(w, r, "Invalid path format. Expected: /api/v1/market-data/trades/{book}/{id}")
		return
	}

	book := parts[0]
	tradeIDStr := parts[1]

	tradeID, err := strconv.ParseUint(tradeIDStr, 10, 64)
	if err != nil {
		BadRequestResponse(w, r, "Invalid trade ID")
		return
	}

	h.logger.Debug("Getting trade", map[string]interface{}{
		"book":     book,
		"trade_id": tradeID,
	})

	// Call client
	trade, err := h.client.GetTrade(r.Context(), book, tradeID)
	if err != nil {
		h.logger.Error("Failed to get trade", map[string]interface{}{
			"error":    err.Error(),
			"book":     book,
			"trade_id": tradeID,
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, trade)
}

// HandleGetTradeStats handles GET /api/v1/market-data/stats/trades
func (h *MarketDataHandler) HandleGetTradeStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Parse query parameters
	book := r.URL.Query().Get("book")
	if book == "" {
		book = "btc_mxn" // Default book
	}

	h.logger.Debug("Getting trade stats", map[string]interface{}{
		"book": book,
	})

	// Call client
	stats, err := h.client.GetTradeStats(r.Context(), book)
	if err != nil {
		h.logger.Error("Failed to get trade stats", map[string]interface{}{
			"error": err.Error(),
			"book":  book,
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, stats)
}

// HandleGetOrderBook handles GET /api/v1/market-data/orderbook
func (h *MarketDataHandler) HandleGetOrderBook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Parse query parameters
	book := r.URL.Query().Get("book")
	if book == "" {
		book = "btc_mxn" // Default book
	}

	h.logger.Debug("Getting order book", map[string]interface{}{
		"book": book,
	})

	// Call client
	orderBook, err := h.client.GetOrderBook(r.Context(), book)
	if err != nil {
		h.logger.Error("Failed to get order book", map[string]interface{}{
			"error": err.Error(),
			"book":  book,
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, orderBook)
}

// HandleGetTicker handles GET /api/v1/market-data/ticker
func (h *MarketDataHandler) HandleGetTicker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Parse query parameters
	book := r.URL.Query().Get("book")
	if book == "" {
		book = "btc_mxn" // Default book
	}

	h.logger.Debug("Getting ticker", map[string]interface{}{
		"book": book,
	})

	// Call client
	ticker, err := h.client.GetTicker(r.Context(), book)
	if err != nil {
		h.logger.Error("Failed to get ticker", map[string]interface{}{
			"error": err.Error(),
			"book":  book,
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, ticker)
}

// HandleGetMarketSummary handles GET /api/v1/market-data/summary
func (h *MarketDataHandler) HandleGetMarketSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	h.logger.Debug("Getting market summary", nil)

	// Call client
	summary, err := h.client.GetMarketSummary(r.Context())
	if err != nil {
		h.logger.Error("Failed to get market summary", map[string]interface{}{
			"error": err.Error(),
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, summary)
}

