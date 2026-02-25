package api

import (
	"fmt"
	"net/http"
	"time"

	"bitso-trading-platform/backtesting/internal/models"
	"bitso-trading-platform/backtesting/internal/storage"
)

// parseDate is a helper function to parse dates
func parseDate(dateStr string) (time.Time, error) {
	return time.Parse(time.RFC3339, dateStr)
}

// CreateBacktestRequest represents a request to create a backtest
type CreateBacktestRequest struct {
	Name            string                  `json:"name"`
	Description     string                  `json:"description"`
	StartDate       string                  `json:"start_date"`
	EndDate         string                  `json:"end_date"`
	Book            string                  `json:"book"`
	InitialBalance  float64                `json:"initial_balance"`
	Strategy        string                  `json:"strategy"`
	StrategyParams  map[string]interface{}  `json:"strategy_params"`
	SlippageModel   string                  `json:"slippage_model"`
	SlippageValue   float64                 `json:"slippage_value"`
	CommissionRate  float64                 `json:"commission_rate"`
	DataSource      string                  `json:"data_source"`
	DataGranularity string                  `json:"data_granularity"`
	SuccessCriteria *models.SuccessCriteria `json:"success_criteria,omitempty"`
}

// CreateBacktest creates a new backtest
func (h *Handler) CreateBacktest(w http.ResponseWriter, r *http.Request) {
	var req CreateBacktestRequest
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

	// Create backtest config
	config := models.NewBacktestConfig(req.Name, req.Book, startDate, endDate)
	config.Description = req.Description
	config.WithInitialBalance(req.InitialBalance)
	config.WithStrategy(req.Strategy, req.StrategyParams)

	if req.SlippageModel != "" {
		config.WithSlippage(req.SlippageModel, req.SlippageValue)
	}
	if req.CommissionRate > 0 {
		config.WithCommission(req.CommissionRate)
	}
	if req.DataSource != "" {
		config.DataSource = req.DataSource
	}
	if req.DataGranularity != "" {
		config.DataGranularity = req.DataGranularity
	}
	if req.SuccessCriteria != nil {
		config.SuccessCriteria = req.SuccessCriteria
	}

	// Create backtest
	backtest, err := h.manager.CreateBacktest(config)
	if err != nil {
		SendError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}

	// Start backtest
	if err := h.manager.StartBacktest(backtest.ID); err != nil {
		h.logger.Error("Failed to start backtest", map[string]interface{}{"error": err})
	}

	// Return response
	SendJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         backtest.ID,
		"status":     backtest.Status,
		"created_at": backtest.CreatedAt,
	})
}

// GetBacktest retrieves a backtest by ID
func (h *Handler) GetBacktest(w http.ResponseWriter, r *http.Request, backtestID string) {
	backtest, err := h.manager.GetBacktest(backtestID)
	if err != nil {
		SendError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Backtest not found: %s", backtestID))
		return
	}

	SendSuccess(w, map[string]interface{}{
		"id":           backtest.ID,
		"status":       backtest.Status,
		"progress":     backtest.Progress,
		"created_at":   backtest.CreatedAt,
		"started_at":   backtest.StartedAt,
		"completed_at": backtest.CompletedAt,
	})
}

// ListBacktests lists all backtests with optional filters
func (h *Handler) ListBacktests(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	query := r.URL.Query()

	filters := storage.NewListFilters()
	if status := query.Get("status"); status != "" {
		filters.WithStatus(status)
	}
	if strategy := query.Get("strategy"); strategy != "" {
		filters.WithStrategy(strategy)
	}
	if book := query.Get("book"); book != "" {
		filters.WithBook(book)
	}

	// Get backtests
	backtests, err := h.manager.ListBacktests(filters)
	if err != nil {
		SendError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	// Format response
	items := make([]map[string]interface{}, len(backtests))
	for i, bt := range backtests {
		items[i] = map[string]interface{}{
			"id":         bt.ID,
			"status":     bt.Status,
			"progress":   bt.Progress,
			"created_at": bt.CreatedAt,
		}
	}

	SendSuccess(w, map[string]interface{}{
		"backtests": items,
		"total":     len(items),
		"limit":     filters.Limit,
		"offset":    filters.Offset,
	})
}

// DeleteBacktest deletes a backtest
func (h *Handler) DeleteBacktest(w http.ResponseWriter, r *http.Request, backtestID string) {
	// Cancel if running
	h.manager.CancelBacktest(backtestID)

	SendSuccess(w, map[string]interface{}{
		"message": "Backtest deleted successfully",
	})
}

// CancelBacktest cancels a running backtest
func (h *Handler) CancelBacktest(w http.ResponseWriter, r *http.Request, backtestID string) {
	if err := h.manager.CancelBacktest(backtestID); err != nil {
		SendError(w, http.StatusBadRequest, "CANCEL_FAILED", err.Error())
		return
	}

	SendSuccess(w, map[string]interface{}{
		"id":      backtestID,
		"status":  "cancelled",
		"message": "Backtest cancelled successfully",
	})
}
