package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"bitso-trading-platform/api-gateway/internal/config"
	"bitso-trading-platform/api-gateway/internal/logger"
)

// HTTPServer represents an HTTP server
type HTTPServer struct {
	config  *config.Config
	server  *http.Server
	logger  *logger.Logger
	handler http.Handler
}

// NewHTTPServer creates a new HTTP server
func NewHTTPServer(cfg *config.Config, handler http.Handler, logger *logger.Logger) *HTTPServer {
	addr := fmt.Sprintf("%s:%d", cfg.Service.Host, cfg.Service.Port)

	server := &http.Server{
		Addr:    addr,
		Handler: handler,
		// Timeouts
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	// Configure TLS if enabled
	if cfg.TLS.Enabled {
		tlsConfig := &tls.Config{
			MinVersion:               tls.VersionTLS12,
			PreferServerCipherSuites: true,
			CurvePreferences: []tls.CurveID{
				tls.CurveP256,
				tls.X25519,
			},
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			},
		}
		server.TLSConfig = tlsConfig
	}

	return &HTTPServer{
		config:  cfg,
		server:  server,
		logger:  logger,
		handler: handler,
	}
}

// Start starts the HTTP server
func (s *HTTPServer) Start(ctx context.Context) error {
	// Channel to capture server errors
	errChan := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		s.logger.Info("Starting HTTP server", map[string]interface{}{
			"address": s.server.Addr,
			"tls":     s.config.TLS.Enabled,
		})

		var err error
		if s.config.TLS.Enabled {
			// Start HTTPS server
			err = s.server.ListenAndServeTLS(s.config.TLS.CertFile, s.config.TLS.KeyFile)
		} else {
			// Start HTTP server
			err = s.server.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP server error", map[string]interface{}{
				"error": err.Error(),
			})
			errChan <- err
		}
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		s.logger.Info("HTTP server context cancelled", nil)
		return nil
	case err := <-errChan:
		return fmt.Errorf("HTTP server failed: %w", err)
	}
}

// Stop gracefully stops the HTTP server
func (s *HTTPServer) Stop(ctx context.Context) error {
	s.logger.Info("Stopping HTTP server", map[string]interface{}{
		"address": s.server.Addr,
	})

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := s.server.Shutdown(shutdownCtx); err != nil {
		s.logger.Error("Error during HTTP server shutdown", map[string]interface{}{
			"error": err.Error(),
		})
		return fmt.Errorf("failed to shutdown HTTP server: %w", err)
	}

	s.logger.Info("HTTP server stopped successfully", nil)
	return nil
}

// GetAddr returns the server address
func (s *HTTPServer) GetAddr() string {
	return s.server.Addr
}

// IsRunning returns true if the server is running
func (s *HTTPServer) IsRunning() bool {
	return s.server != nil
}

