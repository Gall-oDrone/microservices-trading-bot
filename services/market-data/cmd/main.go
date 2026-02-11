package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"bitso-trading-platform/market-data/internal/api"
	"bitso-trading-platform/market-data/internal/cache"
	"bitso-trading-platform/market-data/internal/config"
	"bitso-trading-platform/market-data/internal/historical"
	"bitso-trading-platform/market-data/internal/logger"
	"bitso-trading-platform/market-data/internal/metrics"
	"bitso-trading-platform/market-data/internal/processor"
	"bitso-trading-platform/market-data/internal/publisher"
	"bitso-trading-platform/market-data/internal/server"
	"bitso-trading-platform/market-data/internal/websocket"
	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/health"
	"bitso-trading-platform/shared/pkg/kafka"
	"bitso-trading-platform/shared/pkg/service"
)

const (
	appName    = "market-data"
	appVersion = "1.0.0"

	shutdownTimeout = 30 * time.Second
)

// Application encapsulates all application components
type Application struct {
	logger logger.Logger
	config *config.Config

	// Core components
	wsManager      websocket.StreamManager
	tradeProcessor processor.TradeProcessor
	tradePublisher publisher.TradePublisher
	kafkaProducer  *kafka.Producer

	// New components
	cacheLayer       cache.Cache
	storage          historical.Storage
	httpServer       *server.HTTPServer
	metricsCollector *metrics.MetricsCollector
	healthManager    *health.HealthManager
	serviceManager   *service.Service

	// Context
	ctx    context.Context
	cancel context.CancelFunc
}

// NewApplication creates and initializes the application
func NewApplication() (*Application, error) {
	// Initialize structured logger
	appLogger := logger.DefaultLogger()
	appLogger.Info("Starting market-data service", map[string]interface{}{
		"version": appVersion,
		"name":    appName,
	})

	// Create context
	ctx, cancel := context.WithCancel(context.Background())

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	appLogger.Info("Configuration loaded successfully")

	// Convert logger to *log.Logger for compatibility
	stdLogger := logger.ToStdLogger(appLogger)

	// Initialize metrics collector
	metricsCollector := metrics.NewMetricsCollector(stdLogger)
	appLogger.Info("Metrics collector initialized")

	// Initialize health manager
	healthManager := health.NewHealthManager(stdLogger)
	appLogger.Info("Health manager initialized")

	// Initialize cache layer
	cacheConfig := cache.DefaultCacheConfig()
	cacheConfig.RedisHost = cfg.RedisHost
	cacheConfig.RedisPort = cfg.RedisPort
	cacheConfig.RedisPassword = cfg.RedisPassword
	cacheConfig.RedisDB = cfg.RedisDB

	cacheLayer, err := cache.NewRedisCache(cacheConfig, stdLogger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create cache layer: %w", err)
	}
	appLogger.Info("Cache layer initialized")

	// Initialize historical storage
	storageConfig := historical.DefaultStorageConfig()
	storageConfig.BackendType = "redis"
	storageConfig.RedisHost = cfg.RedisHost
	storageConfig.RedisPort = cfg.RedisPort
	storageConfig.RedisPassword = cfg.RedisPassword
	storageConfig.RedisDB = cfg.RedisDB + 1 // Use different DB for historical data
	storageConfig.RetentionDays = 30

	storage, err := historical.NewRedisStorage(storageConfig, stdLogger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create storage: %w", err)
	}
	appLogger.Info("Historical storage initialized")

	// Initialize Kafka producer
	var kafkaProducer *kafka.Producer
	if cfg.EnableKafka {
		producerConfig := &kafka.ProducerConfig{
			Brokers:          []string{cfg.KafkaBrokers},
			Topic:            cfg.KafkaTopicTrades,
			BatchSize:        10,
			BatchTimeout:     1 * time.Second,
			CompressionCodec: "snappy",
			RequiredAcks:     -1,
			MaxAttempts:      3,
			WriteTimeout:     10 * time.Second,
		}

		kafkaProducer, err = kafka.NewProducer(producerConfig)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
		}
		appLogger.Info("Kafka producer initialized")
	}

	// Initialize WebSocket Manager
	wsManagerConfig := &websocket.ManagerConfig{
		ReconnectAttempts: cfg.WSReconnectAttempts,
		ReconnectInterval: cfg.WSReconnectInterval,
		ReconnectMaxDelay: cfg.WSReconnectMaxDelay,
		Logger:            stdLogger, // websocket.ManagerConfig uses *log.Logger
	}
	wsManager := websocket.NewManager(wsManagerConfig)
	appLogger.Info("WebSocket manager created")

	// Initialize Trade Processor
	processorConfig := &processor.ProcessorConfig{
		Logger:       stdLogger, // processor uses *log.Logger
		TradesInput:  wsManager.GetTradesStream(),
		OutputBuffer: 100,
	}
	tradeProcessor := processor.NewProcessor(processorConfig)
	appLogger.Info("Trade processor created")

	// Initialize Trade Publisher
	var tradePublisher publisher.TradePublisher
	if cfg.EnableKafka && kafkaProducer != nil {
		publisherConfig := &publisher.PublisherConfig{
			Logger:      stdLogger, // publisher uses *log.Logger
			Producer:    kafkaProducer,
			Topic:       cfg.KafkaTopicTrades,
			TradesInput: tradeProcessor.GetProcessedTradesStream(),
		}
		tradePublisher = publisher.NewPublisher(publisherConfig)
		appLogger.Info("Trade publisher created")
	}

	// Initialize API handler
	apiHandler := api.NewHandler(cacheLayer, storage, stdLogger)
	appLogger.Info("API handler created")

	// Initialize HTTP server (with /metrics for Prometheus)
	httpServer := server.NewHTTPServer(cfg.ServicePort, apiHandler, stdLogger, metricsCollector.GetPrometheusHandler())
	appLogger.Info("HTTP server created")

	// Initialize service manager
	// Convert ServicePort from string to int
	servicePortInt := 8083 // default
	if portStr := cfg.ServicePort; portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			servicePortInt = port
		}
	}
	serviceConfig := &service.ServiceConfig{
		Name:        appName,
		Version:     appVersion,
		Host:        "localhost",
		Port:        servicePortInt,
		HealthCheck: "/health",
		Metadata: map[string]string{
			"service": "market-data",
			"version": appVersion,
		},
		Tags: []string{"market-data", "trading", "websocket"},
	}
	serviceManager := service.NewService(serviceConfig, nil, stdLogger) // No registry for now
	appLogger.Info("Service manager created")

	return &Application{
		logger:           appLogger,
		config:           cfg,
		wsManager:        wsManager,
		tradeProcessor:   tradeProcessor,
		tradePublisher:   tradePublisher,
		kafkaProducer:    kafkaProducer,
		cacheLayer:       cacheLayer,
		storage:          storage,
		httpServer:       httpServer,
		metricsCollector: metricsCollector,
		healthManager:    healthManager,
		serviceManager:   serviceManager,
		ctx:              ctx,
		cancel:           cancel,
	}, nil
}

// Start initializes and starts all components
func (app *Application) Start() error {
	app.logger.Info("Starting application components...")

	// Start metrics collection
	go app.metricsCollector.StartSystemMetricsCollection(app.ctx)
	app.logger.Info("System metrics collection started")

	// Start HTTP server
	go func() {
		if err := app.httpServer.Start(app.ctx); err != nil {
			app.logger.Error("HTTP server error", map[string]interface{}{"error": err})
		}
	}()
	app.logger.Info("HTTP server started")

	// Parse trading books
	books := make([]*bitso.Book, 0, len(app.config.BitsoBooks))
	for _, bookStr := range app.config.BitsoBooks {
		book, err := parseBook(bookStr)
		if err != nil {
			return fmt.Errorf("failed to parse book %s: %w", bookStr, err)
		}
		books = append(books, book)
	}
	app.logger.Info("Trading books configured", map[string]interface{}{"books": app.config.BitsoBooks})

	// Connect to WebSocket
	if err := app.wsManager.Connect(app.ctx); err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	// Subscribe to channels
	if err := app.wsManager.Subscribe(books, app.config.BitsoChannels); err != nil {
		return fmt.Errorf("failed to subscribe to channels: %w", err)
	}

	// Start WebSocket manager
	if err := app.wsManager.Start(app.ctx); err != nil {
		return fmt.Errorf("failed to start WebSocket manager: %w", err)
	}
	app.logger.Info("WebSocket manager started")

	// Start trade processor
	if err := app.tradeProcessor.Start(app.ctx); err != nil {
		return fmt.Errorf("failed to start trade processor: %w", err)
	}
	app.logger.Info("Trade processor started")

	// Start trade publisher
	if app.tradePublisher != nil {
		if err := app.tradePublisher.Start(app.ctx); err != nil {
			return fmt.Errorf("failed to start trade publisher: %w", err)
		}
		app.logger.Info("Trade publisher started")
	}

	app.logger.Info("Market data service is now running", map[string]interface{}{
		"service": appName,
		"version": appVersion,
		"port":    app.config.ServicePort,
	})
	return nil
}

// Stop gracefully shuts down the application
func (app *Application) Stop() error {
	app.logger.Info("Initiating graceful shutdown...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	shutdownComplete := make(chan error, 1)

	go func() {
		var lastErr error

		// Stop HTTP server
		app.logger.Info("Stopping HTTP server...")
		if err := app.httpServer.Stop(); err != nil {
			app.logger.Error("Error stopping HTTP server", map[string]interface{}{"error": err})
			lastErr = err
		}

		// Stop trade publisher
		if app.tradePublisher != nil {
			app.logger.Info("Stopping trade publisher...")
			if err := app.tradePublisher.Stop(); err != nil {
				app.logger.Error("Error stopping trade publisher", map[string]interface{}{"error": err})
				lastErr = err
			}
		}

		// Stop trade processor
		app.logger.Info("Stopping trade processor...")
		if err := app.tradeProcessor.Stop(); err != nil {
			app.logger.Error("Error stopping trade processor", map[string]interface{}{"error": err})
			lastErr = err
		}

		// Stop WebSocket manager
		app.logger.Info("Stopping WebSocket manager...")
		if err := app.wsManager.Stop(); err != nil {
			app.logger.Error("Error stopping WebSocket manager", map[string]interface{}{"error": err})
			lastErr = err
		}

		// Close cache layer
		app.logger.Info("Closing cache layer...")
		if err := app.cacheLayer.Close(); err != nil {
			app.logger.Error("Error closing cache layer", map[string]interface{}{"error": err})
			lastErr = err
		}

		// Close storage
		app.logger.Info("Closing storage...")
		if err := app.storage.Close(); err != nil {
			app.logger.Error("Error closing storage", map[string]interface{}{"error": err})
			lastErr = err
		}

		// Close Kafka producer
		if app.kafkaProducer != nil {
			app.logger.Info("Closing Kafka producer...")
			if err := app.kafkaProducer.Close(); err != nil {
				app.logger.Error("Error closing Kafka producer", map[string]interface{}{"error": err})
				lastErr = err
			}
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
		app.logger.Info("Graceful shutdown completed successfully")
		return nil

	case <-shutdownCtx.Done():
		app.logger.Error("Shutdown timeout exceeded, forcing exit")
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

// parseBook parses a book string (e.g., "btc_mxn") into a bitso.Book
func parseBook(bookStr string) (*bitso.Book, error) {
	// Split by underscore
	parts := make([]string, 0, 2)
	current := ""
	for _, ch := range bookStr {
		if ch == '_' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}

	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid book format: %s (expected major_minor)", bookStr)
	}

	major := bitso.ToCurrency(parts[0])
	minor := bitso.ToCurrency(parts[1])

	if major == bitso.CurrencyNone || minor == bitso.CurrencyNone {
		return nil, fmt.Errorf("invalid currency in book: %s", bookStr)
	}

	return bitso.NewBook(major, minor), nil
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
