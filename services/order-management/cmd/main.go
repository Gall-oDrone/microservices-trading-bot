package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"

	"bitso-trading-platform/order-management/internal/config"
	"bitso-trading-platform/order-management/internal/consumer"
	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/manager"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/repository"
	"bitso-trading-platform/order-management/internal/risk"
	"bitso-trading-platform/order-management/internal/server"
	"bitso-trading-platform/order-management/internal/sync"
	"bitso-trading-platform/order-management/internal/validator"
	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/health"
)

const (
	appName    = "order-management"
	appVersion = "1.0.0"

	shutdownTimeout = 30 * time.Second
)

// Application encapsulates all application components
type Application struct {
	logger *logger.Logger
	config *config.Config

	// Core components
	healthManager        *health.HealthManager
	metricsCollector     *metrics.MetricsCollector
	httpServer           *server.HTTPServer
	orderManager         *manager.Manager
	pnlRecorder          *metrics.IntradayAggregator
	ordersPlacedConsumer *consumer.OrdersPlacedConsumer
	bitsoSyncJob         *sync.BitsoSyncJob

	// Redis client (non-nil when STORAGE_TYPE=redis; closed on shutdown)
	redisClient *redis.Client

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
	appLogger.Info("Order Management Service starting...", map[string]interface{}{
		"version":     appVersion,
		"name":        appName,
		"environment": cfg.Service.Environment,
	})

	// Create context
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize metrics collector
	metricsCollector := metrics.NewMetricsCollector(appName)
	appLogger.Info("Metrics collector initialized", nil)

	// Intraday P&L aggregator (writes to Prometheus)
	pnlRecorder := metrics.NewIntradayAggregator(metricsCollector, nil)
	appLogger.Info("Intraday P&L aggregator initialized", nil)

	// Repositories: memory (default) or Redis based on STORAGE_TYPE
	var orderRepo repository.OrderRepository
	var positionRepo repository.PositionRepository
	var redisClient *redis.Client

	if cfg.Storage.Type == "redis" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
			PoolSize: cfg.Redis.PoolSize,
		})
		if err := redisClient.Ping(ctx).Err(); err != nil {
			redisClient.Close()
			return nil, fmt.Errorf("redis connect: %w", err)
		}
		orderRepo = repository.NewRedisOrderRepository(redisClient, appLogger, metricsCollector)
		positionRepo = repository.NewRedisPositionRepository(redisClient, appLogger, metricsCollector)
		appLogger.Info("Storage: Redis (orders and positions)", map[string]interface{}{
			"redis_host": cfg.Redis.Host,
			"redis_port": cfg.Redis.Port,
		})
	} else {
		orderRepo = repository.NewInMemoryOrderRepository(appLogger, metricsCollector)
		positionRepo = repository.NewInMemoryPositionRepository(appLogger, metricsCollector)
		appLogger.Info("Storage: in-memory (orders and positions)", nil)
	}

	// Validator and risk manager
	orderValidator := validator.NewOrderValidator(&cfg.Risk, appLogger, orderRepo, metricsCollector)
	riskManager := risk.NewRiskManager(&cfg.Risk, appLogger, orderRepo, positionRepo, metricsCollector)

	var fillLedger repository.FillLedger
	if cfg.Storage.Type == "redis" && redisClient != nil {
		fillLedger = repository.NewRedisFillLedger(redisClient)
	} else {
		fillLedger = repository.NewInMemoryFillLedger()
	}

	// Order manager with PnL recorder so filled orders update intraday metrics
	orderManager := manager.NewOrderManager(
		cfg,
		appLogger,
		orderValidator,
		riskManager,
		orderRepo,
		positionRepo,
		metricsCollector,
		pnlRecorder,
		fillLedger,
	)
	appLogger.Info("Order manager initialized", nil)

	// Optional: consumer for trading.orders.placed (from trading-engine)
	ordersPlacedConsumer, _ := consumer.NewOrdersPlacedConsumer(
		cfg.Kafka.Brokers,
		cfg.Kafka.TopicOrdersPlaced,
		cfg.Kafka.ConsumerGroup+"-orders-placed",
		orderManager,
		appLogger,
	)
	if ordersPlacedConsumer != nil {
		appLogger.Info("Orders-placed consumer configured", map[string]interface{}{"topic": cfg.Kafka.TopicOrdersPlaced})
	}

	// Optional: Bitso sync job (poll order status when Bitso credentials set)
	var bitsoSyncJob *sync.BitsoSyncJob
	if cfg.Bitso.APIKey != "" && cfg.Bitso.APISecret != "" {
		bitsoClient := bitso.NewClient()
		bitsoClient.SetLogLevel(bitso.LogLevelInfo)
		bitsoClient.SetAuth(cfg.Bitso.APIKey, cfg.Bitso.APISecret)
		bitsoClient.SetAPIBaseURL(cfg.Bitso.APIBaseURL)
		bitsoSyncJob = sync.NewBitsoSyncJob(bitsoClient, orderManager, appLogger, 60*time.Second, metricsCollector)
		appLogger.Info("Bitso sync job configured", nil)
	}

	// Initialize health manager
	healthManager := health.NewHealthManager(log.New(os.Stdout, "[HEALTH] ", log.LstdFlags))
	appLogger.Info("Health manager initialized", nil)

	// Add basic health check
	healthManager.AddChecker(health.NewSimpleHealthChecker("service", func(ctx context.Context) error {
		return nil
	}))
	// When using Redis storage, add Redis health check
	if redisClient != nil {
		healthManager.AddChecker(health.NewSimpleHealthChecker("redis", func(ctx context.Context) error {
			return redisClient.Ping(ctx).Err()
		}))
	}

	// Initialize HTTP server
	httpServer := server.NewHTTPServer(
		&cfg.Service,
		healthManager,
		metricsCollector,
		appLogger,
		pnlRecorder,
	)
	appLogger.Info("HTTP server initialized", nil)

	appLogger.Info("Configuration loaded successfully", map[string]interface{}{
		"service_port": cfg.Service.Port,
		"environment":  cfg.Service.Environment,
	})

	return &Application{
		logger:               appLogger,
		config:               cfg,
		healthManager:        healthManager,
		metricsCollector:     metricsCollector,
		httpServer:           httpServer,
		orderManager:         orderManager,
		pnlRecorder:          pnlRecorder,
		ordersPlacedConsumer: ordersPlacedConsumer,
		bitsoSyncJob:         bitsoSyncJob,
		redisClient:          redisClient,
		ctx:                  ctx,
		cancel:               cancel,
	}, nil
}

// Start initializes and starts all components
func (app *Application) Start() error {
	app.logger.Info("Starting application components...", nil)

	// Start order manager (background tasks)
	if err := app.orderManager.Start(app.ctx); err != nil {
		return fmt.Errorf("order manager start: %w", err)
	}
	app.logger.Info("Order manager started", nil)

	// Feed equity and unrealized P&L into intraday aggregator periodically
	go app.feedIntradayMetrics()

	if app.ordersPlacedConsumer != nil {
		go app.ordersPlacedConsumer.Run(app.ctx)
	}
	if app.bitsoSyncJob != nil {
		go app.bitsoSyncJob.Run(app.ctx)
	}

	// Start metrics collection
	go app.startMetricsCollection()
	app.logger.Info("System metrics collection started", nil)

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

	app.logger.Info("Order Management Service is now running", map[string]interface{}{
		"service": appName,
		"version": appVersion,
		"port":    app.config.Service.Port,
	})

	return nil
}

// feedIntradayMetrics periodically pushes position summary (equity, unrealized P&L) to the aggregator.
func (app *Application) feedIntradayMetrics() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(app.ctx, 10*time.Second)
			summary, err := app.orderManager.GetPositionSummary(ctx)
			cancel()
			if err != nil {
				app.logger.Debug("Position summary for intraday metrics failed (may be empty)", map[string]interface{}{"error": err.Error()})
				continue
			}
			if summary == nil {
				continue
			}
			// Feed unrealized P&L and session equity (TotalPnL as proxy for drawdown)
			currency := "MXN"
			app.pnlRecorder.RecordDailyUnrealizedPnL(currency, decimal.NewFromFloat(summary.TotalUnrealizedPnL))
			app.pnlRecorder.RecordEquityUpdate(currency, decimal.NewFromFloat(summary.TotalPnL))
		}
	}
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

		// Stop order manager
		app.logger.Info("Stopping order manager...", nil)
		if err := app.orderManager.Stop(); err != nil {
			app.logger.Error("Error stopping order manager", map[string]interface{}{"error": err})
			lastErr = err
		}

		if app.redisClient != nil {
			app.logger.Info("Closing Redis client...", nil)
			if err := app.redisClient.Close(); err != nil {
				app.logger.Error("Error closing Redis client", map[string]interface{}{"error": err})
				lastErr = err
			}
		}

		if app.ordersPlacedConsumer != nil {
			app.logger.Info("Closing orders-placed consumer...", nil)
			if err := app.ordersPlacedConsumer.Close(); err != nil {
				app.logger.Error("Error closing orders-placed consumer", map[string]interface{}{"error": err})
				lastErr = err
			}
		}

		// Stop HTTP server
		app.logger.Info("Stopping HTTP server...", nil)
		if err := app.httpServer.Stop(shutdownCtx); err != nil {
			app.logger.Error("Error stopping HTTP server", map[string]interface{}{"error": err})
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
