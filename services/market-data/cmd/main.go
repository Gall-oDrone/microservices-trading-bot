package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bitso-trading-platform/market-data/internal/config"
	"bitso-trading-platform/market-data/internal/processor"
	"bitso-trading-platform/market-data/internal/publisher"
	"bitso-trading-platform/market-data/internal/websocket"
	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/kafka"
)

const (
	appName    = "market-data"
	appVersion = "1.0.0"

	shutdownTimeout = 30 * time.Second
)

// Application encapsulates all application components
type Application struct {
	logger *log.Logger
	config *config.Config

	// Components
	wsManager      websocket.StreamManager
	tradeProcessor processor.TradeProcessor
	tradePublisher publisher.TradePublisher
	kafkaProducer  *kafka.Producer

	// Context
	ctx    context.Context
	cancel context.CancelFunc
}

// NewApplication creates and initializes the application
func NewApplication() (*Application, error) {
	// Initialize logger
	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", appName), log.LstdFlags|log.Lshortfile)
	logger.Printf("Starting %s v%s", appName, appVersion)

	// Create context
	ctx, cancel := context.WithCancel(context.Background())

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	logger.Println("✓ Configuration loaded")

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
		logger.Println("✓ Kafka producer initialized")
	}

	// Initialize WebSocket Manager
	wsManagerConfig := &websocket.ManagerConfig{
		ReconnectAttempts: cfg.WSReconnectAttempts,
		ReconnectInterval: cfg.WSReconnectInterval,
		ReconnectMaxDelay: cfg.WSReconnectMaxDelay,
		Logger:            logger,
	}
	wsManager := websocket.NewManager(wsManagerConfig)
	logger.Println("✓ WebSocket manager created")

	// Initialize Trade Processor
	processorConfig := &processor.ProcessorConfig{
		Logger:       logger,
		TradesInput:  wsManager.GetTradesStream(),
		OutputBuffer: 100,
	}
	tradeProcessor := processor.NewProcessor(processorConfig)
	logger.Println("✓ Trade processor created")

	// Initialize Trade Publisher
	var tradePublisher publisher.TradePublisher
	if cfg.EnableKafka && kafkaProducer != nil {
		publisherConfig := &publisher.PublisherConfig{
			Logger:      logger,
			Producer:    kafkaProducer,
			Topic:       cfg.KafkaTopicTrades,
			TradesInput: tradeProcessor.GetProcessedTradesStream(),
		}
		tradePublisher = publisher.NewPublisher(publisherConfig)
		logger.Println("✓ Trade publisher created")
	}

	return &Application{
		logger:         logger,
		config:         cfg,
		wsManager:      wsManager,
		tradeProcessor: tradeProcessor,
		tradePublisher: tradePublisher,
		kafkaProducer:  kafkaProducer,
		ctx:            ctx,
		cancel:         cancel,
	}, nil
}

// Start initializes and starts all components
func (app *Application) Start() error {
	app.logger.Println("Starting application components...")

	// Parse trading books
	books := make([]*bitso.Book, 0, len(app.config.BitsoBooks))
	for _, bookStr := range app.config.BitsoBooks {
		book, err := parseBook(bookStr)
		if err != nil {
			return fmt.Errorf("failed to parse book %s: %w", bookStr, err)
		}
		books = append(books, book)
	}
	app.logger.Printf("✓ Trading books: %v", app.config.BitsoBooks)

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
	app.logger.Println("✓ WebSocket manager started")

	// Start trade processor
	if err := app.tradeProcessor.Start(app.ctx); err != nil {
		return fmt.Errorf("failed to start trade processor: %w", err)
	}
	app.logger.Println("✓ Trade processor started")

	// Start trade publisher
	if app.tradePublisher != nil {
		if err := app.tradePublisher.Start(app.ctx); err != nil {
			return fmt.Errorf("failed to start trade publisher: %w", err)
		}
		app.logger.Println("✓ Trade publisher started")
	}

	app.logger.Printf("🚀 %s is now running and streaming BTC/MXN market data", appName)
	return nil
}

// Stop gracefully shuts down the application
func (app *Application) Stop() error {
	app.logger.Println("Initiating graceful shutdown...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	shutdownComplete := make(chan error, 1)

	go func() {
		var lastErr error

		// Stop trade publisher
		if app.tradePublisher != nil {
			app.logger.Println("Stopping trade publisher...")
			if err := app.tradePublisher.Stop(); err != nil {
				app.logger.Printf("Error stopping trade publisher: %v", err)
				lastErr = err
			}
		}

		// Stop trade processor
		app.logger.Println("Stopping trade processor...")
		if err := app.tradeProcessor.Stop(); err != nil {
			app.logger.Printf("Error stopping trade processor: %v", err)
			lastErr = err
		}

		// Stop WebSocket manager
		app.logger.Println("Stopping WebSocket manager...")
		if err := app.wsManager.Stop(); err != nil {
			app.logger.Printf("Error stopping WebSocket manager: %v", err)
			lastErr = err
		}

		// Close Kafka producer
		if app.kafkaProducer != nil {
			app.logger.Println("Closing Kafka producer...")
			if err := app.kafkaProducer.Close(); err != nil {
				app.logger.Printf("Error closing Kafka producer: %v", err)
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

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-sigChan
	app.logger.Printf("Received signal: %v", sig)

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
		log.Fatalf("❌ Failed to create application: %v", err)
	}

	// Run application
	if err := app.Run(); err != nil {
		log.Fatalf("❌ Application error: %v", err)
	}

	log.Println("👋 Application exited successfully")
}
