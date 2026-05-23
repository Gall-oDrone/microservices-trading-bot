package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/config"
	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/database"
	"bitso-trading-platform/shared/pkg/kafka"
	"bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/trading-engine/internal/engine"
	"bitso-trading-platform/trading-engine/internal/execution"
	"bitso-trading-platform/trading-engine/internal/metrics"
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
	logger              *log.Logger
	config              *config.Config
	broker              config.Broker
	bitsoClient         *bitso.Client
	etoroClient         *etoro.Client
	redisClient         *database.RedisClient
	kafkaConsumer       *kafka.Consumer
	orderPlacedProducer *kafka.Producer
	engine              *engine.TradingEngine
	metricsCollector    *metrics.Collector
	ctx                 context.Context
	cancel              context.CancelFunc
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

	broker := cfg.Broker
	var bitsoClient *bitso.Client
	var etoroClient *etoro.Client

	if broker.IsEtoro() {
		if err := cfg.ValidateEtoro(); err != nil {
			cancel()
			return nil, err
		}
		var err error
		etoroClient, err = initializeEtoroClient(cfg, logger)
		if err != nil {
			cancel()
			return nil, err
		}
		logger.Printf("✓ eToro API client initialized (env=%s)", etoroClient.Environment())
	} else {
		bitsoClient = initializeBitsoClient(cfg, logger)
		if bitsoClient == nil {
			cancel()
			return nil, fmt.Errorf("Bitso client is nil: set STAGE_BITSO_API_KEY and STAGE_BITSO_API_SECRET (e.g. from trading-secrets / AWS Secrets Manager)")
		}
		logger.Println("✓ Bitso API client initialized")
	}

	// Initialize Redis client
	redisClient, err := initializeRedisClient(cfg, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize Redis: %w", err)
	}
	logger.Println("✓ Redis client connected")

	// Pre-create required Kafka topics. Avoids a kafka-go race where the consumer joins
	// a group before the topic exists and ends up with 0 partition assignments.
	if cfg.KafkaBrokers != "" {
		topics := []kafka.TopicSpec{
			{Name: cfg.KafkaTopicSignals, NumPartitions: 1, ReplicationFactor: 1},
			{Name: cfg.KafkaTopicOrdersPlaced, NumPartitions: 1, ReplicationFactor: 1},
		}
		ensureCtx, ensureCancel := context.WithTimeout(ctx, 15*time.Second)
		if err := kafka.EnsureTopics(ensureCtx, []string{cfg.KafkaBrokers}, topics); err != nil {
			logger.Printf("EnsureTopics best-effort failed (continuing): %v", err)
		} else {
			logger.Printf("✓ Kafka topics ensured: %s, %s", cfg.KafkaTopicSignals, cfg.KafkaTopicOrdersPlaced)
		}
		ensureCancel()
	}

	// Initialize Kafka consumer for trade signals
	kafkaConsumer, err := initializeKafkaConsumer(cfg, logger)
	if err != nil {
		cancel()
		redisClient.Close()
		return nil, fmt.Errorf("failed to initialize Kafka consumer: %w", err)
	}
	logger.Println("✓ Kafka consumer initialized")

	tradingConfig := createTradingConfig(broker)
	bookLabel := tradingConfig.Book.String()
	if broker.IsEtoro() {
		bookLabel = os.Getenv("ETORO_DEFAULT_SYMBOL")
		if bookLabel == "" {
			bookLabel = "AAPL"
		}
	}
	logger.Printf("✓ Trading configuration: broker=%s book=%s strategy=%s",
		broker, bookLabel, tradingConfig.StrategyType)

	// Optional: Kafka producer for order-placed events (order-management sync)
	orderPlacedProducer, _ := initializeOrderPlacedProducer(cfg, logger)
	if orderPlacedProducer != nil {
		logger.Println("✓ Order-placed producer initialized (topic: " + cfg.KafkaTopicOrdersPlaced + ")")
	}

	// Optional: SessionRiskProvider for daily loss / drawdown limits (call order-management GET /api/v1/risk/session)
	// Optional: PreTradeValidator for pre-trade validation (call order-management POST /api/v1/orders/validate)
	var sessionRiskProvider execution.SessionRiskProvider
	var preTradeValidator execution.PreTradeValidator
	if orderMgmtURL := os.Getenv("ORDER_MANAGEMENT_URL"); orderMgmtURL != "" {
		sessionRiskProvider = execution.NewOrderManagementRiskProvider(orderMgmtURL)
		preTradeValidator = execution.NewHTTPPreTradeValidator(orderMgmtURL, 5*time.Second)
		logger.Println("✓ Session risk provider configured (ORDER_MANAGEMENT_URL)")
		logger.Println("✓ Pre-trade validator configured (ORDER_MANAGEMENT_URL)")
	}

	// Metrics collector for Prometheus (/metrics and balance/order gauges)
	metricsCollector := metrics.NewCollector()

	// Initialize trading engine
	tradingEngine, err := engine.NewTradingEngine(
		tradingConfig,
		cfg,
		engine.EngineClients{
			Broker:      broker,
			BitsoClient: bitsoClient,
			EtoroClient: etoroClient,
		},
		redisClient,
		kafkaConsumer,
		sessionRiskProvider,
		preTradeValidator,
		orderPlacedProducer,
		metricsCollector,
	)
	if err != nil {
		cancel()
		redisClient.Close()
		kafkaConsumer.Close()
		if orderPlacedProducer != nil {
			_ = orderPlacedProducer.Close()
		}
		return nil, fmt.Errorf("failed to create trading engine: %w", err)
	}
	logger.Println("✓ Trading engine created")

	return &Application{
		logger:              logger,
		config:              cfg,
		broker:              broker,
		bitsoClient:         bitsoClient,
		etoroClient:         etoroClient,
		redisClient:         redisClient,
		kafkaConsumer:       kafkaConsumer,
		orderPlacedProducer: orderPlacedProducer,
		engine:              tradingEngine,
		metricsCollector:    metricsCollector,
		ctx:                 ctx,
		cancel:              cancel,
	}, nil
}

// initializeBitsoClient creates and configures the Bitso API client.
// Returns nil if stage credentials are not set (e.g. BITSO_API_KEY/BITSO_API_SECRET
// or STAGE_BITSO_API_KEY/STAGE_BITSO_API_SECRET from AWS Secrets Manager / External Secrets).
func initializeBitsoClient(cfg *config.Config, logger *log.Logger) *bitso.Client {
	key := cfg.StageBitsoAPIKey
	secret := cfg.StageBitsoAPISecret
	if key == "" || secret == "" {
		logger.Printf("Bitso stage credentials missing (STAGE_BITSO_API_KEY / STAGE_BITSO_API_SECRET). Ensure trading-secrets (bitso-api-key, bitso-api-secret) are synced from AWS Secrets Manager.")
		return nil
	}
	client := bitso.NewClient()
	client.SetLogLevel(bitso.LogLevelInfo)
	client.SetAuth(key, secret)
	client.SetAPIBaseURL(cfg.BitsoAPIBaseURL)

	// Set rate limiting for API protection
	client.SetBurstRate(100 * time.Millisecond)

	return client
}

func initializeEtoroClient(cfg *config.Config, logger *log.Logger) (*etoro.Client, error) {
	env, err := etoro.ParseEnvironment(cfg.EtoroEnv)
	if err != nil {
		return nil, err
	}
	client, err := etoro.NewClient(etoro.Config{
		PublicKey:  cfg.EtoroPublicKey,
		PrivateKey: cfg.EtoroPrivateKey,
		Env:        env,
	})
	if err != nil {
		logger.Printf("eToro credentials missing (ETORO_PUBLIC_KEY / ETORO_PRIVATE_KEY). Ensure trading-secrets syncs etoro-public-key and etoro-private-key from AWS Secrets Manager.")
		return nil, err
	}
	return client, nil
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

// initializeOrderPlacedProducer creates a Kafka producer for order-placed events (optional; returns nil on failure).
func initializeOrderPlacedProducer(cfg *config.Config, logger *log.Logger) (*kafka.Producer, error) {
	if cfg.KafkaBrokers == "" || cfg.KafkaTopicOrdersPlaced == "" {
		return nil, nil
	}
	producerConfig := &kafka.ProducerConfig{
		Brokers: strings.Split(cfg.KafkaBrokers, ","),
		Topic:   cfg.KafkaTopicOrdersPlaced,
	}
	for i := range producerConfig.Brokers {
		producerConfig.Brokers[i] = strings.TrimSpace(producerConfig.Brokers[i])
	}
	producer, err := kafka.NewProducer(producerConfig)
	if err != nil {
		logger.Printf("Order-placed producer not started (optional): %v", err)
		return nil, nil
	}
	return producer, nil
}

// createTradingConfig creates the trading configuration
func createTradingConfig(broker config.Broker) *models.TradingConfig {
	book := bitso.NewBook(bitso.BTC, bitso.MXN)
	if broker.IsEtoro() {
		// Book is unused for execution; signals carry symbol in the book field (e.g. AAPL).
		book = bitso.NewBook(bitso.BTC, bitso.USD)
	}
	return &models.TradingConfig{
		Book:              book,
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

// startMetricsServer starts the HTTP server for /metrics and /health (Prometheus scraping).
func (app *Application) startMetricsServer(metricsHandler http.Handler) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metricsHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	server := &http.Server{Addr: ":8080", Handler: mux}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			app.logger.Printf("Metrics server error: %v", err)
		}
	}()
	app.logger.Println("✓ Metrics server listening on :8080 (/metrics, /health)")
}

// Start initializes and starts the application
func (app *Application) Start() error {
	app.logger.Println("Starting application components...")

	// Start metrics server first so /metrics is available before engine runs
	app.startMetricsServer(app.metricsCollector.Handler())

	// Set dry-run gauge for Prometheus (1=dry-run, 0=live)
	app.metricsCollector.SetDryRun(app.config.DryRun)

	// Optional: set Available Balance metric from env for testing (e.g. BALANCE_TEST_MXN=50000)
	if testBalances := getTestBalancesFromEnv(); len(testBalances) > 0 {
		app.metricsCollector.RecordBalances(testBalances)
		app.logger.Printf("✓ Test balances set from env for Grafana: %v", testBalances)
	}

	// Initialize trading engine
	if err := app.engine.Initialize(); err != nil {
		if len(getTestBalancesFromEnv()) > 0 {
			// Test mode: keep process running so Prometheus can scrape /metrics (e.g. Available Balance)
			app.logger.Printf("⚠ Trading engine init failed (%v); staying up for metrics only (BALANCE_TEST_* set)", err)
			app.logger.Printf("🚀 %s running in metrics-only mode; /metrics and /health available", appName)
			return nil
		}
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

		if app.orderPlacedProducer != nil {
			app.logger.Println("Closing order-placed producer...")
			if err := app.orderPlacedProducer.Close(); err != nil {
				app.logger.Printf("Error closing order-placed producer: %v", err)
				lastErr = err
			}
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

// getTestBalancesFromEnv returns a map of currency -> amount for testing the Available Balance metric in Grafana.
// Set env vars like BALANCE_TEST_MXN=50000, BALANCE_TEST_USD=1000. If none set, returns nil.
func getTestBalancesFromEnv() map[string]float64 {
	currencies := []string{"MXN", "USD", "BTC", "ETH"}
	out := make(map[string]float64)
	for _, c := range currencies {
		key := "BALANCE_TEST_" + c
		if v := os.Getenv(key); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
				out[c] = f
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
