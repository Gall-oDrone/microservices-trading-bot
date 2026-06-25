package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/strategy-executor/internal/backtest"
	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// BacktestTradeSource supplies historical trades for backtests. It is satisfied
// by indicators.HTTPDataProvider (market-data GET /api/v1/trades).
type BacktestTradeSource interface {
	GetRecentTrades(ctx context.Context, book string, limit int) ([]indicators.Trade, error)
}

// BacktestRequest is the POST /api/v1/backtests body.
type BacktestRequest struct {
	Book           string                 `json:"book"`
	Strategy       string                 `json:"strategy"`        // strategy type (mean_reversion|momentum|limit_profit)
	StrategyName   string                 `json:"strategy_name"`   // optional display name
	Parameters     map[string]interface{} `json:"parameters"`      // optional strategy parameters
	StartDate      string                 `json:"start_date"`      // optional RFC3339; filters fetched trades
	EndDate        string                 `json:"end_date"`        // optional RFC3339; filters fetched trades
	InitialBalance float64                `json:"initial_balance"` // optional (default 100000)
	SlippageBPS    float64                `json:"slippage_bps"`    // optional
	CommissionBPS  float64                `json:"commission_bps"`  // optional
	Limit          int                    `json:"limit"`           // max recent trades to fetch (default 2000)
}

// BacktestJob tracks the lifecycle and result of a backtest.
type BacktestJob struct {
	ID          string                    `json:"id"`
	Status      string                    `json:"status"` // running|completed|failed
	Request     BacktestRequest           `json:"request"`
	Result      *backtest.BacktestResult  `json:"result,omitempty"`
	Error       string                    `json:"error,omitempty"`
	TradesUsed  int                       `json:"trades_used"`
	CreatedAt   time.Time                 `json:"created_at"`
	CompletedAt *time.Time                `json:"completed_at,omitempty"`
}

// BacktestHandler implements the /api/v1/backtests API over the backtest engine.
type BacktestHandler struct {
	source BacktestTradeSource
	mu     sync.RWMutex
	jobs   map[string]*BacktestJob
	seq    int
}

// NewBacktestHandler creates a handler backed by the given trade source.
func NewBacktestHandler(source BacktestTradeSource) *BacktestHandler {
	return &BacktestHandler{
		source: source,
		jobs:   make(map[string]*BacktestJob),
	}
}

// HandleBacktests handles POST (create+run) and GET (list) on /api/v1/backtests.
func (h *BacktestHandler) HandleBacktests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.create(w, r)
	case http.MethodGet:
		h.list(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleBacktest handles GET /api/v1/backtests/{id}[/results|/report].
func (h *BacktestHandler) HandleBacktest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/backtests/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Backtest ID required", http.StatusBadRequest)
		return
	}
	id := parts[0]

	job, ok := h.getJob(id)
	if !ok {
		http.Error(w, "Backtest not found", http.StatusNotFound)
		return
	}

	if len(parts) == 1 {
		writeJSON(w, http.StatusOK, job)
		return
	}

	switch parts[1] {
	case "results":
		if job.Result == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"status": job.Status, "error": job.Error})
			return
		}
		writeJSON(w, http.StatusOK, job.Result)
	case "report":
		if r.URL.Query().Get("format") == "text" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(renderTextReport(job)))
			return
		}
		writeJSON(w, http.StatusOK, job)
	default:
		http.Error(w, "Unknown sub-resource", http.StatusNotFound)
	}
}

func (h *BacktestHandler) create(w http.ResponseWriter, r *http.Request) {
	if h.source == nil {
		http.Error(w, "Backtest trade source not configured", http.StatusServiceUnavailable)
		return
	}
	var req BacktestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.Book == "" || req.Strategy == "" {
		http.Error(w, "book and strategy are required", http.StatusBadRequest)
		return
	}
	if req.Limit <= 0 {
		req.Limit = 2000
	}

	h.mu.Lock()
	h.seq++
	id := fmt.Sprintf("bt_%d_%d", time.Now().Unix(), h.seq)
	job := &BacktestJob{ID: id, Status: "running", Request: req, CreatedAt: time.Now()}
	h.jobs[id] = job
	h.mu.Unlock()

	// Run synchronously; backtests operate on a bounded trade window and complete
	// quickly. Status is recorded so poll-until-complete clients still work.
	h.run(r.Context(), job)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":          job.ID,
		"status":      job.Status,
		"trades_used": job.TradesUsed,
		"error":       job.Error,
	})
}

func (h *BacktestHandler) run(ctx context.Context, job *BacktestJob) {
	req := job.Request
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	trades, err := h.source.GetRecentTrades(fetchCtx, req.Book, req.Limit)
	cancel()
	if err != nil {
		h.finish(job, nil, 0, fmt.Errorf("fetch trades: %w", err))
		return
	}

	trades = filterByDateRange(trades, req.StartDate, req.EndDate)
	if len(trades) == 0 {
		h.finish(job, nil, 0, fmt.Errorf("no trades in range for book %s", req.Book))
		return
	}

	runCtx, cancel2 := context.WithTimeout(ctx, 120*time.Second)
	defer cancel2()
	result, err := backtest.RunHistorical(runCtx, trades, backtest.EngineConfig{
		Book:           req.Book,
		StrategyType:   req.Strategy,
		StrategyName:   req.StrategyName,
		Parameters:     req.Parameters,
		InitialBalance: req.InitialBalance,
		SlippageBPS:    req.SlippageBPS,
		CommissionBPS:  req.CommissionBPS,
	})
	if err != nil {
		h.finish(job, nil, len(trades), err)
		return
	}
	h.finish(job, result, len(trades), nil)
}

func (h *BacktestHandler) finish(job *BacktestJob, result *backtest.BacktestResult, tradesUsed int, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	job.CompletedAt = &now
	job.TradesUsed = tradesUsed
	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
		return
	}
	job.Status = "completed"
	job.Result = result
}

func (h *BacktestHandler) list(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	out := make([]*BacktestJob, 0, len(h.jobs))
	for _, j := range h.jobs {
		out = append(out, j)
	}
	h.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{"backtests": out, "count": len(out)})
}

func (h *BacktestHandler) getJob(id string) (*BacktestJob, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	j, ok := h.jobs[id]
	return j, ok
}

// filterByDateRange keeps trades whose timestamp falls within [start, end] when
// either bound is a valid RFC3339 string. Invalid/empty bounds are ignored.
func filterByDateRange(trades []indicators.Trade, startStr, endStr string) []indicators.Trade {
	var start, end time.Time
	if startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = t
		}
	}
	if endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = t
		}
	}
	if start.IsZero() && end.IsZero() {
		return trades
	}
	out := make([]indicators.Trade, 0, len(trades))
	for _, tr := range trades {
		if !start.IsZero() && tr.Timestamp.Before(start) {
			continue
		}
		if !end.IsZero() && tr.Timestamp.After(end) {
			continue
		}
		out = append(out, tr)
	}
	return out
}

func renderTextReport(job *BacktestJob) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Backtest %s\n", job.ID)
	fmt.Fprintf(&sb, "Status:        %s\n", job.Status)
	fmt.Fprintf(&sb, "Strategy:      %s (%s)\n", job.Request.StrategyName, job.Request.Strategy)
	fmt.Fprintf(&sb, "Book:          %s\n", job.Request.Book)
	fmt.Fprintf(&sb, "Trades used:   %d\n", job.TradesUsed)
	if job.Error != "" {
		fmt.Fprintf(&sb, "Error:         %s\n", job.Error)
	}
	if job.Result != nil {
		r := job.Result
		fmt.Fprintf(&sb, "----------------------------------------\n")
		fmt.Fprintf(&sb, "Total trades:  %d (win %d / loss %d)\n", r.TotalTrades, r.WinningTrades, r.LosingTrades)
		fmt.Fprintf(&sb, "Win rate:      %.2f%%\n", r.WinRate)
		fmt.Fprintf(&sb, "Profit factor: %.2f\n", r.ProfitFactor)
		fmt.Fprintf(&sb, "Total P&L:     %.2f\n", r.TotalPnL)
		fmt.Fprintf(&sb, "Max drawdown:  %.2f%%\n", r.MaxDrawdownPct)
		fmt.Fprintf(&sb, "Sharpe ratio:  %.2f\n", r.SharpeRatio)
		fmt.Fprintf(&sb, "Sortino ratio: %.2f\n", r.SortinoRatio)
		fmt.Fprintf(&sb, "Avg hold:      %s\n", r.AverageHoldTime)
		fmt.Fprintf(&sb, "Ticks:         %d\n", r.TicksProcessed)
	}
	return sb.String()
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
