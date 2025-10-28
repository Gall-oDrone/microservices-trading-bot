package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"bitso-trading-platform/backtesting/internal/api"
	"bitso-trading-platform/backtesting/internal/config"
	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/metrics"
	"bitso-trading-platform/shared/pkg/health"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// HTTPServer handles HTTP requests
type HTTPServer struct {
	config           *config.ServiceConfig
	server           *http.Server
	handler          *api.Handler
	healthManager    *health.HealthManager
	metricsCollector *metrics.MetricsCollector
	logger           logger.Logger
}

// NewHTTPServer creates a new HTTP server
func NewHTTPServer(
	cfg *config.ServiceConfig,
	handler *api.Handler,
	healthManager *health.HealthManager,
	metricsCollector *metrics.MetricsCollector,
	log logger.Logger,
) *HTTPServer {
	return &HTTPServer{
		config:           cfg,
		handler:          handler,
		healthManager:    healthManager,
		metricsCollector: metricsCollector,
		logger:           log,
	}
}

// Start starts the HTTP server
func (s *HTTPServer) Start(ctx context.Context) error {
	// Set up router
	mux := http.NewServeMux()

	// Register routes
	s.registerRoutes(mux)

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      s.wrapWithMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.logger.Info("Starting HTTP server", map[string]interface{}{
		"address": addr,
	})

	// Start server
	errChan := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Check for immediate errors
	select {
	case err := <-errChan:
		return fmt.Errorf("failed to start server: %w", err)
	case <-time.After(100 * time.Millisecond):
		s.logger.Info("HTTP server started successfully", nil)
		return nil
	}
}

// Stop gracefully stops the HTTP server
func (s *HTTPServer) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	s.logger.Info("Stopping HTTP server", nil)

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	s.logger.Info("HTTP server stopped", nil)
	return nil
}

// registerRoutes registers all HTTP routes
func (s *HTTPServer) registerRoutes(mux *http.ServeMux) {
	// Health endpoints
	mux.HandleFunc("/health", s.healthManager.HTTPHandler())
	mux.HandleFunc("/health/live", s.healthManager.LivenessHandler())
	mux.HandleFunc("/health/ready", s.healthManager.ReadinessHandler())

	// Metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	// API endpoints
	mux.HandleFunc("/api/v1/backtests", s.handler.HandleBacktests)
	mux.HandleFunc("/api/v1/backtests/", s.handler.HandleBacktestByID)
	mux.HandleFunc("/api/v1/optimizations", s.handler.HandleOptimizations)
	mux.HandleFunc("/api/v1/optimizations/", s.handler.HandleOptimizationByID)
	
	s.logger.Info("Routes registered", map[string]interface{}{
		"endpoints": []string{
			"GET /health",
			"GET /health/live",
			"GET /health/ready",
			"GET /metrics",
			"POST /api/v1/backtests",
			"GET /api/v1/backtests",
			"GET /api/v1/backtests/{id}",
			"POST /api/v1/optimizations",
			"GET /api/v1/optimizations/{id}",
		},
	})
}

// wrapWithMiddleware wraps the handler with middleware
func (s *HTTPServer) wrapWithMiddleware(handler http.Handler) http.Handler {
	// Apply middleware in reverse order (last wraps first)
	wrapped := handler

	// Recovery middleware (outermost)
	wrapped = recoveryMiddleware(s.logger)(wrapped)

	// Metrics middleware
	if s.metricsCollector != nil {
		wrapped = metricsMiddleware(s.metricsCollector)(wrapped)
	}

	// Logging middleware
	wrapped = loggingMiddleware(s.logger)(wrapped)

	// CORS middleware
	wrapped = corsMiddleware()(wrapped)

	return wrapped
}
