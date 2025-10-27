package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"bitso-trading-platform/strategy-executor/internal/health"
	"bitso-trading-platform/strategy-executor/internal/metrics"
)

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
	Health  *HealthHandler
	Metrics *MetricsHandler
	API     *APIHandler
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
	// Add service dependencies here
}

// Config holds server configuration
type Config struct {
	Host string
	Port int
}

// New creates a new HTTP server
func New(config *Config, healthMgr *health.Manager, metrics *metrics.Metrics) *Server {
	handlers := &Handlers{
		Health:  &HealthHandler{healthMgr: healthMgr},
		Metrics: &MetricsHandler{metrics: metrics},
		API:     &APIHandler{},
	}

	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc("/health", handlers.Health.Health)
	mux.HandleFunc("/health/ready", handlers.Health.Ready)
	mux.HandleFunc("/health/live", handlers.Health.Live)

	// Metrics endpoint
	mux.HandleFunc("/metrics", handlers.Metrics.Metrics)

	// API endpoints
	mux.HandleFunc("/api/v1/status", handlers.API.Status)
	mux.HandleFunc("/api/v1/strategies", handlers.API.Strategies)
	mux.HandleFunc("/api/v1/strategies/", handlers.API.StrategyHandler)

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

	// This would typically use prometheus.Handler() to serve metrics
	// For now, return a simple response
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("# Metrics endpoint - Prometheus metrics would be served here\n"))
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
