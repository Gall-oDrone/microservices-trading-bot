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

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/config"
	"bitso-trading-platform/shared/pkg/database"
	"bitso-trading-platform/shared/pkg/kafka"
	"bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/trading-engine/internal/engine"
)

const (
	// Application metadata
	appName    = "trading-engine"
	appVersion = "1.0.0"

	// Graceful shutdown timeout
	shutdownTimeout = 30 * time.Second
)

// Application encapsulates all application components
type Application struct {
	logger        *log.Logger
	config        *config.Config
	bitsoClient   *bitso.Client
	redisClient   *database.RedisClient
	kafkaConsumer *kafka.Consumer
	engine        *engine.TradingEngine
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewApplication creates and initializes a new application instance
func NewApplication() (*Application, error) {
	// Initialize logger
	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", appName), log.LstdFlags|log.Lshortfile)
	logger.Printf("Starting %s v%s", appName, appVersion)

	// Create application context
	ctx, cancel := context.WithCancel(context.Background())

	// Load configuration from environment
	cfg, err := config.LoadConfig()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}
	logger.Println("✓ Configuration loaded successfully")

	// Initialize Bitso API client
	bitsoClient := initializeBitsoClient(cfg, logger)
	logger.Println("✓ Bitso API client initialized")

	// Initialize Redis client
	redisClient, err := initializeRedisClient(cfg, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize Redis: %w", err)
	}
	logger.Println("✓ Redis client connected")

	// Initialize Kafka consumer for trade signals
	kafkaConsumer, err := initializeKafkaConsumer(cfg, logger)
	if err != nil {
		cancel()
		redisClient.Close()
		return nil, fmt.Errorf("failed to initialize Kafka consumer: %w", err)
	}
	logger.Println("✓ Kafka consumer initialized")

	// Create trading configuration
	tradingConfig := createTradingConfig()
	logger.Printf("✓ Trading configuration: Book=%s, Strategy=%s",
		tradingConfig.Book.String(), tradingConfig.StrategyType)

	// Initialize trading engine
	tradingEngine, err := engine.NewTradingEngine(
		tradingConfig,
		cfg,
		bitsoClient,
		redisClient,
		kafkaConsumer,
	)
	if err != nil {
		cancel()
		redisClient.Close()
		kafkaConsumer.Close()
		return nil, fmt.Errorf("failed to create trading engine: %w", err)
	}
	logger.Println("✓ Trading engine created")

	return &Application{
		logger:        logger,
		config:        cfg,
		bitsoClient:   bitsoClient,
		redisClient:   redisClient,
		kafkaConsumer: kafkaConsumer,
		engine:        tradingEngine,
		ctx:           ctx,
		cancel:        cancel,
	}, nil
}

// initializeBitsoClient creates and configures the Bitso API client
func initializeBitsoClient(cfg *config.Config, logger *log.Logger) *bitso.Client {
	client := bitso.NewClient()
	client.SetLogLevel(bitso.LogLevelInfo)
	client.SetAuth(cfg.StageBitsoAPIKey, cfg.StageBitsoAPISecret)
	client.SetAPIBaseURL("https://stage.bitso.com/api")

	// Set rate limiting for API protection
	client.SetBurstRate(100 * time.Millisecond)

	return client
}

// initializeRedisClient creates and connects to Redis
func initializeRedisClient(cfg *config.Config, logger *log.Logger) (*database.RedisClient, error) {
	redisPort, err := strconv.Atoi(cfg.RedisPort)
	if err != nil {
		return nil, fmt.Errorf("invalid Redis port: %w", err)
	}

	redisClient, err := database.InitializeWithConfig(
		cfg.RedisHost,
		redisPort,
		cfg.RedisPassword,
		cfg.RedisDB,
		10, // Connection pool size
	)
	if err != nil {
		return nil, err
	}

	// Verify connection
	if err := redisClient.Ping(context.Background()); err != nil {
		redisClient.Close()
		return nil, fmt.Errorf("Redis ping failed: %w", err)
	}

	return redisClient, nil
}

// initializeKafkaConsumer creates a Kafka consumer for trade signals
func initializeKafkaConsumer(cfg *config.Config, logger *log.Logger) (*kafka.Consumer, error) {
	// Get topic from config or use default
	topic := cfg.KafkaTopicSignals
	if topic == "" {
		topic = "trading.signals"
	}
	
	consumerConfig := &kafka.ConsumerConfig{
		Brokers:         []string{cfg.KafkaBrokers},
		Topic:           topic,
		GroupID:         "trading-engine-group",
		AutoOffsetReset: "latest",
	}

	consumer, err := kafka.NewConsumer(consumerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka consumer: %w", err)
	}

	return consumer, nil
}

// createTradingConfig creates the trading configuration
func createTradingConfig() *models.TradingConfig {
	return &models.TradingConfig{
		Book:              bitso.NewBook(bitso.BTC, bitso.MXN),
		MinTradeAmount:    0.001,          // 0.001 BTC minimum
		MaxTradeAmount:    0.1,            // 0.1 BTC maximum
		MaxTradeValue:     10000.0,        // 10,000 MXN maximum
		StopLossPercent:   2.0,            // 2% stop loss
		TakeProfitPercent: 3.0,            // 3% take profit
		MaxTradingTime:    24 * time.Hour, // 24 hours max position time
		StartTime:         time.Now(),
		EndTime:           time.Now().Add(24 * time.Hour),
		MaxOpenPositions:  3, // Maximum 3 concurrent positions
		StrategyType:      "basic",
		Parameters:        make(map[string]interface{}),
	}
}

// Start initializes and starts the application
func (app *Application) Start() error {
	app.logger.Println("Starting application components...")

	// Initialize trading engine
	if err := app.engine.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize trading engine: %w", err)
	}
	app.logger.Println("✓ Trading engine initialized")

	// Start trading engine
	if err := app.engine.Start(); err != nil {
		return fmt.Errorf("failed to start trading engine: %w", err)
	}
	app.logger.Println("✓ Trading engine started")

	app.logger.Printf("🚀 %s is now running and processing trade signals", appName)
	return nil
}

// Stop gracefully shuts down the application
func (app *Application) Stop() error {
	app.logger.Println("Initiating graceful shutdown...")

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	// Channel to track shutdown completion
	shutdownComplete := make(chan error, 1)

	go func() {
		var lastErr error

		// Stop trading engine
		app.logger.Println("Stopping trading engine...")
		if err := app.engine.Stop(); err != nil {
			app.logger.Printf("Error stopping trading engine: %v", err)
			lastErr = err
		}

		// Close Kafka consumer
		app.logger.Println("Closing Kafka consumer...")
		if err := app.kafkaConsumer.Close(); err != nil {
			app.logger.Printf("Error closing Kafka consumer: %v", err)
			lastErr = err
		}

		// Close Redis connection
		app.logger.Println("Closing Redis connection...")
		if err := app.redisClient.Close(); err != nil {
			app.logger.Printf("Error closing Redis: %v", err)
			lastErr = err
		}

		// Cancel application context
		app.cancel()

		shutdownComplete <- lastErr
	}()

	// Wait for shutdown to complete or timeout
	select {
	case err := <-shutdownComplete:
		if err != nil {
			app.logger.Printf("⚠ Shutdown completed with errors: %v", err)
			return err
		}
		app.logger.Println("✓ Graceful shutdown completed successfully")
		return nil

	case <-shutdownCtx.Done():
		app.logger.Println("⚠ Shutdown timeout exceeded, forcing exit")
		return fmt.Errorf("shutdown timeout exceeded")
	}
}

// Run executes the application with signal handling
func (app *Application) Run() error {
	// Start the application
	if err := app.Start(); err != nil {
		return err
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-sigChan
	app.logger.Printf("Received signal: %v", sig)

	// Perform graceful shutdown
	return app.Stop()
}

func main() {
	// Create application instance
	app, err := NewApplication()
	if err != nil {
		log.Fatalf("❌ Failed to create application: %v", err)
	}

	// Run application
	if err := app.Run(); err != nil {
		log.Fatalf("❌ Application error: %v", err)
	}

	log.Println("👋 Application exited successfully")
}
