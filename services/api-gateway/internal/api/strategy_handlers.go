package api

import (
	"encoding/json"
	"io"
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

// HandleStartStrategy handles POST /api/v1/strategies/{name}/start
func (h *StrategyHandler) HandleStartStrategy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Extract strategy name from path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/strategies/")
	name := strings.TrimSuffix(path, "/start")

	if name == "" || name == path {
		BadRequestResponse(w, r, "Strategy name is required")
		return
	}

	h.logger.Info("Starting strategy", map[string]interface{}{
		"strategy": name,
	})

	// Call client
	err := h.client.StartStrategy(r.Context(), name)
	if err != nil {
		h.logger.Error("Failed to start strategy", map[string]interface{}{
			"error":    err.Error(),
			"strategy": name,
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, map[string]interface{}{
		"message":  "Strategy started successfully",
		"strategy": name,
	})
}

// HandleStopStrategy handles POST /api/v1/strategies/{name}/stop
func (h *StrategyHandler) HandleStopStrategy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Extract strategy name from path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/strategies/")
	name := strings.TrimSuffix(path, "/stop")

	if name == "" || name == path {
		BadRequestResponse(w, r, "Strategy name is required")
		return
	}

	h.logger.Info("Stopping strategy", map[string]interface{}{
		"strategy": name,
	})

	// Call client
	err := h.client.StopStrategy(r.Context(), name)
	if err != nil {
		h.logger.Error("Failed to stop strategy", map[string]interface{}{
			"error":    err.Error(),
			"strategy": name,
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, map[string]interface{}{
		"message":  "Strategy stopped successfully",
		"strategy": name,
	})
}

// HandleUpdateStrategyConfig handles PUT /api/v1/strategies/{name}/config
func (h *StrategyHandler) HandleUpdateStrategyConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Extract strategy name from path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/strategies/")
	name := strings.TrimSuffix(path, "/config")

	if name == "" || name == path {
		BadRequestResponse(w, r, "Strategy name is required")
		return
	}

	// Parse request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		BadRequestResponse(w, r, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	var configUpdate map[string]interface{}
	if err := json.Unmarshal(body, &configUpdate); err != nil {
		BadRequestResponse(w, r, "Invalid JSON in request body")
		return
	}

	// Extract config field
	config, ok := configUpdate["config"].(map[string]interface{})
	if !ok {
		BadRequestResponse(w, r, "Config field is required")
		return
	}

	h.logger.Info("Updating strategy config", map[string]interface{}{
		"strategy": name,
	})

	// Call client
	err = h.client.UpdateStrategyConfig(r.Context(), name, config)
	if err != nil {
		h.logger.Error("Failed to update strategy config", map[string]interface{}{
			"error":    err.Error(),
			"strategy": name,
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, map[string]interface{}{
		"message":  "Strategy configuration updated successfully",
		"strategy": name,
	})
}
