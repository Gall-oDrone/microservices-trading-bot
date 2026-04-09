package main

import (
	"context"
	"encoding/json"
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
	"bitso-trading-platform/shared/pkg/kafka"
	sharedModels "bitso-trading-platform/shared/pkg/models"
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
	signalsConsumer      *consumer.SignalsConsumer
	ordersPlacedConsumer *consumer.OrdersPlacedConsumer
	bitsoSyncJob         *sync.BitsoSyncJob
	userTradesPoller     *sync.UserTradesPoller
	orderFillsProducer   *kafka.Producer

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

	// Intraday P&L: prime gauges so Grafana shows 0 (not empty) before first fill/feed
	metricsCollector.PrimeIntradayGauges("MXN", "btc_mxn", "basic")

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

	// When Bitso credentials exist, Bitso sync sets orders_active from /open_orders (matches Stage UI); otherwise use repo counts.
	repositoryActiveOrdersGauge := cfg.Bitso.APIKey == "" || cfg.Bitso.APISecret == ""

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
		repositoryActiveOrdersGauge,
	)
	appLogger.Info("Order manager initialized", map[string]interface{}{
		"active_orders_gauge_source": map[bool]string{true: "repository", false: "bitso_open_orders"}[repositoryActiveOrdersGauge],
	})

	var orderFillsProducer *kafka.Producer
	if cfg.Kafka.OrderFillsPublishEnabled && cfg.Kafka.TopicOrderFills != "" {
		var err error
		orderFillsProducer, err = kafka.NewProducer(&kafka.ProducerConfig{
			Brokers:          cfg.Kafka.Brokers,
			Topic:            cfg.Kafka.TopicOrderFills,
			BatchSize:        cfg.Kafka.BatchSize,
			BatchTimeout:     cfg.Kafka.BatchTimeout,
			CompressionCodec: cfg.Kafka.CompressionCodec,
			RequiredAcks:     cfg.Kafka.RequiredAcks,
		})
		if err != nil {
			appLogger.Warn("Kafka order-fills producer disabled (failed to create)", map[string]interface{}{"error": err.Error()})
			orderFillsProducer = nil
		} else {
			topicOrderFills := cfg.Kafka.TopicOrderFills
			orderManager.SetOrderFillPublisher(func(ctx context.Context, ev *sharedModels.OrderFillEvent) error {
				b, err := json.Marshal(ev)
				if err != nil {
					return err
				}
				key := ev.EventID
				if key == "" {
					key = ev.OrderID
				}
				if err := orderFillsProducer.Produce(ctx, []byte(key), b); err != nil {
					return err
				}
				appLogger.Info("kafka_order_fill_published", map[string]interface{}{
					"topic":          topicOrderFills,
					"event_id":       ev.EventID,
					"order_id":       ev.OrderID,
					"book":           ev.Book,
					"side":           ev.Side,
					"average_price":  ev.AveragePrice,
					"filled_amount":  ev.FilledAmount,
				})
				return nil
			})
			appLogger.Info("Kafka order-fills producer enabled", map[string]interface{}{"topic": cfg.Kafka.TopicOrderFills})
		}
	}

	// Consumer for trading.signals: creates Redis orders (event_id) before trading-engine publishes trading.orders.placed.
	signalsConsumer, _ := consumer.NewSignalsConsumer(
		cfg.Kafka.Brokers,
		cfg.Kafka.TopicSignals,
		cfg.Kafka.ConsumerGroup+"-signals",
		orderManager,
		appLogger,
		cfg.Kafka.SignalsAutoOffsetReset,
	)
	if signalsConsumer != nil {
		appLogger.Info("Trading-signals consumer configured", map[string]interface{}{"topic": cfg.Kafka.TopicSignals})
	}

	// Optional: consumer for trading.orders.placed (from trading-engine)
	ordersPlacedConsumer, _ := consumer.NewOrdersPlacedConsumer(
		cfg.Kafka.Brokers,
		cfg.Kafka.TopicOrdersPlaced,
		cfg.Kafka.ConsumerGroup+"-orders-placed",
		orderManager,
		appLogger,
		cfg.Kafka.OrdersPlacedAutoOffsetReset,
	)
	if ordersPlacedConsumer != nil {
		appLogger.Info("Orders-placed consumer configured", map[string]interface{}{"topic": cfg.Kafka.TopicOrdersPlaced})
	}

	// Optional: Bitso sync job (poll order status when Bitso credentials set)
	var bitsoSyncJob *sync.BitsoSyncJob
	var userTradesPoller *sync.UserTradesPoller
	if cfg.Bitso.APIKey != "" && cfg.Bitso.APISecret != "" {
		bitsoClient := bitso.NewClient()
		bitsoClient.SetLogLevel(bitso.LogLevelInfo)
		bitsoClient.SetAuth(cfg.Bitso.APIKey, cfg.Bitso.APISecret)
		bitsoClient.SetAPIBaseURL(cfg.Bitso.APIBaseURL)
		bitsoSyncJob = sync.NewBitsoSyncJob(bitsoClient, orderManager, appLogger, cfg.Bitso.SyncInterval, metricsCollector)
		appLogger.Info("Bitso sync job configured", map[string]interface{}{"interval": cfg.Bitso.SyncInterval})
		// User-trades poller for continuous fill discovery
		if cfg.Bitso.UserTradesPollEnabled {
			book := os.Getenv("BITSO_BOOK")
			if book == "" {
				book = "btc_mxn"
			}
			userTradesPoller = sync.NewUserTradesPoller(bitsoClient, orderManager, appLogger, cfg.Bitso.UserTradesPollInterval, book, metricsCollector)
			appLogger.Info("User-trades poller configured", map[string]interface{}{
				"interval": cfg.Bitso.UserTradesPollInterval,
				"book":     book,
			})
		}
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

	// Initialize HTTP server with validation endpoint for pre-trade risk checks
	httpServer := server.NewHTTPServerWithOptions(
		&cfg.Service,
		healthManager,
		metricsCollector,
		appLogger,
		&server.HTTPServerOptions{
			SessionAggregator: pnlRecorder,
			Validator:         orderValidator,
			RiskManager:       riskManager,
		},
	)
	appLogger.Info("HTTP server initialized", map[string]interface{}{
		"endpoints": []string{"/health", "/api/v1/status", "/api/v1/risk/session", "/api/v1/orders/validate"},
	})

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
		signalsConsumer:      signalsConsumer,
		ordersPlacedConsumer: ordersPlacedConsumer,
		bitsoSyncJob:         bitsoSyncJob,
		userTradesPoller:     userTradesPoller,
		orderFillsProducer:   orderFillsProducer,
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

	if app.signalsConsumer != nil {
		go app.signalsConsumer.Run(app.ctx)
	}
	if app.ordersPlacedConsumer != nil {
		go app.ordersPlacedConsumer.Run(app.ctx)
	}
	if app.bitsoSyncJob != nil {
		go app.bitsoSyncJob.Run(app.ctx)
	}
	if app.userTradesPoller != nil {
		go app.userTradesPoller.Run(app.ctx)
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
	app.feedIntradayMetricsOnce()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			app.feedIntradayMetricsOnce()
		}
	}
}

func (app *Application) feedIntradayMetricsOnce() {
	ctx, cancel := context.WithTimeout(app.ctx, 10*time.Second)
	summary, err := app.orderManager.GetPositionSummary(ctx)
	cancel()
	if err != nil {
		app.logger.Debug("Position summary for intraday metrics failed (may be empty)", map[string]interface{}{"error": err.Error()})
		return
	}
	if summary == nil {
		return
	}
	currency := "MXN"
	app.pnlRecorder.RecordDailyUnrealizedPnL(currency, decimal.NewFromFloat(summary.TotalUnrealizedPnL))
	app.pnlRecorder.RecordEquityUpdate(currency, decimal.NewFromFloat(summary.TotalPnL))
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

		if app.signalsConsumer != nil {
			app.logger.Info("Closing trading-signals consumer...", nil)
			if err := app.signalsConsumer.Close(); err != nil {
				app.logger.Error("Error closing trading-signals consumer", map[string]interface{}{"error": err})
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
		if app.orderFillsProducer != nil {
			app.logger.Info("Closing order-fills Kafka producer...", nil)
			if err := app.orderFillsProducer.Close(); err != nil {
				app.logger.Error("Error closing order-fills producer", map[string]interface{}{"error": err})
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
