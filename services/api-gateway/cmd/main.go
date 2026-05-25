package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"strings"

	"bitso-trading-platform/api-gateway/internal/api"
	"bitso-trading-platform/api-gateway/internal/client"
	"bitso-trading-platform/api-gateway/internal/config"
	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
	researchpkg "bitso-trading-platform/api-gateway/internal/research"
	"bitso-trading-platform/api-gateway/internal/router"
	"bitso-trading-platform/api-gateway/internal/server"
	"bitso-trading-platform/shared/pkg/health"
	"bitso-trading-platform/shared/pkg/kafka"
)

const (
	appName    = "api-gateway"
	appVersion = "1.0.0"

	shutdownTimeout = 30 * time.Second
)

// Application encapsulates all application components
type Application struct {
	config *config.Config
	logger *logger.Logger

	// Core components
	metrics       *metrics.MetricsCollector
	healthManager *health.HealthManager
	clientFactory *client.ClientFactory

	// Handlers
	marketDataHandler  *api.MarketDataHandler
	orderHandler       *api.OrderHandler
	strategyHandler    *api.StrategyHandler
	aggregationHandler *api.AggregationHandler
	researchHandler    *api.ResearchHandler
	signalProducer     *kafka.Producer
	mainHandler        *api.Handler

	// Router and server
	router     *router.Router
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
	appLogger.Info("API Gateway Service starting...", map[string]interface{}{
		"version":     appVersion,
		"name":        appName,
		"environment": cfg.Service.Environment,
	})

	// Create context
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize metrics collector
	metricsCollector := metrics.NewMetricsCollector(cfg.Service.Name)
	appLogger.Info("Metrics collector initialized", nil)

	// Initialize health manager
	healthManager := health.NewHealthManager(log.New(os.Stdout, "[HEALTH] ", log.LstdFlags))
	appLogger.Info("Health manager initialized", nil)

	// Add basic service health check
	healthManager.AddChecker(health.NewSimpleHealthChecker("service", func(ctx context.Context) error {
		return nil
	}))

	// Initialize client factory
	clientFactory, err := client.NewClientFactory(cfg, appLogger, metricsCollector)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create client factory: %w", err)
	}
	appLogger.Info("Client factory initialized", nil)

	// Get clients from factory
	marketDataClient := clientFactory.MarketDataClient()
	orderManagementClient := clientFactory.OrderManagementClient()
	strategyExecutorClient := clientFactory.StrategyExecutorClient()

	// Add health checks for backend services
	healthManager.AddChecker(health.NewHTTPHealthChecker(
		"market-data",
		cfg.Backend.MarketDataURL+"/health",
		5*time.Second,
	))
	healthManager.AddChecker(health.NewHTTPHealthChecker(
		"order-management",
		cfg.Backend.OrderManagementURL+"/health",
		5*time.Second,
	))
	healthManager.AddChecker(health.NewHTTPHealthChecker(
		"strategy-executor",
		cfg.Backend.StrategyExecutorURL+"/health",
		5*time.Second,
	))
	appLogger.Info("Backend service health checks registered", nil)

	// Initialize handlers
	marketDataHandler := api.NewMarketDataHandler(marketDataClient, appLogger, metricsCollector)
	orderHandler := api.NewOrderHandler(orderManagementClient, appLogger, metricsCollector)
	strategyHandler := api.NewStrategyHandler(strategyExecutorClient, appLogger, metricsCollector)
	aggregationHandler := api.NewAggregationHandler(
		marketDataClient,
		orderManagementClient,
		strategyExecutorClient,
		appLogger,
		metricsCollector,
	)
	var researchHandler *api.ResearchHandler
	var signalProducer *kafka.Producer
	if cfg.Research.Enabled {
		var memoStore *researchpkg.MemoStore
		if cfg.Research.S3Bucket != "" {
			ms, err := researchpkg.NewMemoStore(ctx, cfg.Research.S3Bucket, cfg.Research.S3Prefix)
			if err != nil {
				cancel()
				return nil, fmt.Errorf("research memo store: %w", err)
			}
			memoStore = ms
		}
		researchAgent := client.NewResearchAgentClient(cfg.Backend.ResearchAgentURL, cfg.Client.Timeout)
		if cfg.Research.KafkaBrokers != "" && cfg.Research.KafkaTopicSignals != "" {
			brokers := strings.Split(cfg.Research.KafkaBrokers, ",")
			for i := range brokers {
				brokers[i] = strings.TrimSpace(brokers[i])
			}
			prod, err := kafka.NewProducer(&kafka.ProducerConfig{
				Brokers: brokers,
				Topic:   cfg.Research.KafkaTopicSignals,
			})
			if err != nil {
				cancel()
				return nil, fmt.Errorf("research signal producer: %w", err)
			}
			signalProducer = prod
		}
		researchHandler = api.NewResearchHandler(cfg, appLogger, metricsCollector, memoStore, researchAgent, signalProducer)
		appLogger.Info("Research API enabled", map[string]interface{}{
			"s3_bucket":    cfg.Research.S3Bucket,
			"kafka_topic":  cfg.Research.KafkaTopicSignals,
			"research_url": cfg.Backend.ResearchAgentURL,
		})
	}

	appLogger.Info("API handlers initialized", nil)

	// Initialize main handler
	mainHandler := api.NewHandler(
		cfg,
		appLogger,
		metricsCollector,
		healthManager,
		marketDataHandler,
		orderHandler,
		strategyHandler,
		aggregationHandler,
		researchHandler,
	)
	appLogger.Info("Main handler initialized", nil)

	// Setup router with middleware
	appRouter := router.SetupRoutes(cfg, mainHandler, appLogger, metricsCollector)
	appLogger.Info("Router configured with middleware", nil)

	// Initialize HTTP server
	httpServer := server.NewHTTPServer(cfg, appRouter.Handler(), appLogger)
	appLogger.Info("HTTP server initialized", nil)

	appLogger.Info("Configuration loaded successfully", map[string]interface{}{
		"service_port":            cfg.Service.Port,
		"environment":             cfg.Service.Environment,
		"market_data_url":         cfg.Backend.MarketDataURL,
		"order_management_url":    cfg.Backend.OrderManagementURL,
		"strategy_executor_url":   cfg.Backend.StrategyExecutorURL,
		"rate_limiting_enabled":   cfg.RateLimit.Enabled,
		"circuit_breaker_enabled": cfg.CircuitBreaker.Enabled,
		"auth_enabled":            cfg.Auth.Enabled,
	})

	return &Application{
		config:             cfg,
		logger:             appLogger,
		metrics:            metricsCollector,
		healthManager:      healthManager,
		clientFactory:      clientFactory,
		marketDataHandler:  marketDataHandler,
		orderHandler:       orderHandler,
		strategyHandler:    strategyHandler,
		aggregationHandler: aggregationHandler,
		researchHandler:    researchHandler,
		signalProducer:     signalProducer,
		mainHandler:        mainHandler,
		router:             appRouter,
		httpServer:         httpServer,
		ctx:                ctx,
		cancel:             cancel,
	}, nil
}

// Start initializes and starts all components
func (app *Application) Start() error {
	app.logger.Info("Starting application components...", nil)

	// Start metrics collection
	go app.startMetricsCollection()
	app.logger.Info("System metrics collection started", nil)

	// Start HTTP server
	go func() {
		if err := app.httpServer.Start(app.ctx); err != nil {
			app.logger.Error("HTTP server error", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()
	
	// Give server a moment to start
	time.Sleep(100 * time.Millisecond)
	
	app.logger.Info("API Gateway is now running", map[string]interface{}{
		"service": appName,
		"version": appVersion,
		"port":    app.config.Service.Port,
		"address": fmt.Sprintf("http://%s:%d", app.config.Service.Host, app.config.Service.Port),
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
			app.metrics.RecordServiceUptime(uptime)
			app.metrics.RecordServiceHealth(true)
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

		// Stop HTTP server
		app.logger.Info("Stopping HTTP server...", nil)
		if err := app.httpServer.Stop(shutdownCtx); err != nil {
			app.logger.Error("Error stopping HTTP server", map[string]interface{}{
				"error": err.Error(),
			})
			lastErr = err
		}

		if app.signalProducer != nil {
			app.logger.Info("Closing research signal producer...", nil)
			if err := app.signalProducer.Close(); err != nil {
				app.logger.Error("Error closing signal producer", map[string]interface{}{
					"error": err.Error(),
				})
				lastErr = err
			}
		}

		// Close client factory
		app.logger.Info("Closing client factory...", nil)
		if err := app.clientFactory.Close(); err != nil {
			app.logger.Error("Error closing client factory", map[string]interface{}{
				"error": err.Error(),
			})
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
			app.logger.Error("Shutdown completed with errors", map[string]interface{}{
				"error": err.Error(),
			})
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
	app.logger.Info("Received shutdown signal", map[string]interface{}{
		"signal": sig.String(),
	})

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
