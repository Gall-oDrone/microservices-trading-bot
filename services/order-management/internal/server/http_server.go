package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"bitso-trading-platform/order-management/internal/config"
	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/risk"
	"bitso-trading-platform/order-management/internal/validator"
	"bitso-trading-platform/shared/pkg/health"
	sharedModels "bitso-trading-platform/shared/pkg/models"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// DevOrderFillPublisher invokes the same Kafka publish path as a production fill (for smoke tests).
type DevOrderFillPublisher func(context.Context, *sharedModels.OrderFillEvent) error

// OrderValidationRequest represents a pre-trade validation request
type OrderValidationRequest struct {
	Book      string  `json:"book"`
	Side      string  `json:"side"`
	Type      string  `json:"type"`
	Amount    float64 `json:"amount"`
	Price     float64 `json:"price"`
	SignalID  string  `json:"signal_id,omitempty"`
	Strategy  string  `json:"strategy,omitempty"`
}

// OrderValidationResponse represents the validation result
type OrderValidationResponse struct {
	Valid       bool     `json:"valid"`
	Approved    bool     `json:"approved"`
	Errors      []string `json:"errors,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	ValidatedAt string   `json:"validated_at"`
	// IdempotentReplay is true when the canonical order for this signal_id was already created
	// by the trading.signals consumer and economics match — risk was already applied at ingest.
	IdempotentReplay bool `json:"idempotent_replay,omitempty"`
}

// HTTPServer handles HTTP requests
type HTTPServer struct {
	logger            *logger.Logger
	config            *config.ServiceConfig
	healthManager     *health.HealthManager
	metrics           *metrics.MetricsCollector
	sessionAggregator *metrics.IntradayAggregator // optional: for GET /api/v1/risk/session
	validator         validator.OrderValidator    // optional: for POST /api/v1/orders/validate
	riskManager       risk.RiskManager            // optional: for POST /api/v1/orders/validate
	devPublishOrderFill DevOrderFillPublisher     // optional: POST /internal/v1/dev/publish-order-fill-test
	server              *http.Server
	router              *http.ServeMux
}

// HTTPServerOptions contains optional dependencies for HTTPServer
type HTTPServerOptions struct {
	SessionAggregator *metrics.IntradayAggregator
	Validator         validator.OrderValidator
	RiskManager       risk.RiskManager
	// DevPublishOrderFill enables POST /internal/v1/dev/publish-order-fill-test when non-nil (see OM_DEV_ORDER_FILL_PUBLISH_TEST_ENABLED).
	DevPublishOrderFill DevOrderFillPublisher
}

// NewHTTPServer creates a new HTTP server. Options can be nil; individual fields enable specific endpoints.
func NewHTTPServer(
	config *config.ServiceConfig,
	healthManager *health.HealthManager,
	metrics *metrics.MetricsCollector,
	logger *logger.Logger,
	sessionAggregator *metrics.IntradayAggregator,
) *HTTPServer {
	return NewHTTPServerWithOptions(config, healthManager, metrics, logger, &HTTPServerOptions{
		SessionAggregator: sessionAggregator,
	})
}

// NewHTTPServerWithOptions creates a new HTTP server with full options.
func NewHTTPServerWithOptions(
	config *config.ServiceConfig,
	healthManager *health.HealthManager,
	metricsCollector *metrics.MetricsCollector,
	logger *logger.Logger,
	opts *HTTPServerOptions,
) *HTTPServer {
	router := http.NewServeMux()

	server := &HTTPServer{
		logger:        logger,
		config:        config,
		healthManager: healthManager,
		metrics:       metricsCollector,
		router:        router,
		server: &http.Server{
			Addr:         fmt.Sprintf("%s:%d", config.Host, config.Port),
			Handler:      router,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}

	if opts != nil {
		server.sessionAggregator = opts.SessionAggregator
		server.validator = opts.Validator
		server.riskManager = opts.RiskManager
		server.devPublishOrderFill = opts.DevPublishOrderFill
	}

	server.setupRoutes()
	return server
}

// Start starts the HTTP server
func (s *HTTPServer) Start(ctx context.Context) error {
	// Start server in goroutine
	go func() {
		s.logger.Infof("HTTP server listening on %s", s.server.Addr)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Errorf("HTTP server error: %v", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	return nil
}

// Stop stops the HTTP server gracefully
func (s *HTTPServer) Stop(ctx context.Context) error {
	s.logger.Info("Stopping HTTP server...", nil)
	return s.server.Shutdown(ctx)
}

// setupRoutes configures all HTTP routes
func (s *HTTPServer) setupRoutes() {
	// Health check endpoints
	s.router.HandleFunc("/health", s.withMetrics(s.healthCheckHandler))
	s.router.HandleFunc("/health/live", s.withMetrics(s.livenessHandler))
	s.router.HandleFunc("/health/ready", s.withMetrics(s.readinessHandler))

	// Metrics endpoint
	s.router.Handle("/metrics", promhttp.Handler())

	// Status endpoint
	s.router.HandleFunc("/api/v1/status", s.withMetrics(s.statusHandler))

	// Session risk (for trading-engine daily loss / drawdown limits)
	if s.sessionAggregator != nil {
		s.router.HandleFunc("/api/v1/risk/session", s.withMetrics(s.riskSessionHandler))
	}

	// Pre-trade validation endpoint (for trading-engine to validate before placing orders)
	if s.validator != nil && s.riskManager != nil {
		s.router.HandleFunc("/api/v1/orders/validate", s.withMetrics(s.validateOrderHandler))
	}

	// Dev-only: smoke-test Kafka order-fill publish + kafka_order_fill_published log (OM_DEV_ORDER_FILL_PUBLISH_TEST_ENABLED)
	if s.config.DevOrderFillPublishTestEnabled && s.devPublishOrderFill != nil {
		s.router.HandleFunc("/internal/v1/dev/publish-order-fill-test", s.withMetrics(s.devPublishOrderFillTestHandler))
		s.logger.Info("HTTP dev route enabled", map[string]interface{}{"path": "/internal/v1/dev/publish-order-fill-test"})
	}

	s.logger.Info("HTTP routes configured", nil)
}

// devPublishOrderFillTestHandler POST body: OrderFillEvent JSON (event_id, order_id, book, side required).
func (s *HTTPServer) devPublishOrderFillTestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var ev sharedModels.OrderFillEvent
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}
	if ev.EventID == "" || ev.OrderID == "" || strings.TrimSpace(ev.Book) == "" || strings.TrimSpace(ev.Side) == "" {
		s.respondError(w, http.StatusBadRequest, "event_id, order_id, book, and side are required")
		return
	}
	if ev.TimestampMs == 0 {
		ev.TimestampMs = time.Now().UnixMilli()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := s.devPublishOrderFill(ctx, &ev); err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"event_id": ev.EventID,
		"order_id": ev.OrderID,
	})
}

func (s *HTTPServer) riskSessionHandler(w http.ResponseWriter, r *http.Request) {
	s.metrics.RecordSessionRiskRequest()
	if r.Method != http.MethodGet {
		s.metrics.RecordSessionRiskRequestError()
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	dailyRealizedPnL, drawdownPct := s.sessionAggregator.SessionSnapshot()
	response := map[string]interface{}{
		"daily_realized_pnl": dailyRealizedPnL,
		"drawdown_percent":   drawdownPct,
	}
	s.respondJSON(w, http.StatusOK, response)
}

// validateOrderHandler handles pre-trade order validation requests.
// Trading-engine calls this endpoint BEFORE placing orders on Bitso to ensure
// the order passes all validation and risk checks.
func (s *HTTPServer) validateOrderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req OrderValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}
	// Order validation uses lowercase buy/sell; TradeSignalEvent validation expects BUY/SELL/HOLD.
	incomingSide := strings.TrimSpace(req.Side)
	req.Side = strings.ToLower(incomingSide)
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	response := OrderValidationResponse{
		Valid:       true,
		Approved:    true,
		ValidatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Step 1: Validate signal fields
	signal := &sharedModels.TradeSignalEvent{
		EventID:   req.SignalID,
		Book:      req.Book,
		Signal:    strings.ToUpper(incomingSide),
		Price:     req.Price,
		Amount:    req.Amount,
		Strategy:  req.Strategy,
		Timestamp: time.Now().UnixMilli(),
	}

	if err := s.validator.ValidateSignal(signal); err != nil {
		response.Valid = false
		response.Approved = false
		response.Errors = append(response.Errors, fmt.Sprintf("Signal validation failed: %v", err))
	}

	// Step 2: Create temporary order for risk checks
	tempOrder := &models.Order{
		ID:       fmt.Sprintf("validate-%d", time.Now().UnixNano()),
		Book:     req.Book,
		Side:     req.Side,
		Type:     req.Type,
		Amount:   req.Amount,
		Price:    req.Price,
		SignalID: req.SignalID,
		Strategy: req.Strategy,
		Status:   models.OrderStatusPending,
	}

	// Step 3: Pre-trade validation (idempotent when trading.signals consumer created the row first)
	var idempotent bool
	if response.Valid {
		var err error
		idempotent, err = s.validator.PreTradeValidateOrder(tempOrder)
		if err != nil {
			response.Valid = false
			response.Approved = false
			response.Errors = append(response.Errors, fmt.Sprintf("Order validation failed: %v", err))
		} else if idempotent {
			response.IdempotentReplay = true
		}
	}

	// Step 4: Perform risk checks (only if basic validation passed, and not idempotent replay)
	if response.Valid && !idempotent {
		if err := s.riskManager.CheckRisk(ctx, tempOrder); err != nil {
			response.Approved = false
			response.Errors = append(response.Errors, fmt.Sprintf("Risk check failed: %v", err))
		}
	}

	// Log validation result
	s.logger.Infof("Order validation: book=%s side=%s amount=%.8f price=%.8f valid=%v approved=%v errors=%v",
		req.Book, req.Side, req.Amount, req.Price, response.Valid, response.Approved, response.Errors)

	// Return appropriate status code
	statusCode := http.StatusOK
	if !response.Approved {
		statusCode = http.StatusUnprocessableEntity
	}

	s.respondJSON(w, statusCode, response)
}

// Health check handlers

func (s *HTTPServer) healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	checks := s.healthManager.Check(ctx)
	overallStatus := s.healthManager.GetOverallStatus(ctx)

	response := map[string]interface{}{
		"status":    overallStatus,
		"timestamp": time.Now().UTC(),
		"checks":    checks,
		"version":   s.config.Version,
		"service":   s.config.Name,
	}

	statusCode := http.StatusOK
	if overallStatus == health.StatusUnhealthy {
		statusCode = http.StatusServiceUnavailable
	}

	s.respondJSON(w, statusCode, response)
}

func (s *HTTPServer) livenessHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	response := map[string]interface{}{
		"status":    "alive",
		"timestamp": time.Now().UTC(),
	}

	s.respondJSON(w, http.StatusOK, response)
}

func (s *HTTPServer) readinessHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	overallStatus := s.healthManager.GetOverallStatus(ctx)

	response := map[string]interface{}{
		"status":    overallStatus,
		"timestamp": time.Now().UTC(),
	}

	statusCode := http.StatusOK
	if overallStatus != health.StatusHealthy {
		statusCode = http.StatusServiceUnavailable
	}

	s.respondJSON(w, statusCode, response)
}

func (s *HTTPServer) statusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	response := map[string]interface{}{
		"service":     s.config.Name,
		"version":     s.config.Version,
		"environment": s.config.Environment,
		"status":      "running",
		"timestamp":   time.Now().UTC(),
	}

	s.respondJSON(w, http.StatusOK, response)
}

// Helper methods

func (s *HTTPServer) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Errorf("Failed to encode JSON response: %v", err)
	}
}

func (s *HTTPServer) respondError(w http.ResponseWriter, status int, message string) {
	response := map[string]interface{}{
		"error":     message,
		"status":    status,
		"timestamp": time.Now().UTC(),
	}
	s.respondJSON(w, status, response)
}

// withMetrics wraps a handler with metrics collection
func (s *HTTPServer) withMetrics(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Track in-flight requests
		s.metrics.IncHTTPInFlight(r.Method, r.URL.Path)
		defer s.metrics.DecHTTPInFlight(r.Method, r.URL.Path)

		// Create a response writer wrapper to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Call the actual handler
		handler(rw, r)

		// Record metrics
		duration := time.Since(start)
		s.metrics.RecordHTTPRequest(r.Method, r.URL.Path, fmt.Sprintf("%d", rw.statusCode))
		s.metrics.RecordHTTPDuration(r.Method, r.URL.Path, duration)

		// Log the request
		s.logger.Infof("HTTP %s %s - %d (%v)", r.Method, r.URL.Path, rw.statusCode, duration)
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
