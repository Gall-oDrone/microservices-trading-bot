package api

import (
	"net/http"
	"time"

	"bitso-trading-platform/api-gateway/internal/config"
	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
	"bitso-trading-platform/shared/pkg/health"
)

// Handler is the main API handler that aggregates all sub-handlers
type Handler struct {
	config             *config.Config
	logger             *logger.Logger
	metrics            *metrics.MetricsCollector
	healthManager      *health.HealthManager
	marketDataHandler  *MarketDataHandler
	orderHandler       *OrderHandler
	strategyHandler    *StrategyHandler
	aggregationHandler *AggregationHandler
	researchHandler    *ResearchHandler
}

// NewHandler creates a new main API handler
func NewHandler(
	cfg *config.Config,
	logger *logger.Logger,
	metrics *metrics.MetricsCollector,
	healthManager *health.HealthManager,
	marketDataHandler *MarketDataHandler,
	orderHandler *OrderHandler,
	strategyHandler *StrategyHandler,
	aggregationHandler *AggregationHandler,
	researchHandler *ResearchHandler,
) *Handler {
	return &Handler{
		config:             cfg,
		logger:             logger.WithComponent("api-handler"),
		metrics:            metrics,
		healthManager:      healthManager,
		marketDataHandler:  marketDataHandler,
		orderHandler:       orderHandler,
		strategyHandler:    strategyHandler,
		aggregationHandler: aggregationHandler,
		researchHandler:    researchHandler,
	}
}

// HandleHealth handles GET /health
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Use the health manager from shared package
	h.healthManager.HTTPHandler()(w, r)
}

// HandleLiveness handles GET /health/live
func (h *Handler) HandleLiveness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Simple liveness check - if we can respond, we're alive
	SuccessResponse(w, r, map[string]interface{}{
		"status":    "alive",
		"timestamp": time.Now().UTC(),
		"service":   h.config.Service.Name,
	})
}

// HandleReadiness handles GET /health/ready
func (h *Handler) HandleReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Use the health manager readiness handler
	h.healthManager.ReadinessHandler()(w, r)
}

// HandleStatus handles GET /api/v1/status
func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	status := map[string]interface{}{
		"service":     h.config.Service.Name,
		"version":     h.config.Service.Version,
		"environment": h.config.Service.Environment,
		"status":      "running",
		"timestamp":   time.Now().UTC(),
	}

	SuccessResponse(w, r, status)
}

// HandleVersion handles GET /api/v1/version
func (h *Handler) HandleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	version := map[string]interface{}{
		"service":     h.config.Service.Name,
		"version":     h.config.Service.Version,
		"api_version": "v1",
	}

	SuccessResponse(w, r, version)
}

// HandleNotFound handles 404 Not Found
func (h *Handler) HandleNotFound(w http.ResponseWriter, r *http.Request) {
	h.logger.Warn("Route not found", map[string]interface{}{
		"path":   r.URL.Path,
		"method": r.Method,
	})

	NotFoundResponse(w, r, "Route not found")
}

// RegisterRoutes registers all API routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Health and status endpoints
	mux.HandleFunc("/health", h.HandleHealth)
	mux.HandleFunc("/health/live", h.HandleLiveness)
	mux.HandleFunc("/health/ready", h.HandleReadiness)
	mux.HandleFunc("/api/v1/status", h.HandleStatus)
	mux.HandleFunc("/api/v1/version", h.HandleVersion)

	// Market data endpoints
	mux.HandleFunc("/api/v1/market-data/trades", h.marketDataHandler.HandleGetTrades)
	mux.HandleFunc("/api/v1/market-data/trades/", h.marketDataHandler.HandleGetTrade)
	mux.HandleFunc("/api/v1/market-data/stats/trades", h.marketDataHandler.HandleGetTradeStats)
	mux.HandleFunc("/api/v1/market-data/orderbook", h.marketDataHandler.HandleGetOrderBook)
	mux.HandleFunc("/api/v1/market-data/ticker", h.marketDataHandler.HandleGetTicker)
	mux.HandleFunc("/api/v1/market-data/summary", h.marketDataHandler.HandleGetMarketSummary)

	// Order management endpoints
	mux.HandleFunc("/api/v1/orders", h.orderHandler.HandleListOrders)
	mux.HandleFunc("/api/v1/orders/", h.handleOrderRoutes)
	mux.HandleFunc("/api/v1/orders/active", h.orderHandler.HandleGetActiveOrders)
	mux.HandleFunc("/api/v1/orders/history", h.orderHandler.HandleGetOrderHistory)

	// Position endpoints
	mux.HandleFunc("/api/v1/positions", h.orderHandler.HandleListPositions)
	mux.HandleFunc("/api/v1/positions/", h.handlePositionRoutes)
	mux.HandleFunc("/api/v1/positions/summary", h.orderHandler.HandleGetPositionSummary)

	// Strategy endpoints
	mux.HandleFunc("/api/v1/strategies", h.strategyHandler.HandleListStrategies)
	mux.HandleFunc("/api/v1/strategies/", h.handleStrategyRoutes)
	mux.HandleFunc("/api/v1/strategies/status", h.strategyHandler.HandleGetStatus)

	// Research cold-path (optional; requires RESEARCH_API_ENABLED)
	if h.researchHandler != nil {
		mux.HandleFunc("/api/v1/research/", h.researchHandler.HandleResearchRoutes)
	}

	// Aggregation endpoints
	mux.HandleFunc("/api/v1/dashboard", h.aggregationHandler.HandleGetDashboard)
	mux.HandleFunc("/api/v1/portfolio", h.aggregationHandler.HandleGetPortfolio)
	mux.HandleFunc("/api/v1/trading/overview", h.aggregationHandler.HandleGetTradingOverview)
	mux.HandleFunc("/api/v1/system/status", h.aggregationHandler.HandleGetSystemStatus)

	// Metrics endpoint
	mux.Handle("/metrics", h.metrics.Handler())

	h.logger.Info("API routes registered", nil)
}

// handleOrderRoutes handles order sub-routes
func (h *Handler) handleOrderRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Check for /cancel suffix
	if len(path) > 7 && path[len(path)-7:] == "/cancel" {
		h.orderHandler.HandleCancelOrder(w, r)
		return
	}

	// Otherwise, it's a get order by ID
	h.orderHandler.HandleGetOrder(w, r)
}

// handlePositionRoutes handles position sub-routes
func (h *Handler) handlePositionRoutes(w http.ResponseWriter, r *http.Request) {
	// All position sub-routes are GetPosition by book
	h.orderHandler.HandleGetPosition(w, r)
}

// handleStrategyRoutes handles strategy sub-routes
func (h *Handler) handleStrategyRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Check for /start suffix
	if len(path) > 6 && path[len(path)-6:] == "/start" {
		h.strategyHandler.HandleStartStrategy(w, r)
		return
	}

	// Check for /stop suffix
	if len(path) > 5 && path[len(path)-5:] == "/stop" {
		h.strategyHandler.HandleStopStrategy(w, r)
		return
	}

	// Check for /config suffix
	if len(path) > 7 && path[len(path)-7:] == "/config" {
		h.strategyHandler.HandleUpdateStrategyConfig(w, r)
		return
	}

	// Otherwise, it's a get strategy by name
	h.strategyHandler.HandleGetStrategy(w, r)
}
