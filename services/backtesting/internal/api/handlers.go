package api

import (
	"net/http"
	"strings"

	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/manager"
	"bitso-trading-platform/backtesting/internal/metrics"
)

// Handler handles API requests
type Handler struct {
	manager          *manager.BacktestManager
	logger           logger.Logger
	metricsCollector *metrics.MetricsCollector
}

// NewHandler creates a new API handler
func NewHandler(
	manager *manager.BacktestManager,
	log logger.Logger,
	metricsCollector *metrics.MetricsCollector,
) *Handler {
	return &Handler{
		manager:          manager,
		logger:           log,
		metricsCollector: metricsCollector,
	}
}

// HandleBacktests handles /api/v1/backtests (GET, POST)
func (h *Handler) HandleBacktests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.CreateBacktest(w, r)
	case http.MethodGet:
		h.ListBacktests(w, r)
	default:
		SendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

// HandleBacktestByID handles /api/v1/backtests/{id}
func (h *Handler) HandleBacktestByID(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/backtests/")
	parts := strings.Split(path, "/")
	
	if len(parts) == 0 || parts[0] == "" {
		SendError(w, http.StatusBadRequest, "INVALID_REQUEST", "Backtest ID required")
		return
	}
	
	backtestID := parts[0]
	
	// Check for sub-resources
	if len(parts) > 1 {
		subResource := parts[1]
		switch subResource {
		case "results":
			h.GetBacktestResults(w, r, backtestID)
		case "summary":
			h.GetBacktestSummary(w, r, backtestID)
		case "trades":
			h.GetBacktestTrades(w, r, backtestID)
		case "report":
			h.DownloadBacktestReport(w, r, backtestID)
		case "cancel":
			if r.Method == http.MethodPost {
				h.CancelBacktest(w, r, backtestID)
			} else {
				SendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			}
		default:
			SendError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		}
		return
	}
	
	// Handle main resource
	switch r.Method {
	case http.MethodGet:
		h.GetBacktest(w, r, backtestID)
	case http.MethodDelete:
		h.DeleteBacktest(w, r, backtestID)
	default:
		SendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

// GetStatus returns service status
func (h *Handler) GetStatus(w http.ResponseWriter, r *http.Request) {
	stats := h.manager.GetStats()
	
	SendSuccess(w, map[string]interface{}{
		"service": "backtesting",
		"version": "1.0.0",
		"status":  "running",
		"stats":   stats,
	})
}

