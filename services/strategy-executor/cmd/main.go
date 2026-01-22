package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bitso-trading-platform/strategy-executor/internal/config"
	"bitso-trading-platform/strategy-executor/internal/health"
	"bitso-trading-platform/strategy-executor/internal/logger"
	"bitso-trading-platform/strategy-executor/internal/metrics"
	"bitso-trading-platform/strategy-executor/internal/server"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	appLogger := logger.New(&logger.Config{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
		Output: cfg.Logging.Output,
	})
	logger.SetGlobalLogger(appLogger)

	appLogger.Info("Strategy Executor Service starting...")
	appLogger.Infof("Service: %s v%s", cfg.Service.Name, cfg.Service.Version)
	appLogger.Infof("Environment: %s", cfg.Service.Environment)

	// Initialize metrics
	appMetrics := metrics.New(cfg.Service.Name)
	appMetrics.RecordServiceStart()

	// Initialize health manager
	healthMgr := health.NewManager(cfg.Service.Name, cfg.Service.Version, 30*time.Second)

	// Add basic health checks
	healthMgr.RegisterCheck(health.NewSimpleCheck("service", func(ctx context.Context) error {
		// Basic service health check
		return nil
	}))

	// Initialize HTTP server
	httpServer := server.New(&server.Config{
		Host: cfg.Service.Host,
		Port: cfg.Service.Port,
	}, healthMgr, appMetrics)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start HTTP server in goroutine
	go func() {
		appLogger.Infof("Starting HTTP server on %s:%d", cfg.Service.Host, cfg.Service.Port)
		if err := httpServer.Start(ctx); err != nil {
			appLogger.Errorf("HTTP server error: %v", err)
		}
	}()

	// Start uptime tracking
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		startTime := time.Now()
		for {
			select {
			case <-ticker.C:
				uptime := time.Since(startTime)
				appMetrics.RecordServiceUptime(uptime)
				appMetrics.RecordServiceHealth(true)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	appLogger.Info("Service started successfully. Waiting for interrupt signal...")
	<-sigChan

	appLogger.Info("Shutdown signal received. Gracefully shutting down...")

	// Cancel context to stop all goroutines
	cancel()

	// Shutdown HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := httpServer.Stop(shutdownCtx); err != nil {
		appLogger.Errorf("Error shutting down HTTP server: %v", err)
	}

	appLogger.Info("Service shutdown complete")
}
