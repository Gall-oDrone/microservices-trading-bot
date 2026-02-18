package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"bitso-trading-platform/order-management/internal/config"
	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/shared/pkg/health"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// HTTPServer handles HTTP requests
type HTTPServer struct {
	logger           *logger.Logger
	config           *config.ServiceConfig
	healthManager    *health.HealthManager
	metrics          *metrics.MetricsCollector
	sessionAggregator *metrics.IntradayAggregator // optional: for GET /api/v1/risk/session
	server           *http.Server
	router           *http.ServeMux
}

// NewHTTPServer creates a new HTTP server. sessionAggregator can be nil; if set, GET /api/v1/risk/session is enabled.
func NewHTTPServer(
	config *config.ServiceConfig,
	healthManager *health.HealthManager,
	metrics *metrics.MetricsCollector,
	logger *logger.Logger,
	sessionAggregator *metrics.IntradayAggregator,
) *HTTPServer {
	router := http.NewServeMux()

	server := &HTTPServer{
		logger:            logger,
		config:            config,
		healthManager:     healthManager,
		metrics:           metrics,
		sessionAggregator:  sessionAggregator,
		router:             router,
		server: &http.Server{
			Addr:         fmt.Sprintf("%s:%d", config.Host, config.Port),
			Handler:      router,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
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

	s.logger.Info("HTTP routes configured", nil)
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
