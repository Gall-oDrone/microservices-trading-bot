package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"bitso-trading-platform/strategy-executor/internal/health"
	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/metrics"
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

// TradeSignalPublisher publishes strategy signals to Kafka (optional; set from main when producer exists).
type TradeSignalPublisher func(ctx context.Context, book string, signals []*strategies.Signal) error

// Server represents the HTTP server
type Server struct {
	server    *http.Server
	healthMgr *health.Manager
	metrics   *metrics.Metrics
	handlers  *Handlers
	startTime time.Time
}

// Handlers holds all HTTP handlers
type Handlers struct {
	Health     *HealthHandler
	Metrics    *MetricsHandler
	API        *APIHandler
	Indicators *IndicatorHandler
	Strategies *StrategyHandler
	Backtests  *BacktestHandler
}

// HealthHandler handles health check endpoints
type HealthHandler struct {
	healthMgr *health.Manager
}

// MetricsHandler handles metrics endpoints
type MetricsHandler struct {
	metrics *metrics.Metrics
}

// APIHandler handles API endpoints
type APIHandler struct {
	registry *strategies.EnhancedRegistry
}

// IndicatorHandler handles indicator API endpoints
type IndicatorHandler struct {
	service *indicators.Service
}

// StrategyHandler handles strategy management endpoints
type StrategyHandler struct {
	registry            *strategies.EnhancedRegistry
	publishTradeSignals TradeSignalPublisher
}

// Config holds server configuration
type Config struct {
	Host string
	Port int
}

// ServerOptions holds optional dependencies for the server
type ServerOptions struct {
	IndicatorService *indicators.Service
	StrategyRegistry *strategies.EnhancedRegistry
	// PublishTradeSignals publishes signals to Kafka (trading.signals) so trading-engine can execute orders.
	PublishTradeSignals TradeSignalPublisher
	// BacktestTradeSource supplies historical trades for the /api/v1/backtests API (optional).
	BacktestTradeSource BacktestTradeSource
}

// New creates a new HTTP server
func New(config *Config, healthMgr *health.Manager, metrics *metrics.Metrics) *Server {
	return NewWithOptions(config, healthMgr, metrics, nil)
}

// NewWithOptions creates a new HTTP server with optional dependencies
func NewWithOptions(config *Config, healthMgr *health.Manager, metrics *metrics.Metrics, opts *ServerOptions) *Server {
	var registry *strategies.EnhancedRegistry
	if opts != nil && opts.StrategyRegistry != nil {
		registry = opts.StrategyRegistry
	}

	handlers := &Handlers{
		Health:  &HealthHandler{healthMgr: healthMgr},
		Metrics: &MetricsHandler{metrics: metrics},
		API:     &APIHandler{registry: registry},
	}

	if opts != nil && opts.IndicatorService != nil {
		handlers.Indicators = &IndicatorHandler{service: opts.IndicatorService}
	}

	if opts != nil && opts.BacktestTradeSource != nil {
		handlers.Backtests = NewBacktestHandler(opts.BacktestTradeSource)
	}

	if registry != nil {
		sh := &StrategyHandler{registry: registry}
		if opts != nil && opts.PublishTradeSignals != nil {
			sh.publishTradeSignals = opts.PublishTradeSignals
		}
		handlers.Strategies = sh
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", handlers.Health.Health)
	mux.HandleFunc("/health/ready", handlers.Health.Ready)
	mux.HandleFunc("/health/live", handlers.Health.Live)

	mux.HandleFunc("/metrics", handlers.Metrics.Metrics)

	mux.HandleFunc("/api/v1/status", handlers.API.Status)

	if handlers.Strategies != nil {
		mux.HandleFunc("/api/v1/strategies", handlers.Strategies.HandleStrategies)
		mux.HandleFunc("/api/v1/strategies/order-fill", handlers.Strategies.HandleOrderFill)
		mux.HandleFunc("/api/v1/strategies/", handlers.Strategies.HandleStrategy)
		mux.HandleFunc("/api/v1/strategies/types", handlers.Strategies.GetAvailableTypes)
		mux.HandleFunc("/api/v1/strategies/stats", handlers.Strategies.GetStats)
		mux.HandleFunc("/api/v1/strategies/process", handlers.Strategies.ProcessTick)
		mux.HandleFunc("/api/v1/test/signals", handlers.Strategies.GenerateTestSignals)
	} else {
		mux.HandleFunc("/api/v1/strategies", handlers.API.Strategies)
		mux.HandleFunc("/api/v1/strategies/", handlers.API.StrategyHandler)
	}

	if handlers.Indicators != nil {
		mux.HandleFunc("/api/v1/indicators/", handlers.Indicators.HandleIndicators)
	}

	if handlers.Backtests != nil {
		mux.HandleFunc("/api/v1/backtests", handlers.Backtests.HandleBacktests)
		mux.HandleFunc("/api/v1/backtests/", handlers.Backtests.HandleBacktest)
	}

	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.Host, config.Port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return &Server{
		server:    server,
		healthMgr: healthMgr,
		metrics:   metrics,
		handlers:  handlers,
		startTime: time.Now(),
	}
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	s.metrics.RecordServiceStart()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.server.Shutdown(shutdownCtx)
	}()

	return s.server.ListenAndServe()
}

// Stop stops the HTTP server
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Health endpoint handlers
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	health := h.healthMgr.GetHealth(r.Context())

	w.Header().Set("Content-Type", "application/json")

	statusCode := http.StatusOK
	if health.IsUnhealthy() {
		statusCode = http.StatusServiceUnavailable
	} else if health.IsDegraded() {
		statusCode = http.StatusOK // Still OK but degraded
	}

	w.WriteHeader(statusCode)

	if data, err := health.ToJSON(); err != nil {
		http.Error(w, "Failed to marshal health", http.StatusInternalServerError)
	} else {
		w.Write(data)
	}
}

func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	health := h.healthMgr.GetHealth(r.Context())

	if health.IsHealthy() || health.IsDegraded() {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Not Ready"))
	}
}

func (h *HealthHandler) Live(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Liveness check - always return OK if the service is running
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// Metrics endpoint handler
func (h *MetricsHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	promMetrics := metrics.GetPrometheusMetrics()
	promMetrics.Handler()(w, r)
}

// API endpoint handlers
func (h *APIHandler) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_ = map[string]interface{}{
		"service": "strategy-executor",
		"status":  "running",
		"time":    time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Simple JSON response (in production, use json.Marshal)
	response := fmt.Sprintf(`{"service":"strategy-executor","status":"running","time":"%s"}`, time.Now().UTC().Format(time.RFC3339))
	w.Write([]byte(response))
}

func (h *APIHandler) Strategies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getStrategies(w, r)
	case http.MethodPost:
		h.createStrategy(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) StrategyHandler(w http.ResponseWriter, r *http.Request) {
	// Extract strategy name from URL path
	// This is a simplified implementation
	strategyName := r.URL.Path[len("/api/v1/strategies/"):]

	switch r.Method {
	case http.MethodGet:
		h.getStrategy(w, r, strategyName)
	case http.MethodPut:
		h.updateStrategy(w, r, strategyName)
	case http.MethodDelete:
		h.deleteStrategy(w, r, strategyName)
	case http.MethodPost:
		// Check if it's a start/stop action
		if r.URL.Path[len("/api/v1/strategies/"+strategyName):] == "/start" {
			h.startStrategy(w, r, strategyName)
		} else if r.URL.Path[len("/api/v1/strategies/"+strategyName):] == "/stop" {
			h.stopStrategy(w, r, strategyName)
		} else {
			http.Error(w, "Not found", http.StatusNotFound)
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) getStrategies(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement get strategies
	_ = []map[string]interface{}{
		{
			"name":   "basic",
			"status": "active",
			"book":   "btc_mxn",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Simple JSON response
	response := `[{"name":"basic","status":"active","book":"btc_mxn"}]`
	w.Write([]byte(response))
}

func (h *APIHandler) createStrategy(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement create strategy
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte("Not implemented"))
}

func (h *APIHandler) getStrategy(w http.ResponseWriter, r *http.Request, name string) {
	// TODO: Implement get strategy
	_ = map[string]interface{}{
		"name":   name,
		"status": "active",
		"book":   "btc_mxn",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := fmt.Sprintf(`{"name":"%s","status":"active","book":"btc_mxn"}`, name)
	w.Write([]byte(response))
}

func (h *APIHandler) updateStrategy(w http.ResponseWriter, r *http.Request, name string) {
	// TODO: Implement update strategy
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte("Not implemented"))
}

func (h *APIHandler) deleteStrategy(w http.ResponseWriter, r *http.Request, name string) {
	// TODO: Implement delete strategy
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte("Not implemented"))
}

func (h *APIHandler) startStrategy(w http.ResponseWriter, r *http.Request, name string) {
	// TODO: Implement start strategy
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte("Not implemented"))
}

func (h *APIHandler) stopStrategy(w http.ResponseWriter, r *http.Request, name string) {
	// TODO: Implement stop strategy
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte("Not implemented"))
}

// Indicator endpoint handlers

// HandleIndicators routes indicator requests
// GET /api/v1/indicators/{book} - Get all indicators for a book
// GET /api/v1/indicators/{book}/{indicator}?period=20 - Get specific indicator
// GET /api/v1/indicators/{book}/snapshot - Get all indicators at once
func (h *IndicatorHandler) HandleIndicators(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/indicators/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Book is required", http.StatusBadRequest)
		return
	}

	book := parts[0]

	if len(parts) == 1 {
		h.getAllIndicators(w, r, book)
		return
	}

	indicator := parts[1]

	if indicator == "snapshot" {
		h.getSnapshot(w, r, book)
		return
	}

	h.getIndicator(w, r, book, indicator)
}

// getAllIndicators returns all indicators for a book
func (h *IndicatorHandler) getAllIndicators(w http.ResponseWriter, r *http.Request, book string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	indicators, err := h.service.GetAllIndicators(ctx, book)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get indicators: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(indicators)
}

// getIndicator returns a specific indicator for a book
func (h *IndicatorHandler) getIndicator(w http.ResponseWriter, r *http.Request, book, indicator string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var result interface{}
	var err error

	switch indicator {
	case "sma":
		result, err = h.service.GetSMA(ctx, book)
	case "ema":
		result, err = h.service.GetEMA(ctx, book)
	case "rsi":
		result, err = h.service.GetRSI(ctx, book)
	case "bollinger":
		result, err = h.service.GetBollinger(ctx, book)
	case "atr":
		result, err = h.service.GetATR(ctx, book)
	case "vwap":
		result, err = h.service.GetVWAP(ctx, book)
	default:
		http.Error(w, fmt.Sprintf("Unknown indicator: %s", indicator), http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get indicator: %v", err), http.StatusInternalServerError)
		return
	}

	if result == nil {
		http.Error(w, "Indicator not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// getSnapshot returns all indicators for a book in a single response
func (h *IndicatorHandler) getSnapshot(w http.ResponseWriter, r *http.Request, book string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	snapshot, err := h.service.GetSnapshot(ctx, book)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get snapshot: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snapshot)
}

// HandleStrategies handles list and create strategy operations
func (h *StrategyHandler) HandleStrategies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listStrategies(w, r)
	case http.MethodPost:
		h.createStrategy(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleOrderFill reports an exchange fill for strategies that wait for fills (e.g. limit_profit BUY).
// Body JSON: event_id, book, side, average_price, filled_amount; optional liquidity (maker|taker), buy_fee_rate.
func (h *StrategyHandler) HandleOrderFill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var fill strategies.OrderFill
	if err := json.NewDecoder(r.Body).Decode(&fill); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	if fill.EventID == "" || fill.Book == "" || fill.Side == "" {
		http.Error(w, "event_id, book, and side are required", http.StatusBadRequest)
		return
	}
	h.registry.NotifyOrderFilled(fill)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleStrategy handles single strategy operations
func (h *StrategyHandler) HandleStrategy(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/strategies/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Strategy name required", http.StatusBadRequest)
		return
	}

	name := parts[0]

	if len(parts) > 1 {
		action := parts[1]
		switch action {
		case "start":
			h.startStrategy(w, r, name)
		case "stop":
			h.stopStrategy(w, r, name)
		case "state":
			h.getStrategyState(w, r, name)
		case "metrics":
			h.getStrategyMetrics(w, r, name)
		default:
			http.Error(w, "Unknown action", http.StatusNotFound)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getStrategy(w, r, name)
	case http.MethodDelete:
		h.deleteStrategy(w, r, name)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listStrategies returns all registered strategies
func (h *StrategyHandler) listStrategies(w http.ResponseWriter, r *http.Request) {
	infos := h.registry.GetAllStrategyInfo()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"strategies": infos,
		"count":      len(infos),
	})
}

// createStrategy creates and registers a new strategy
func (h *StrategyHandler) createStrategy(w http.ResponseWriter, r *http.Request) {
	var config strategies.StrategyConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if config.Name == "" || config.Type == "" || config.Book == "" {
		http.Error(w, "Name, type, and book are required", http.StatusBadRequest)
		return
	}

	strategy, err := h.registry.CreateAndRegister(config)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create strategy: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":    strategy.Name(),
		"version": strategy.Version(),
		"status":  "created",
	})
}

// getStrategy returns strategy info
func (h *StrategyHandler) getStrategy(w http.ResponseWriter, r *http.Request, name string) {
	info, err := h.registry.GetStrategyInfo(name)
	if err != nil {
		http.Error(w, fmt.Sprintf("Strategy not found: %v", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// deleteStrategy removes a strategy
func (h *StrategyHandler) deleteStrategy(w http.ResponseWriter, r *http.Request, name string) {
	if err := h.registry.Remove(name); err != nil {
		http.Error(w, fmt.Sprintf("Failed to remove strategy: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// startStrategy starts a strategy
func (h *StrategyHandler) startStrategy(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := h.registry.Start(ctx, name); err != nil {
		http.Error(w, fmt.Sprintf("Failed to start strategy: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":   name,
		"status": "started",
	})
}

// stopStrategy stops a strategy
func (h *StrategyHandler) stopStrategy(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.registry.Stop(name); err != nil {
		http.Error(w, fmt.Sprintf("Failed to stop strategy: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":   name,
		"status": "stopped",
	})
}

// getStrategyState returns the current state of a strategy
func (h *StrategyHandler) getStrategyState(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	strategy, err := h.registry.Get(name)
	if err != nil {
		http.Error(w, fmt.Sprintf("Strategy not found: %v", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(strategy.GetState())
}

// getStrategyMetrics returns the metrics of a strategy
func (h *StrategyHandler) getStrategyMetrics(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	strategy, err := h.registry.Get(name)
	if err != nil {
		http.Error(w, fmt.Sprintf("Strategy not found: %v", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(strategy.GetMetrics())
}

// GetAvailableTypes returns available strategy types
func (h *StrategyHandler) GetAvailableTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	types := h.registry.GetAvailableTypes()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"types": types,
	})
}

// GetStats returns registry statistics
func (h *StrategyHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.registry.GetStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// ProcessTick processes a market tick through all running strategies
func (h *StrategyHandler) ProcessTick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Book   string  `json:"book"`
		Price  float64 `json:"price"`
		Amount float64 `json:"amount"`
		Side   string  `json:"side"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Book == "" {
		req.Book = "btc_mxn"
	}

	tick := &indicators.Trade{
		Timestamp: time.Now(),
		Price:     req.Price,
		Amount:    req.Amount,
		Side:      req.Side,
	}

	signals, err := h.registry.ProcessTick(tick, req.Book)
	if err != nil {
		http.Error(w, fmt.Sprintf("Process tick failed: %v", err), http.StatusInternalServerError)
		return
	}

	var kafkaErr string
	if len(signals) > 0 && h.publishTradeSignals != nil {
		pubCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		kafkaErrVal := h.publishTradeSignals(pubCtx, req.Book, signals)
		cancel()
		if kafkaErrVal != nil {
			kafkaErr = kafkaErrVal.Error()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"signals_generated": len(signals),
		"signals":           signals,
		"tick":              tick,
	}
	if kafkaErr != "" {
		resp["kafka_publish_error"] = kafkaErr
	} else if len(signals) > 0 && h.publishTradeSignals != nil {
		resp["kafka_published"] = true
	}
	json.NewEncoder(w).Encode(resp)
}

// GenerateTestSignals generates test signals for dashboard verification
func (h *StrategyHandler) GenerateTestSignals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Count    int    `json:"count"`
		Strategy string `json:"strategy"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Count = 5
		req.Strategy = "mean_reversion"
	}

	if req.Count <= 0 {
		req.Count = 5
	}
	if req.Count > 100 {
		req.Count = 100
	}
	if req.Strategy == "" {
		req.Strategy = "mean_reversion"
	}

	sides := []string{"buy", "sell"}
	generated := 0

	for i := 0; i < req.Count; i++ {
		side := sides[i%2]
		h.registry.UpdateMetricsForSignal(req.Strategy, side)
		generated++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"generated": generated,
		"strategy":  req.Strategy,
		"message":   "Test signals generated for dashboard verification",
	})
}
