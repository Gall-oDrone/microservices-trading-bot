package api

import (
	"fmt"
	"net/http"
	"strconv"

	"bitso-trading-platform/backtesting/internal/analyzer"
)

// GetBacktestResults retrieves complete backtest results
func (h *Handler) GetBacktestResults(w http.ResponseWriter, r *http.Request, backtestID string) {
	result, err := h.manager.GetBacktestResult(backtestID)
	if err != nil {
		SendError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Results not found: %s", backtestID))
		return
	}

	SendSuccess(w, result)
}

// GetBacktestSummary retrieves performance summary only
func (h *Handler) GetBacktestSummary(w http.ResponseWriter, r *http.Request, backtestID string) {
	result, err := h.manager.GetBacktestResult(backtestID)
	if err != nil {
		SendError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Results not found: %s", backtestID))
		return
	}

	if result.Summary == nil {
		SendError(w, http.StatusNotFound, "NO_SUMMARY", "Summary not available yet")
		return
	}

	SendSuccess(w, result.Summary)
}

// GetBacktestTrades retrieves trade history
func (h *Handler) GetBacktestTrades(w http.ResponseWriter, r *http.Request, backtestID string) {
	result, err := h.manager.GetBacktestResult(backtestID)
	if err != nil {
		SendError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Results not found: %s", backtestID))
		return
	}

	// Parse pagination parameters
	query := r.URL.Query()
	limit := 100
	offset := 0

	if limitStr := query.Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	if offsetStr := query.Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			offset = o
		}
	}

	// Apply pagination
	trades := result.Trades
	total := len(trades)

	if offset > total {
		offset = total
	}

	end := offset + limit
	if end > total {
		end = total
	}

	paginatedTrades := trades[offset:end]

	SendSuccess(w, map[string]interface{}{
		"trades": paginatedTrades,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// DownloadBacktestReport downloads a backtest report
func (h *Handler) DownloadBacktestReport(w http.ResponseWriter, r *http.Request, backtestID string) {
	result, err := h.manager.GetBacktestResult(backtestID)
	if err != nil {
		SendError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Results not found: %s", backtestID))
		return
	}

	// Get format from query parameter
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=backtest-%s.json", backtestID))
		SendJSON(w, http.StatusOK, result)

	case "text":
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=backtest-%s.txt", backtestID))
		w.Write([]byte(analyzer.GenerateTextReport(result)))

	case "html":
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=backtest-%s.html", backtestID))
		w.Write([]byte(analyzer.GenerateHTMLReport(result)))

	default:
		SendError(w, http.StatusBadRequest, "INVALID_FORMAT", "Invalid format (use: json, text, html)")
	}
}
