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

	"bitso_trading_bot/internal/config"
	"bitso_trading_bot/internal/database"
	"bitso_trading_bot/internal/models"
	"bitso_trading_bot/internal/trading_bot"
	"bitso_trading_bot/pkg/bitso"
)

// Application represents the main application structure
type Application struct {
	config      *config.Config
	bitsoClient *bitso.Client
	redisClient *database.RedisClient
	ctx         context.Context
	cancel      context.CancelFunc
	logger      *log.Logger
}

// NewApplication creates a new instance of the application
func NewApplication() (*Application, error) {
	// Initialize logger
	logger := log.New(os.Stdout, "[APP] ", log.LstdFlags|log.Lshortfile)

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %v", err)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize Bitso client
	bitsoClient := bitso.NewClient()
	bitsoClient.SetLogLevel(bitso.LogLevelDebug)
	bitsoClient.SetAuth(cfg.StageBitsoAPIKey, cfg.StageBitsoAPISecret)
	bitsoClient.SetAPIBaseURL("https://stage.bitso.com/api")

	// Initialize Redis client with custom configuration
	redisPort, err := strconv.Atoi(cfg.RedisPort)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("invalid Redis port: %v", err)
	}

	redisClient, err := database.InitializeWithConfig(
		cfg.RedisHost,
		redisPort,
		cfg.RedisPassword,
		cfg.RedisDB,
		10, // Default pool size
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize Redis client: %v", err)
	}

	return &Application{
		config:      cfg,
		bitsoClient: bitsoClient,
		redisClient: redisClient,
		ctx:         ctx,
		cancel:      cancel,
		logger:      logger,
	}, nil
}

// Start initializes and starts the application
func (app *Application) Start() error {
	app.logger.Println("Starting application...")

	// Create trading configuration
	tradingConfig := &models.TradingConfig{
		Book:              bitso.NewBook(bitso.BTC, bitso.USD), // Example book
		MinTradeAmount:    0.001,                               // Example minimum trade amount
		MaxTradeAmount:    0.1,                                 // Example maximum trade amount
		MaxTradeValue:     1000.0,                              // Example maximum trade value
		StopLossPercent:   2.0,                                 // Example stop loss percentage
		TakeProfitPercent: 3.0,                                 // Example take profit percentage
		MaxTradingTime:    24 * time.Hour,                      // Maximum time to keep a position open
		StartTime:         time.Now(),                          // Start trading now
		EndTime:           time.Now().Add(24 * time.Hour),      // End trading in 24 hours
		MaxOpenPositions:  3,                                   // Maximum number of open positions
		StrategyType:      "basic",                             // Basic trading strategy
		Parameters:        make(map[string]interface{}),        // Strategy parameters
	}

	// Create and initialize trading bot
	bot := trading_bot.NewTradingBot(tradingConfig, app.bitsoClient, app.redisClient)
	if err := bot.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize trading bot: %v", err)
	}

	// Start the trading bot
	if err := bot.Start(); err != nil {
		return fmt.Errorf("failed to start trading bot: %v", err)
	}

	app.logger.Println("Trading bot started successfully")
	return nil
}

// Stop gracefully shuts down the application
func (app *Application) Stop() error {
	app.logger.Println("Stopping application...")

	// Cancel the context
	app.cancel()

	// Close Redis connection
	if err := app.redisClient.Close(); err != nil {
		return fmt.Errorf("error closing Redis connection: %v", err)
	}

	return nil
}

// Run executes the application with proper signal handling
func (app *Application) Run() error {
	// Start the application
	if err := app.Start(); err != nil {
		return fmt.Errorf("failed to start application: %v", err)
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigChan

	// Gracefully shutdown
	if err := app.Stop(); err != nil {
		return fmt.Errorf("failed to stop application: %v", err)
	}

	app.logger.Println("Application stopped successfully")
	return nil
}

func main() {
	// Create new application instance
	app, err := NewApplication()
	if err != nil {
		log.Fatalf("Failed to create application: %v", err)
	}

	// Run the application
	if err := app.Run(); err != nil {
		log.Fatalf("Application error: %v", err)
	}
}
