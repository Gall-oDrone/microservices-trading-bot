package api

import (
	"fmt"
	"net/http"

	"bitso-trading-platform/backtesting/internal/models"
	"bitso-trading-platform/backtesting/internal/optimizer"
)

// CreateOptimizationRequest represents a request to create an optimization
type CreateOptimizationRequest struct {
	Name           string                               `json:"name"`
	Description    string                               `json:"description"`
	StartDate      string                               `json:"start_date"`
	EndDate        string                               `json:"end_date"`
	Book           string                               `json:"book"`
	InitialBalance float64                              `json:"initial_balance"`
	Strategy       string                               `json:"strategy"`
	Parameters     map[string]*optimizer.ParameterRange `json:"parameters"`
	Metric         string                               `json:"metric"`
	MaxWorkers     int                                  `json:"max_workers"`
	TopN           int                                  `json:"top_n"`
	SlippageModel  string                               `json:"slippage_model"`
	SlippageValue  float64                              `json:"slippage_value"`
	CommissionRate float64                              `json:"commission_rate"`
}

// CreateOptimization creates a new optimization
func (h *Handler) CreateOptimization(w http.ResponseWriter, r *http.Request) {
	var req CreateOptimizationRequest
	if err := ParseRequest(r, &req); err != nil {
		SendError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	// Parse dates
	startDate, err := parseDate(req.StartDate)
	if err != nil {
		SendError(w, http.StatusBadRequest, "INVALID_DATE", "Invalid start_date format")
		return
	}

	endDate, err := parseDate(req.EndDate)
	if err != nil {
		SendError(w, http.StatusBadRequest, "INVALID_DATE", "Invalid end_date format")
		return
	}

	// Create base backtest config
	baseConfig := models.NewBacktestConfig(req.Name, req.Book, startDate, endDate)
	baseConfig.Description = req.Description
	baseConfig.WithInitialBalance(req.InitialBalance)
	baseConfig.WithStrategy(req.Strategy, make(map[string]interface{}))

	if req.SlippageModel != "" {
		baseConfig.WithSlippage(req.SlippageModel, req.SlippageValue)
	}
	if req.CommissionRate > 0 {
		baseConfig.WithCommission(req.CommissionRate)
	}

	// Create optimization config
	optConfig := &optimizer.OptimizationConfig{
		Name:        req.Name,
		Description: req.Description,
		BaseConfig:  baseConfig,
		Parameters:  req.Parameters,
		Metric:      req.Metric,
		MaxWorkers:  req.MaxWorkers,
		TopN:        req.TopN,
	}

	// Start optimization
	opt, err := h.optimizer.Optimize(r.Context(), optConfig)
	if err != nil {
		SendError(w, http.StatusBadRequest, "OPTIMIZATION_FAILED", err.Error())
		return
	}

	// Return response
	SendJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         opt.ID,
		"status":     opt.Status,
		"total_runs": opt.TotalRuns,
		"created_at": opt.CreatedAt,
	})
}

// GetOptimization retrieves an optimization by ID
func (h *Handler) GetOptimization(w http.ResponseWriter, r *http.Request, optimizationID string) {
	opt, err := h.optimizer.GetOptimization(optimizationID)
	if err != nil {
		SendError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Optimization not found: %s", optimizationID))
		return
	}

	// Return simplified view or full view based on query param
	detailed := r.URL.Query().Get("detailed") == "true"

	if detailed {
		SendSuccess(w, opt)
	} else {
		// Return summary only
		SendSuccess(w, map[string]interface{}{
			"id":             opt.ID,
			"name":           opt.Name,
			"status":         opt.Status,
			"progress":       opt.Progress,
			"total_runs":     opt.TotalRuns,
			"completed_runs": opt.CompletedRuns,
			"created_at":     opt.CreatedAt,
			"started_at":     opt.StartedAt,
			"completed_at":   opt.CompletedAt,
		})
	}
}

// GetOptimizationResults retrieves optimization results
func (h *Handler) GetOptimizationResults(w http.ResponseWriter, r *http.Request, optimizationID string) {
	opt, err := h.optimizer.GetOptimization(optimizationID)
	if err != nil {
		SendError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Optimization not found: %s", optimizationID))
		return
	}

	if opt.Status != "completed" {
		SendError(w, http.StatusBadRequest, "NOT_COMPLETED", "Optimization is not completed yet")
		return
	}

	SendSuccess(w, map[string]interface{}{
		"results":     opt.Results,
		"best_result": opt.BestResult,
		"total":       len(opt.Results),
	})
}

// GetOptimizationBest retrieves the best result
func (h *Handler) GetOptimizationBest(w http.ResponseWriter, r *http.Request, optimizationID string) {
	opt, err := h.optimizer.GetOptimization(optimizationID)
	if err != nil {
		SendError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Optimization not found: %s", optimizationID))
		return
	}

	if opt.BestResult == nil {
		SendError(w, http.StatusNotFound, "NO_RESULTS", "No results available yet")
		return
	}

	SendSuccess(w, opt.BestResult)
}

// CancelOptimization cancels a running optimization
func (h *Handler) CancelOptimization(w http.ResponseWriter, r *http.Request, optimizationID string) {
	if err := h.optimizer.CancelOptimization(optimizationID); err != nil {
		SendError(w, http.StatusBadRequest, "CANCEL_FAILED", err.Error())
		return
	}

	SendSuccess(w, map[string]interface{}{
		"id":      optimizationID,
		"status":  "cancelled",
		"message": "Optimization cancelled successfully",
	})
}
