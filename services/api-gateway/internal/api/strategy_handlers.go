package api

import (
	"net/http"
	"strings"

	"bitso-trading-platform/api-gateway/internal/client"
	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
)

// StrategyHandler handles strategy executor related requests
type StrategyHandler struct {
	client  client.StrategyExecutorClient
	logger  *logger.Logger
	metrics *metrics.MetricsCollector
}

// NewStrategyHandler creates a new strategy handler
func NewStrategyHandler(
	client client.StrategyExecutorClient,
	logger *logger.Logger,
	metrics *metrics.MetricsCollector,
) *StrategyHandler {
	return &StrategyHandler{
		client:  client,
		logger:  logger.WithComponent("strategy-handler"),
		metrics: metrics,
	}
}

// HandleGetStatus handles GET /api/v1/strategies/status
func (h *StrategyHandler) HandleGetStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	h.logger.Debug("Getting service status", nil)

	// Call client
	status, err := h.client.GetStatus(r.Context())
	if err != nil {
		h.logger.Error("Failed to get service status", map[string]interface{}{
			"error": err.Error(),
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, status)
}

// HandleListStrategies handles GET /api/v1/strategies
func (h *StrategyHandler) HandleListStrategies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	h.logger.Debug("Listing strategies", nil)

	// Call client
	strategies, err := h.client.ListStrategies(r.Context())
	if err != nil {
		h.logger.Error("Failed to list strategies", map[string]interface{}{
			"error": err.Error(),
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, strategies)
}

// HandleGetStrategy handles GET /api/v1/strategies/{name}
func (h *StrategyHandler) HandleGetStrategy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Extract strategy name from path
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/strategies/")
	if name == "" {
		BadRequestResponse(w, r, "Strategy name is required")
		return
	}

	h.logger.Debug("Getting strategy", map[string]interface{}{
		"strategy": name,
	})

	// Call client
	strategy, err := h.client.GetStrategy(r.Context(), name)
	if err != nil {
		h.logger.Error("Failed to get strategy", map[string]interface{}{
			"error":    err.Error(),
			"strategy": name,
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, strategy)
}
