package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"bitso-trading-platform/api-gateway/internal/client"
	"bitso-trading-platform/api-gateway/internal/config"
	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
	"bitso-trading-platform/api-gateway/internal/research"
	"bitso-trading-platform/shared/pkg/kafka"
)

// ResearchHandler exposes cold-path research memo operator APIs.
type ResearchHandler struct {
	cfg           *config.Config
	logger        *logger.Logger
	metrics       *metrics.MetricsCollector
	memoStore     *research.MemoStore
	researchAgent client.ResearchAgentClient
	signalProducer *kafka.Producer
}

// NewResearchHandler wires research dependencies. Partial config disables specific routes.
func NewResearchHandler(
	cfg *config.Config,
	log *logger.Logger,
	metrics *metrics.MetricsCollector,
	memoStore *research.MemoStore,
	researchAgent client.ResearchAgentClient,
	signalProducer *kafka.Producer,
) *ResearchHandler {
	return &ResearchHandler{
		cfg:            cfg,
		logger:         log.WithComponent("research-handler"),
		metrics:        metrics,
		memoStore:      memoStore,
		researchAgent:  researchAgent,
		signalProducer: signalProducer,
	}
}

// HandleResearchRoutes dispatches /api/v1/research/* subpaths.
func (h *ResearchHandler) HandleResearchRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/research/")
	path = strings.Trim(path, "/")

	switch {
	case path == "run" && r.Method == http.MethodPost:
		h.HandleRunResearch(w, r)
	case path == "memos" && r.Method == http.MethodGet:
		h.HandleListMemos(w, r)
	case strings.HasPrefix(path, "memos/") && strings.HasSuffix(path, "/approve") && r.Method == http.MethodPost:
		runID := strings.TrimSuffix(strings.TrimPrefix(path, "memos/"), "/approve")
		runID = strings.Trim(runID, "/")
		h.HandleApproveMemo(w, r, runID)
	case strings.HasPrefix(path, "memos/") && r.Method == http.MethodGet:
		runID := strings.TrimPrefix(path, "memos/")
		h.HandleGetMemo(w, r, runID)
	default:
		NotFoundResponse(w, r, "Research route not found")
	}
}

type researchRunRequest struct {
	Ticker             string `json:"ticker"`
	TradeDate          string `json:"trade_date"`
	IncludeNewsContext bool   `json:"include_news_context"`
}

// HandleRunResearch proxies POST /api/v1/research/run to research-agent.
func (h *ResearchHandler) HandleRunResearch(w http.ResponseWriter, r *http.Request) {
	if h.researchAgent == nil {
		ErrorResponse(w, r, http.StatusServiceUnavailable, "RESEARCH_DISABLED", "research-agent URL is not configured")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		BadRequestResponse(w, r, "invalid request body")
		return
	}
	var req researchRunRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			BadRequestResponse(w, r, "invalid JSON body")
			return
		}
	}
	if req.Ticker == "" || req.TradeDate == "" {
		BadRequestResponse(w, r, "ticker and trade_date are required")
		return
	}

	memo, err := h.researchAgent.RunResearch(r.Context(), req.Ticker, req.TradeDate, req.IncludeNewsContext)
	if err != nil {
		h.logger.Error("research run failed", map[string]interface{}{"error": err.Error()})
		InternalErrorResponse(w, r, err)
		return
	}
	SuccessResponse(w, r, memo)
}

// HandleListMemos handles GET /api/v1/research/memos.
func (h *ResearchHandler) HandleListMemos(w http.ResponseWriter, r *http.Request) {
	if h.memoStore == nil {
		ErrorResponse(w, r, http.StatusServiceUnavailable, "RESEARCH_S3_DISABLED", "research S3 store is not configured")
		return
	}
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	memos, err := h.memoStore.List(r.Context(), limit)
	if err != nil {
		InternalErrorResponse(w, r, err)
		return
	}
	SuccessResponse(w, r, memos)
}

// HandleGetMemo handles GET /api/v1/research/memos/{run_id}.
func (h *ResearchHandler) HandleGetMemo(w http.ResponseWriter, r *http.Request, runID string) {
	if h.memoStore == nil {
		ErrorResponse(w, r, http.StatusServiceUnavailable, "RESEARCH_S3_DISABLED", "research S3 store is not configured")
		return
	}
	memo, err := h.memoStore.Get(r.Context(), runID)
	if err != nil {
		NotFoundResponse(w, r, "Research memo not found")
		return
	}
	SuccessResponse(w, r, memo)
}

// HandleApproveMemo handles POST /api/v1/research/memos/{run_id}/approve (P3 bridge).
func (h *ResearchHandler) HandleApproveMemo(w http.ResponseWriter, r *http.Request, runID string) {
	if h.memoStore == nil {
		ErrorResponse(w, r, http.StatusServiceUnavailable, "RESEARCH_S3_DISABLED", "research S3 store is not configured")
		return
	}
	if h.signalProducer == nil {
		ErrorResponse(w, r, http.StatusServiceUnavailable, "KAFKA_DISABLED", "Kafka producer for trading.signals is not configured")
		return
	}

	var req research.ApproveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		BadRequestResponse(w, r, "invalid JSON body")
		return
	}

	memo, err := h.memoStore.Get(r.Context(), runID)
	if err != nil {
		NotFoundResponse(w, r, "Research memo not found")
		return
	}

	evt, result, err := research.BuildTradeSignalEvent(memo, req)
	if err != nil {
		BadRequestResponse(w, r, err.Error())
		return
	}

	payload, err := json.Marshal(evt)
	if err != nil {
		InternalErrorResponse(w, r, err)
		return
	}
	if err := h.signalProducer.Produce(r.Context(), []byte(evt.EventID), payload); err != nil {
		h.logger.Error("failed to publish approved research signal", map[string]interface{}{
			"error":    err.Error(),
			"audit_id": result.AuditID,
		})
		InternalErrorResponse(w, r, err)
		return
	}

	approvalRecord := map[string]interface{}{
		"audit_id":        result.AuditID,
		"research_run_id": result.ResearchRunID,
		"operator_id":     req.OperatorID,
		"signal_event_id": result.SignalEventID,
		"signal":          result.Signal,
		"approved_at_ms":  evt.Timestamp,
	}
	if err := h.memoStore.PutApproval(r.Context(), result.AuditID, approvalRecord); err != nil {
		h.logger.Warn("approval published but audit persist failed", map[string]interface{}{
			"error": err.Error(),
		})
	}

	h.logger.Info("research memo approved and signal published", map[string]interface{}{
		"audit_id":        result.AuditID,
		"research_run_id": result.ResearchRunID,
		"signal":          result.Signal,
	})
	SuccessResponseWithStatus(w, r, http.StatusCreated, result)
}
