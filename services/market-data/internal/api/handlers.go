package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/market-data/internal/cache"
	"bitso-trading-platform/market-data/internal/historical"
	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
)

// Cache defines the interface for cache operations
// Note: Uses cache.TradeStats to match actual implementation
type Cache interface {
	GetRecentTrades(ctx context.Context, book string, limit int) ([]*models.TradeEvent, error)
	GetTrade(ctx context.Context, book string, tradeID uint64) (*models.TradeEvent, error)
	GetTradeStats(ctx context.Context, book string) (*cache.TradeStats, error)
	GetOrderBook(ctx context.Context, book string) (*bitso.OrderBook, error)
	GetTicker(ctx context.Context, book string) (*bitso.Ticker, error)
	Exists(ctx context.Context, key string) (bool, error)
}

// Storage defines the interface for storage operations
// Note: Uses historical types to match actual implementation
type Storage interface {
	GetOrderBookHistory(ctx context.Context, book string, start, end time.Time) ([]*historical.OrderBookSnapshot, error)
	GetTickerHistory(ctx context.Context, book string, start, end time.Time) ([]*bitso.Ticker, error)
	GetTradeStatistics(ctx context.Context, book string, start, end time.Time) (*historical.TradeStatistics, error)
	GetStorageStats(ctx context.Context) (*historical.StorageStats, error)
	GetVolumeStatistics(ctx context.Context, book string, start, end time.Time) (*historical.VolumeStatistics, error)
}

// TradeStats is an alias for cache.TradeStats to maintain API compatibility
type TradeStats = cache.TradeStats

// OrderBookSnapshot is an alias for historical.OrderBookSnapshot to maintain API compatibility
type OrderBookSnapshot = historical.OrderBookSnapshot

// TradeStatistics is an alias for historical.TradeStatistics to maintain API compatibility
type TradeStatistics = historical.TradeStatistics

// VolumeStatistics is an alias for historical.VolumeStatistics to maintain API compatibility
type VolumeStatistics = historical.VolumeStatistics

// VolumeLevel is an alias for historical.VolumeLevel to maintain API compatibility
type VolumeLevel = historical.VolumeLevel

// Handler handles HTTP API requests
type Handler struct {
	cache   Cache
	storage Storage
	logger  *log.Logger
}

// NewHandler creates a new API handler
func NewHandler(cache Cache, storage Storage, logger *log.Logger) *Handler {
	if logger == nil {
		logger = log.New(log.Writer(), "[API-HANDLER] ", log.LstdFlags|log.Lshortfile)
	}

	return &Handler{
		cache:   cache,
		storage: storage,
		logger:  logger,
	}
}

// RegisterRoutes registers all API routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Health check endpoints
	mux.HandleFunc("/health", h.HealthCheck)
	mux.HandleFunc("/health/live", h.LivenessCheck)
	mux.HandleFunc("/health/ready", h.ReadinessCheck)

	// Trade endpoints
	mux.HandleFunc("/api/v1/trades", h.GetTrades)
	mux.HandleFunc("/api/v1/trades/", h.GetTradeByID)
	mux.HandleFunc("/api/v1/trades/stats", h.GetTradeStats)

	// Order book endpoints
	mux.HandleFunc("/api/v1/orderbook", h.GetOrderBook)
	mux.HandleFunc("/api/v1/orderbook/history", h.GetOrderBookHistory)

	// Ticker endpoints
	mux.HandleFunc("/api/v1/ticker", h.GetTicker)
	mux.HandleFunc("/api/v1/ticker/history", h.GetTickerHistory)

	// Statistics endpoints
	mux.HandleFunc("/api/v1/stats/trades", h.GetTradeStatistics)
	mux.HandleFunc("/api/v1/stats/volume", h.GetVolumeStatistics)

	// Market data endpoints
	mux.HandleFunc("/api/v1/market/summary", h.GetMarketSummary)
	mux.HandleFunc("/api/v1/market/books", h.GetAvailableBooks)
}

// HealthCheck handles health check requests
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"service":   "market-data",
		"version":   "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// LivenessCheck handles liveness probe requests
func (h *Handler) LivenessCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Simple liveness check - service is alive if it can respond
	status := map[string]interface{}{
		"status":    "alive",
		"timestamp": time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// ReadinessCheck handles readiness probe requests
func (h *Handler) ReadinessCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if service is ready to serve requests
	ready := true
	checks := make(map[string]string)

	// Check cache connection
	if h.cache != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		if exists, err := h.cache.Exists(ctx, "health_check"); err != nil || !exists {
			checks["cache"] = "not ready"
			ready = false
		} else {
			checks["cache"] = "ready"
		}
	}

	// Check storage connection
	if h.storage != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		if _, err := h.storage.GetStorageStats(ctx); err != nil {
			checks["storage"] = "not ready"
			ready = false
		} else {
			checks["storage"] = "ready"
		}
	}

	status := map[string]interface{}{
		"status":    map[string]bool{"ready": ready},
		"timestamp": time.Now().UTC(),
		"checks":    checks,
	}

	w.Header().Set("Content-Type", "application/json")

	if ready {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(status)
}

// GetTrades handles trade retrieval requests
func (h *Handler) GetTrades(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	// Get trades from cache
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	trades, err := h.cache.GetRecentTrades(ctx, book, limit)
	if err != nil {
		h.logger.Printf("Error getting trades: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"book":      book,
		"trades":    trades,
		"count":     len(trades),
		"timestamp": time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetTradeByID handles individual trade retrieval requests
func (h *Handler) GetTradeByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract trade ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/trades/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid trade ID format", http.StatusBadRequest)
		return
	}

	book := parts[0]
	tradeIDStr := parts[1]

	tradeID, err := strconv.ParseUint(tradeIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid trade ID", http.StatusBadRequest)
		return
	}

	// Get trade from cache
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	trade, err := h.cache.GetTrade(ctx, book, tradeID)
	if err != nil {
		h.logger.Printf("Error getting trade: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if trade == nil {
		http.Error(w, "Trade not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(trade)
}

// GetTradeStats handles trade statistics requests
func (h *Handler) GetTradeStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	book := r.URL.Query().Get("book")
	if book == "" {
		book = "btc_mxn" // Default book
	}

	// Get trade statistics from cache
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	stats, err := h.cache.GetTradeStats(ctx, book)
	if err != nil {
		h.logger.Printf("Error getting trade stats: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if stats == nil {
		stats = &TradeStats{
			Book: book,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(stats)
}

// GetOrderBook handles order book retrieval requests
func (h *Handler) GetOrderBook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	book := r.URL.Query().Get("book")
	if book == "" {
		book = "btc_mxn" // Default book
	}

	// Get order book from cache
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	orderBook, err := h.cache.GetOrderBook(ctx, book)
	if err != nil {
		h.logger.Printf("Error getting order book: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if orderBook == nil {
		http.Error(w, "Order book not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(orderBook)
}

// GetOrderBookHistory handles order book history requests
func (h *Handler) GetOrderBookHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	book := r.URL.Query().Get("book")
	if book == "" {
		book = "btc_mxn" // Default book
	}

	// Parse time range parameters
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var start, end time.Time
	var err error

	if startStr != "" {
		start, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			http.Error(w, "Invalid start time format", http.StatusBadRequest)
			return
		}
	} else {
		start = time.Now().Add(-1 * time.Hour) // Default to last hour
	}

	if endStr != "" {
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			http.Error(w, "Invalid end time format", http.StatusBadRequest)
			return
		}
	} else {
		end = time.Now() // Default to now
	}

	// Get order book history from storage
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	history, err := h.storage.GetOrderBookHistory(ctx, book, start, end)
	if err != nil {
		h.logger.Printf("Error getting order book history: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"book":      book,
		"start":     start,
		"end":       end,
		"history":   history,
		"count":     len(history),
		"timestamp": time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetTicker handles ticker retrieval requests
func (h *Handler) GetTicker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	book := r.URL.Query().Get("book")
	if book == "" {
		book = "btc_mxn" // Default book
	}

	// Get ticker from cache
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	ticker, err := h.cache.GetTicker(ctx, book)
	if err != nil {
		h.logger.Printf("Error getting ticker: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if ticker == nil {
		http.Error(w, "Ticker not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ticker)
}

// GetTickerHistory handles ticker history requests
func (h *Handler) GetTickerHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	book := r.URL.Query().Get("book")
	if book == "" {
		book = "btc_mxn" // Default book
	}

	// Parse time range parameters
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var start, end time.Time
	var err error

	if startStr != "" {
		start, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			http.Error(w, "Invalid start time format", http.StatusBadRequest)
			return
		}
	} else {
		start = time.Now().Add(-1 * time.Hour) // Default to last hour
	}

	if endStr != "" {
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			http.Error(w, "Invalid end time format", http.StatusBadRequest)
			return
		}
	} else {
		end = time.Now() // Default to now
	}

	// Get ticker history from storage
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	history, err := h.storage.GetTickerHistory(ctx, book, start, end)
	if err != nil {
		h.logger.Printf("Error getting ticker history: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"book":      book,
		"start":     start,
		"end":       end,
		"history":   history,
		"count":     len(history),
		"timestamp": time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetTradeStatistics handles trade statistics requests
func (h *Handler) GetTradeStatistics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	book := r.URL.Query().Get("book")
	if book == "" {
		book = "btc_mxn" // Default book
	}

	// Parse time range parameters
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var start, end time.Time
	var err error

	if startStr != "" {
		start, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			http.Error(w, "Invalid start time format", http.StatusBadRequest)
			return
		}
	} else {
		start = time.Now().Add(-24 * time.Hour) // Default to last 24 hours
	}

	if endStr != "" {
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			http.Error(w, "Invalid end time format", http.StatusBadRequest)
			return
		}
	} else {
		end = time.Now() // Default to now
	}

	// Get trade statistics from storage
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	stats, err := h.storage.GetTradeStatistics(ctx, book, start, end)
	if err != nil {
		h.logger.Printf("Error getting trade statistics: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(stats)
}

// GetVolumeStatistics handles volume statistics requests
func (h *Handler) GetVolumeStatistics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	book := r.URL.Query().Get("book")
	if book == "" {
		book = "btc_mxn" // Default book
	}

	// Parse time range parameters
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var start, end time.Time
	var err error

	if startStr != "" {
		start, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			http.Error(w, "Invalid start time format", http.StatusBadRequest)
			return
		}
	} else {
		start = time.Now().Add(-24 * time.Hour) // Default to last 24 hours
	}

	if endStr != "" {
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			http.Error(w, "Invalid end time format", http.StatusBadRequest)
			return
		}
	} else {
		end = time.Now() // Default to now
	}

	// Get volume statistics from storage
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	stats, err := h.storage.GetVolumeStatistics(ctx, book, start, end)
	if err != nil {
		h.logger.Printf("Error getting volume statistics: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(stats)
}

// GetMarketSummary handles market summary requests
func (h *Handler) GetMarketSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get market summary for all available books
	books := []string{"btc_mxn", "eth_mxn", "xrp_mxn", "ltc_mxn", "bch_mxn"}

	summary := make(map[string]interface{})

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	for _, book := range books {
		// Get ticker for each book
		ticker, err := h.cache.GetTicker(ctx, book)
		if err != nil {
			h.logger.Printf("Error getting ticker for %s: %v", book, err)
			continue
		}

		if ticker != nil {
			summary[book] = ticker
		}
	}

	response := map[string]interface{}{
		"summary":   summary,
		"timestamp": time.Now().UTC(),
		"count":     len(summary),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetAvailableBooks handles available books requests
func (h *Handler) GetAvailableBooks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Return list of available trading books
	books := []string{
		"btc_mxn",
		"eth_mxn",
		"xrp_mxn",
		"ltc_mxn",
		"bch_mxn",
	}

	response := map[string]interface{}{
		"books":     books,
		"count":     len(books),
		"timestamp": time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
