package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bitso-trading-platform/backtesting/internal/api"
	"bitso-trading-platform/backtesting/internal/config"
	"bitso-trading-platform/backtesting/internal/data"
	"bitso-trading-platform/backtesting/internal/engine"
	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/manager"
	"bitso-trading-platform/backtesting/internal/metrics"
	"bitso-trading-platform/backtesting/internal/optimizer"
	"bitso-trading-platform/backtesting/internal/server"
	"bitso-trading-platform/backtesting/internal/storage"
	"bitso-trading-platform/shared/pkg/health"

	"github.com/redis/go-redis/v9"
)

const (
	appName         = "backtesting"
	appVersion      = "1.0.0"
	shutdownTimeout = 30 * time.Second
)

// Application encapsulates all application components
type Application struct {
	logger *logger.ZerologLogger
	config *config.Config

	// Core components
	healthManager    *health.HealthManager
	metricsCollector *metrics.MetricsCollector

	// Data layer
	redisClient   *redis.Client
	dataProvider  data.DataProvider
	resultStorage storage.ResultStorage

	// Business logic
	backtestEngine  engine.BacktestEngine
	backtestManager *manager.BacktestManager
	optimizer       optimizer.Optimizer

	// API
	apiHandler *api.Handler
	httpServer *server.HTTPServer

	// Context
	ctx    context.Context
	cancel context.CancelFunc
}

// NewApplication creates and initializes the application
func NewApplication() (*Application, error) {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize logger
	appLogger := logger.New(&logger.Config{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
		Output: cfg.Logging.Output,
	})
	appLogger.Info("Backtesting Service starting...", map[string]interface{}{
		"version":     appVersion,
		"name":        appName,
		"environment": cfg.Service.Environment,
		"port":        cfg.Service.Port,
	})

	// Create context
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize metrics collector
	metricsCollector := metrics.NewMetricsCollector(appName)
	appLogger.Info("Metrics collector initialized", nil)

	// Initialize health manager
	healthManager := health.NewHealthManager(log.New(os.Stdout, "[HEALTH] ", log.LstdFlags))
	appLogger.Info("Health manager initialized", nil)

	// Add basic health check
	healthManager.AddChecker(health.NewSimpleHealthChecker("service", func(ctx context.Context) error {
		// Basic service health check
		return nil
	}))

	// Initialize Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
		PoolSize: cfg.Redis.PoolSize,
	})
	appLogger.Info("Redis client initialized", nil)

	// Add Redis health check
	healthManager.AddChecker(health.NewSimpleHealthChecker("redis", func(ctx context.Context) error {
		return redisClient.Ping(ctx).Err()
	}))

	// Add market-data service health check
	healthManager.AddChecker(health.NewHTTPHealthChecker(
		"market-data",
		cfg.MarketData.BaseURL+"/health",
		cfg.MarketData.Timeout,
	))

	// Initialize data cache
	cache := data.NewCache(redisClient, time.Hour, appLogger)
	appLogger.Info("Data cache initialized", nil)

	// Initialize data provider
	dataProvider := data.NewMarketDataProvider(
		cfg.MarketData.BaseURL,
		cache,
		appLogger,
		cfg.MarketData.RetryCount,
		cfg.MarketData.RetryDelay,
	)
	appLogger.Info("Data provider initialized", nil)

	// Initialize result storage
	ttl := time.Hour * 24 * time.Duration(cfg.Storage.RetentionDays)
	var resultStorage storage.ResultStorage
	if cfg.Storage.Type == "redis" {
		resultStorage = storage.NewRedisStorage(redisClient, appLogger, ttl)
	} else {
		resultStorage = storage.NewFileStorage(cfg.Storage.Path, appLogger)
	}
	appLogger.Info("Result storage initialized", map[string]interface{}{
		"type": cfg.Storage.Type,
	})

	// Initialize backtest engine
	backtestEngine := engine.NewEngine(dataProvider, resultStorage, appLogger, metricsCollector)
	appLogger.Info("Backtest engine initialized", nil)

	// Initialize backtest manager
	backtestManager := manager.NewBacktestManager(
		backtestEngine,
		resultStorage,
		cfg.Execution.MaxConcurrentBacktests,
		appLogger,
		metricsCollector,
	)
	appLogger.Info("Backtest manager initialized", nil)

	// Initialize optimizer
	opt := optimizer.NewOptimizer(backtestEngine, resultStorage, appLogger)
	appLogger.Info("Optimizer initialized", nil)

	// Initialize API handlers
	apiHandler := api.NewHandler(backtestManager, opt, appLogger, metricsCollector)
	appLogger.Info("API handler initialized", nil)

	// Initialize HTTP server
	httpServer := server.NewHTTPServer(
		&cfg.Service,
		apiHandler,
		healthManager,
		metricsCollector,
		appLogger,
	)
	appLogger.Info("HTTP server initialized", nil)

	appLogger.Info("Configuration loaded successfully", map[string]interface{}{
		"service_port": cfg.Service.Port,
		"environment":  cfg.Service.Environment,
	})

	return &Application{
		logger:           appLogger,
		config:           cfg,
		healthManager:    healthManager,
		metricsCollector: metricsCollector,
		redisClient:      redisClient,
		dataProvider:     dataProvider,
		resultStorage:    resultStorage,
		backtestEngine:   backtestEngine,
		backtestManager:  backtestManager,
		optimizer:        opt,
		apiHandler:       apiHandler,
		httpServer:       httpServer,
		ctx:              ctx,
		cancel:           cancel,
	}, nil
}

// Start initializes and starts all components
func (app *Application) Start() error {
	app.logger.Info("Starting application components...", nil)

	// Start metrics collection
	go app.startMetricsCollection()
	app.logger.Info("System metrics collection started", nil)

	// Start backtest manager
	if err := app.backtestManager.Start(app.ctx); err != nil {
		return fmt.Errorf("failed to start backtest manager: %w", err)
	}
	app.logger.Info("Backtest manager started", nil)

	// Start HTTP server
	go func() {
		if err := app.httpServer.Start(app.ctx); err != nil {
			app.logger.Error("HTTP server error", map[string]interface{}{"error": err})
		}
	}()
	app.logger.Info("HTTP server started", map[string]interface{}{
		"port": app.config.Service.Port,
		"host": app.config.Service.Host,
	})

	app.logger.Info("Backtesting Service is now running", map[string]interface{}{
		"service": appName,
		"version": appVersion,
		"port":    app.config.Service.Port,
		"api":     fmt.Sprintf("http://%s:%d/api/v1", app.config.Service.Host, app.config.Service.Port),
	})

	return nil
}

// startMetricsCollection starts periodic metrics collection
func (app *Application) startMetricsCollection() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-ticker.C:
			uptime := time.Since(startTime)
			app.metricsCollector.RecordServiceUptime(uptime)
			app.metricsCollector.RecordServiceHealth(true)

			// Record component health
			app.metricsCollector.RecordComponentHealth("service", true)
			// TODO: Add more component health checks

		case <-app.ctx.Done():
			return
		}
	}
}

// Stop gracefully shuts down the application
func (app *Application) Stop() error {
	app.logger.Info("Initiating graceful shutdown...", nil)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	shutdownComplete := make(chan error, 1)

	go func() {
		var lastErr error

		// Stop backtest manager
		app.logger.Info("Stopping backtest manager...", nil)
		if err := app.backtestManager.Stop(); err != nil {
			app.logger.Error("Error stopping backtest manager", map[string]interface{}{"error": err})
			lastErr = err
		}

		// Stop HTTP server
		app.logger.Info("Stopping HTTP server...", nil)
		if err := app.httpServer.Stop(shutdownCtx); err != nil {
			app.logger.Error("Error stopping HTTP server", map[string]interface{}{"error": err})
			lastErr = err
		}

		// Close data provider
		app.logger.Info("Closing data provider...", nil)
		if err := app.dataProvider.Close(); err != nil {
			app.logger.Error("Error closing data provider", map[string]interface{}{"error": err})
			lastErr = err
		}

		// Close result storage
		app.logger.Info("Closing result storage...", nil)
		if err := app.resultStorage.Close(); err != nil {
			app.logger.Error("Error closing result storage", map[string]interface{}{"error": err})
			lastErr = err
		}

		// Close Redis client
		app.logger.Info("Closing Redis client...", nil)
		if err := app.redisClient.Close(); err != nil {
			app.logger.Error("Error closing Redis client", map[string]interface{}{"error": err})
			lastErr = err
		}

		// Cancel context
		app.cancel()

		shutdownComplete <- lastErr
	}()

	// Wait for shutdown or timeout
	select {
	case err := <-shutdownComplete:
		if err != nil {
			app.logger.Error("Shutdown completed with errors", map[string]interface{}{"error": err})
			return err
		}
		app.logger.Info("Graceful shutdown completed successfully", nil)
		return nil

	case <-shutdownCtx.Done():
		app.logger.Error("Shutdown timeout exceeded, forcing exit", nil)
		return fmt.Errorf("shutdown timeout exceeded")
	}
}

// Run executes the application with signal handling
func (app *Application) Run() error {
	// Start the application
	if err := app.Start(); err != nil {
		return err
	}

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-sigChan
	app.logger.Info("Received shutdown signal", map[string]interface{}{"signal": sig})

	// Perform graceful shutdown
	return app.Stop()
}

func main() {
	// Create application
	app, err := NewApplication()
	if err != nil {
		log.Fatalf("Failed to create application: %v", err)
	}

	// Run application
	if err := app.Run(); err != nil {
		log.Fatalf("Application error: %v", err)
	}

	log.Println("Application exited successfully")
}
