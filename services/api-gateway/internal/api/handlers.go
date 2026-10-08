package api

import (
	"net/http"
	"regexp"
	"strings"
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
	strategyHandler    *StrategyHandler
	aggregationHandler *AggregationHandler
}

// NewHandler creates a new main API handler
func NewHandler(
	cfg *config.Config,
	logger *logger.Logger,
	metrics *metrics.MetricsCollector,
	healthManager *health.HealthManager,
	marketDataHandler *MarketDataHandler,
	strategyHandler *StrategyHandler,
	aggregationHandler *AggregationHandler,
) *Handler {
	return &Handler{
		config:             cfg,
		logger:             logger.WithComponent("api-handler"),
		metrics:            metrics,
		healthManager:      healthManager,
		marketDataHandler:  marketDataHandler,
		strategyHandler:    strategyHandler,
		aggregationHandler: aggregationHandler,
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

	// Order and position routes were removed 2026-10-08 (plan §7 items 3-4):
	// order-management serves none of them (every call was a 404), and the
	// order cancel route must not be public. Positions and exposure are read
	// through ui-api; the aggregation endpoints below still try
	// order-management and leave those fields empty when it has no data.

	// Strategy endpoints: read-only. Start/stop are operator controls and go
	// through ui-api (token, confirmation, audit log, plan §8.13), never
	// through this public gateway.
	mux.HandleFunc("/api/v1/strategies", h.strategyHandler.HandleListStrategies)
	mux.HandleFunc("/api/v1/strategies/", h.handleStrategyRoutes)
	mux.HandleFunc("/api/v1/strategies/status", h.strategyHandler.HandleGetStatus)

	// Aggregation endpoints
	mux.HandleFunc("/api/v1/dashboard", h.aggregationHandler.HandleGetDashboard)
	mux.HandleFunc("/api/v1/portfolio", h.aggregationHandler.HandleGetPortfolio)
	mux.HandleFunc("/api/v1/trading/overview", h.aggregationHandler.HandleGetTradingOverview)
	mux.HandleFunc("/api/v1/system/status", h.aggregationHandler.HandleGetSystemStatus)

	// Metrics endpoint
	mux.Handle("/metrics", h.metrics.Handler())

	h.logger.Info("API routes registered", nil)
}

// strategyNameRe is a strategy-executor strategy name.
var strategyNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// reservedStrategyPaths are strategy-executor endpoints under
// /api/v1/strategies/ that are not strategy names. process publishes
// signals and order-fill feeds fills; neither may be reachable from here.
var reservedStrategyPaths = map[string]bool{
	"process": true, "order-fill": true, "types": true, "stats": true,
}

// handleStrategyRoutes serves GET /api/v1/strategies/{name} only. Any
// sub-path (start, stop, config, ...) is 404 and any other method is 405.
func (h *Handler) handleStrategyRoutes(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/strategies/")
	if strings.Contains(name, "/") || reservedStrategyPaths[name] || !strategyNameRe.MatchString(name) {
		NotFoundResponse(w, r, "Route not found")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		MethodNotAllowedResponse(w, r)
		return
	}
	h.strategyHandler.HandleGetStrategy(w, r)
}
