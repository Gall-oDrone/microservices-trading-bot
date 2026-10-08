package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"bitso-trading-platform/market-data/internal/api"
	"bitso-trading-platform/shared/pkg/httpcors"
)

// HTTPServer handles HTTP server operations
type HTTPServer struct {
	server *http.Server
	logger *log.Logger
}

// NewHTTPServer creates a new HTTP server. If metricsHandler is non-nil, /metrics is registered for Prometheus scraping.
func NewHTTPServer(port string, handler *api.Handler, logger *log.Logger, metricsHandler http.Handler) *HTTPServer {
	if logger == nil {
		logger = log.New(log.Writer(), "[HTTP-SERVER] ", log.LstdFlags|log.Lshortfile)
	}

	mux := http.NewServeMux()
	if metricsHandler != nil {
		mux.Handle("/metrics", metricsHandler)
	}
	handler.RegisterRoutes(mux)

	// CORS: explicit allowlist (CORS_ALLOWED_ORIGINS); unset or invalid = none.
	cors, err := httpcors.FromEnv(os.Getenv)
	if err != nil {
		logger.Printf("CORS disabled: %v", err)
		cors = nil
	} else if len(cors.Origins()) > 0 {
		logger.Printf("CORS allowed origins: %v", cors.Origins())
	}

	// Add middleware
	handlerWithMiddleware := addMiddleware(mux, logger, cors)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      handlerWithMiddleware,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return &HTTPServer{
		server: server,
		logger: logger,
	}
}

// Start starts the HTTP server
func (s *HTTPServer) Start(ctx context.Context) error {
	s.logger.Printf("Starting HTTP server on port %s", s.server.Addr)

	// Start server in a goroutine
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Printf("HTTP server error: %v", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	return nil
}

// Stop gracefully stops the HTTP server
func (s *HTTPServer) Stop() error {
	s.logger.Println("Stopping HTTP server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown HTTP server: %w", err)
	}

	s.logger.Println("HTTP server stopped")
	return nil
}

// addMiddleware adds common middleware to the HTTP handler
func addMiddleware(handler http.Handler, logger *log.Logger, cors *httpcors.Policy) http.Handler {
	return cors.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Add request logging
		logger.Printf("%s %s %s", r.Method, r.URL.Path, r.RemoteAddr)

		// Add security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Call the next handler
		handler.ServeHTTP(wrapped, r)

		// Log response
		duration := time.Since(start)
		logger.Printf("%s %s %d %v", r.Method, r.URL.Path, wrapped.statusCode, duration)
	}))
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
