package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/config"
	"bitso-trading-platform/shared/pkg/database"
	"bitso-trading-platform/shared/pkg/kafka"
	"bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/trading-engine/internal/execution"
)

// MetricsRecorder is an optional interface for recording Prometheus metrics.
// Implementations can record order executions, balance updates, signals, Kafka, and engine state.
type MetricsRecorder interface {
	RecordOrderExecuted(book, strategy string)
	RecordOrderFailed(book, strategy, reason string)
	ObserveOrderExecutionDuration(book, strategy string, d time.Duration)
	RecordBalances(currencyToAvailable map[string]float64)
	RecordSignalReceived()
	RecordSignalsProcessed(book, strategy, outcome string)
	RecordSignalsDropped(reason string)
	ObserveSignalProcessingDuration(d time.Duration)
	RecordBalanceFetchError()
	SetBalanceLastSuccessTimestamp(ts float64)
	RecordSessionRiskCheck(result string)
	RecordSessionRiskRejection()
	RecordKafkaMessageConsumed(topic string)
	RecordKafkaConsumerError()
	RecordOrderPlacedPublished()
	RecordOrderPlacedPublishError()
	SetEngineState(state float64)
	SetDryRun(dryRun bool)
	RecordHealthCheckFailure()
}

// orderPlacedEvent is published to Kafka for order-management sync
type orderPlacedEvent struct {
	OrderID  string  `json:"order_id"`
	Book     string  `json:"book"`
	Side     string  `json:"side"`
	Amount   float64 `json:"amount"`
	Price    float64 `json:"price"`
	Strategy string  `json:"strategy,omitempty"`
}

// EngineState represents the current state of the trading engine
type EngineState int

const (
	StateInitializing EngineState = iota
	StateRunning
	StatePaused
	StateStopping
	StateStopped
)

// String returns the string representation of the engine state
func (s EngineState) String() string {
	return [...]string{"Initializing", "Running", "Paused", "Stopping", "Stopped"}[s]
}

// TradingEngine represents the core trading engine
type TradingEngine struct {
	// Configuration
	config    *models.TradingConfig
	appConfig *config.Config

	// External clients
	bitsoClient         *bitso.Client
	dbClient            *database.RedisClient
	kafkaConsumer       *kafka.Consumer
	orderPlacedProducer *kafka.Producer // optional: publish to trading.orders.placed for order-management

	// Internal components
	executor            execution.Executor
	sessionRiskProvider execution.SessionRiskProvider // optional: for daily loss / drawdown limits
	metricsRecorder     MetricsRecorder              // optional: for Prometheus metrics
	book                *bitso.Book

	// State management
	state      EngineState
	stateMutex sync.RWMutex

	// Concurrency control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Channels
	signalChan chan *models.TradeSignalEvent
	errorChan  chan error
	stopChan   chan struct{}

	// Statistics
	stats      *EngineStatistics
	statsMutex sync.RWMutex

	// Logging
	logger *log.Logger
}

// EngineStatistics tracks engine performance metrics
type EngineStatistics struct {
	StartTime        time.Time
	SignalsProcessed int64
	SignalsSucceeded int64
	SignalsFailed    int64
	OrdersPlaced     int64
	OrdersFailed     int64
	LastSignalTime   time.Time
	LastErrorTime    time.Time
	LastError        string
}

// NewTradingEngine creates a new trading engine instance.
// sessionRiskProvider is optional; if set, used to enforce MaxDailyLoss/MaxDrawdownPct before placing orders.
// orderPlacedProducer is optional; if set, placed orders are published to Kafka for order-management sync.
// metricsRecorder is optional; if set, order executions and balance updates are recorded for Prometheus.
func NewTradingEngine(
	tradingConfig *models.TradingConfig,
	appConfig *config.Config,
	bitsoClient *bitso.Client,
	dbClient *database.RedisClient,
	kafkaConsumer *kafka.Consumer,
	sessionRiskProvider execution.SessionRiskProvider,
	orderPlacedProducer *kafka.Producer,
	metricsRecorder MetricsRecorder,
) (*TradingEngine, error) {
	// Validate inputs
	if tradingConfig == nil {
		return nil, fmt.Errorf("trading config cannot be nil")
	}
	if bitsoClient == nil {
		return nil, fmt.Errorf("bitso client cannot be nil")
	}
	if dbClient == nil {
		return nil, fmt.Errorf("database client cannot be nil")
	}
	if kafkaConsumer == nil {
		return nil, fmt.Errorf("kafka consumer cannot be nil")
	}

	// Create context
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize logger
	logger := log.New(log.Writer(), "[ENGINE] ", log.LstdFlags|log.Lshortfile)

	// Create executor (trading config for session limits; dry-run from app config)
	executor := execution.NewBasicExecutor(bitsoClient, tradingConfig, appConfig.DryRun)

	engine := &TradingEngine{
		config:        tradingConfig,
		appConfig:     appConfig,
		bitsoClient:   bitsoClient,
		dbClient:      dbClient,
		kafkaConsumer: kafkaConsumer,
		executor:              executor,
		sessionRiskProvider:   sessionRiskProvider,
		orderPlacedProducer:   orderPlacedProducer,
		metricsRecorder:       metricsRecorder,
		book:                  tradingConfig.Book,
		state:                 StateInitializing,
		ctx:           ctx,
		cancel:        cancel,
		signalChan:    make(chan *models.TradeSignalEvent, 100),
		errorChan:     make(chan error, 10),
		stopChan:      make(chan struct{}),
		stats: &EngineStatistics{
			StartTime: time.Now(),
		},
		logger: logger,
	}

	if metricsRecorder != nil {
		metricsRecorder.SetEngineState(1) // initializing
	}
	return engine, nil
}

// signalsTopic returns the Kafka signals topic for metrics (default trading.signals).
func (te *TradingEngine) signalsTopic() string {
	if te.appConfig != nil && te.appConfig.KafkaTopicSignals != "" {
		return te.appConfig.KafkaTopicSignals
	}
	return "trading.signals"
}

// Initialize sets up the trading engine
func (te *TradingEngine) Initialize() error {
	te.logger.Println("Initializing trading engine...")

	// Validate trading configuration
	if err := te.config.Validate(); err != nil {
		return fmt.Errorf("invalid trading configuration: %w", err)
	}

	// Check trading hours
	if !te.config.IsWithinTradingHours() {
		te.logger.Printf("Warning: Current time is outside trading hours (%v - %v)",
			te.config.StartTime, te.config.EndTime)
	}

	// Fetch and cache initial balances
	if err := te.fetchAndCacheBalances(); err != nil {
		return fmt.Errorf("failed to fetch balances: %w", err)
	}

	// Verify Bitso API connectivity
	if err := te.verifyBitsoConnection(); err != nil {
		return fmt.Errorf("bitso connectivity check failed: %w", err)
	}

	// Verify Redis connectivity
	if err := te.dbClient.Ping(te.ctx); err != nil {
		return fmt.Errorf("redis connectivity check failed: %w", err)
	}

	te.logger.Printf("Engine initialized for trading pair: %s", te.book.String())
	te.logger.Printf("Trade limits: Min=%.8f, Max=%.8f, MaxValue=%.2f",
		te.config.MinTradeAmount,
		te.config.MaxTradeAmount,
		te.config.MaxTradeValue)
	te.logger.Printf("Risk params: StopLoss=%.2f%%, TakeProfit=%.2f%%",
		te.config.StopLossPercent,
		te.config.TakeProfitPercent)

	return nil
}

// Start begins the trading engine operation
func (te *TradingEngine) Start() error {
	te.stateMutex.Lock()
	if te.state != StateInitializing {
		te.stateMutex.Unlock()
		return fmt.Errorf("engine must be in initializing state to start")
	}
	te.state = StateRunning
	te.stateMutex.Unlock()
	if te.metricsRecorder != nil {
		te.metricsRecorder.SetEngineState(2) // running
	}

	te.logger.Println("Starting trading engine...")

	// Start signal processor
	te.wg.Add(1)
	go te.signalProcessor()

	// Start Kafka consumer
	te.wg.Add(1)
	go te.kafkaConsumerLoop()

	// Start health monitor
	te.wg.Add(1)
	go te.healthMonitor()

	// Start statistics reporter
	te.wg.Add(1)
	go te.statisticsReporter()

	te.logger.Println("Trading engine is now running")
	return nil
}

// Stop gracefully stops the trading engine
func (te *TradingEngine) Stop() error {
	te.stateMutex.Lock()
	if te.state == StateStopping || te.state == StateStopped {
		te.stateMutex.Unlock()
		return nil
	}
	te.state = StateStopping
	te.stateMutex.Unlock()

	te.logger.Println("Stopping trading engine...")

	// Cancel context first to signal all operations
	te.cancel()

	// Signal all goroutines to stop
	close(te.stopChan)

	// Wait for all goroutines to finish (with timeout)
	done := make(chan struct{})
	go func() {
		te.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		te.logger.Println("All engine goroutines stopped")
	case <-time.After(10 * time.Second):
		te.logger.Println("Warning: Some goroutines did not stop within timeout")
	}

	// Close channels only after all goroutines have stopped
	// This prevents panics from sending to closed channels
	close(te.signalChan)
	close(te.errorChan)

	te.stateMutex.Lock()
	te.state = StateStopped
	te.stateMutex.Unlock()
	if te.metricsRecorder != nil {
		te.metricsRecorder.SetEngineState(0) // stopped
	}

	te.logger.Println("Trading engine stopped")
	return nil
}

// kafkaConsumerLoop continuously consumes messages from Kafka
func (te *TradingEngine) kafkaConsumerLoop() {
	defer te.wg.Done()
	te.logger.Println("Kafka consumer loop started")

	for {
		select {
		case <-te.stopChan:
			te.logger.Println("Kafka consumer loop stopping")
			return

		case <-te.ctx.Done():
			te.logger.Println("Kafka consumer loop cancelled")
			return

		default:
			// Consume message with timeout (must exceed shared kafka Consumer MaxWait so fetches can complete)
			ctx, cancel := context.WithTimeout(te.ctx, 30*time.Second)
			msg, err := te.kafkaConsumer.Consume(ctx)
			cancel()

			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					continue
				}
				var ke kafkago.Error
				if errors.As(err, &ke) && ke.Timeout() {
					// Idle topic / broker fetch window — not a fault condition
					continue
				}
				te.logger.Printf("Error consuming message: %v", err)
				te.recordError(err)
				if te.metricsRecorder != nil {
					te.metricsRecorder.RecordKafkaConsumerError()
				}
				time.Sleep(1 * time.Second) // Back off on error
				continue
			}

			// Parse trade signal
			signal, err := te.parseTradeSignal(msg)
			if err != nil {
				te.logger.Printf("Error parsing trade signal: %v", err)
				te.recordError(err)
				continue
			}
			if te.metricsRecorder != nil {
				te.metricsRecorder.RecordSignalReceived()
				te.metricsRecorder.RecordKafkaMessageConsumed(te.signalsTopic())
			}

			// Send to signal channel for processing with timeout
			select {
			case te.signalChan <- signal:
				te.logger.Printf("Received signal: %s %s at %.2f",
					signal.Signal, signal.Book, signal.Price)
			case <-te.stopChan:
				te.logger.Println("Stop signal received while sending to signal channel")
				return
			case <-te.ctx.Done():
				te.logger.Println("Context cancelled while sending to signal channel")
				return
			case <-time.After(5 * time.Second):
				te.logger.Printf("Timeout sending signal to channel, dropping message")
				te.recordError(fmt.Errorf("timeout sending signal to channel"))
				if te.metricsRecorder != nil {
					te.metricsRecorder.RecordSignalsDropped("timeout")
				}
			}
		}
	}
}

// signalProcessor processes trade signals from the channel
func (te *TradingEngine) signalProcessor() {
	defer te.wg.Done()
	te.logger.Println("Signal processor started")

	for {
		select {
		case <-te.stopChan:
			te.logger.Println("Signal processor stopping")
			return

		case signal, ok := <-te.signalChan:
			if !ok {
				te.logger.Println("Signal channel closed")
				return
			}

			start := time.Now()
			bookStr := signal.Book
			strategy := te.config.StrategyType
			err := te.processTradeSignal(signal)
			duration := time.Since(start)
			if te.metricsRecorder != nil {
				te.metricsRecorder.ObserveSignalProcessingDuration(duration)
				if err != nil {
					te.metricsRecorder.RecordSignalsProcessed(bookStr, strategy, "failed")
				} else {
					te.metricsRecorder.RecordSignalsProcessed(bookStr, strategy, "success")
				}
			}
			if err != nil {
				te.logger.Printf("Error processing signal: %v", err)
				te.recordError(err)
				te.updateStats(false)
			} else {
				te.updateStats(true)
			}
		}
	}
}

// processTradeSignal processes a single trade signal
func (te *TradingEngine) processTradeSignal(signal *models.TradeSignalEvent) error {
	te.logger.Printf("Processing %s signal for %s at price %.2f",
		signal.Signal, signal.Book, signal.Price)

	// Check if we're in trading hours
	if !te.config.IsWithinTradingHours() {
		te.recordOrderFailedIfMetrics(signal.Book, "validation")
		return fmt.Errorf("signal received outside trading hours")
	}

	// Check current state
	if te.GetState() != StateRunning {
		te.recordOrderFailedIfMetrics(signal.Book, "validation")
		return fmt.Errorf("engine not in running state")
	}

	// Parse book from signal
	book, err := te.parseBook(signal.Book)
	if err != nil {
		te.recordOrderFailedIfMetrics(signal.Book, "validation")
		return fmt.Errorf("invalid book: %w", err)
	}

	// Get current ticker for validation
	ticker, err := te.bitsoClient.Ticker(book)
	if err != nil {
		te.recordOrderFailedIfMetrics(signal.Book, "ticker_fetch")
		return fmt.Errorf("failed to get ticker: %w", err)
	}

	// Validate signal price is reasonable
	if err := te.validateSignalPrice(signal, ticker); err != nil {
		te.recordOrderFailedIfMetrics(signal.Book, "validation")
		return fmt.Errorf("signal validation failed: %w", err)
	}

	// Session risk check (daily loss / drawdown limits)
	var dailyPnL, drawdownPct float64
	if te.sessionRiskProvider != nil {
		var err error
		dailyPnL, drawdownPct, err = te.sessionRiskProvider.GetSessionRisk(te.ctx)
		if err != nil {
			te.recordOrderFailedIfMetrics(signal.Book, "session_risk")
			return fmt.Errorf("session risk check: %w", err)
		}
	}
	if err := te.executor.CheckSessionLimits(dailyPnL, drawdownPct); err != nil {
		if te.metricsRecorder != nil {
			te.metricsRecorder.RecordSessionRiskCheck("rejected")
			te.metricsRecorder.RecordSessionRiskRejection()
		}
		te.recordOrderFailedIfMetrics(signal.Book, "session_risk")
		return err
	}
	if te.metricsRecorder != nil {
		te.metricsRecorder.RecordSessionRiskCheck("allowed")
	}

	// Create trading signal for executor
	tradeSignal := execution.TradingSignal{
		Book:      book,
		Amount:    signal.Amount,
		Price:     signal.Price,
		Reason:    fmt.Sprintf("%v", signal.Metadata["reason"]),
		Timestamp: signal.Timestamp,
	}

	bookStr := book.String()
	strategy := te.config.StrategyType

	// Execute based on signal type
	var orderID string
	var execErr error
	execStart := time.Now()
	switch signal.Signal {
	case "BUY":
		tradeSignal.Type = execution.SignalBuy
		orderID, execErr = te.executor.ExecuteBuySignal(tradeSignal)
		if execErr == nil {
			te.incrementOrdersPlaced()
			if te.metricsRecorder != nil {
				te.metricsRecorder.ObserveOrderExecutionDuration(bookStr, strategy, time.Since(execStart))
				te.metricsRecorder.RecordOrderExecuted(bookStr, strategy)
			}
		} else {
			te.incrementOrdersFailed()
			if te.metricsRecorder != nil {
				te.metricsRecorder.RecordOrderFailed(bookStr, strategy, "bitso_api")
			}
		}

	case "SELL":
		tradeSignal.Type = execution.SignalSell
		orderID, execErr = te.executor.ExecuteSellSignal(tradeSignal)
		if execErr == nil {
			te.incrementOrdersPlaced()
			if te.metricsRecorder != nil {
				te.metricsRecorder.ObserveOrderExecutionDuration(bookStr, strategy, time.Since(execStart))
				te.metricsRecorder.RecordOrderExecuted(bookStr, strategy)
			}
		} else {
			te.incrementOrdersFailed()
			if te.metricsRecorder != nil {
				te.metricsRecorder.RecordOrderFailed(bookStr, strategy, "bitso_api")
			}
		}

	default:
		te.recordOrderFailedIfMetrics(signal.Book, "validation")
		return fmt.Errorf("unknown signal type: %s", signal.Signal)
	}

	if execErr != nil {
		return execErr
	}
	// Publish order-placed event for order-management sync (Phase 3)
	if orderID != "" && orderID != "dry-run" && te.orderPlacedProducer != nil {
		te.publishOrderPlaced(orderID, signal, book, signal.Signal)
	}
	return nil
}

// recordOrderFailedIfMetrics records orders_failed_total when metricsRecorder is set. bookOrFallback is signal.Book (may be invalid).
func (te *TradingEngine) recordOrderFailedIfMetrics(bookOrFallback, reason string) {
	if te.metricsRecorder == nil {
		return
	}
	bookStr := bookOrFallback
	if bookStr == "" {
		bookStr = "unknown"
	}
	te.metricsRecorder.RecordOrderFailed(bookStr, te.config.StrategyType, reason)
}

func (te *TradingEngine) publishOrderPlaced(orderID string, signal *models.TradeSignalEvent, book *bitso.Book, side string) {
	evt := orderPlacedEvent{
		OrderID:  orderID,
		Book:     book.String(),
		Side:     side,
		Amount:   signal.Amount,
		Price:    signal.Price,
		Strategy: te.config.StrategyType,
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		te.logger.Printf("Failed to marshal order-placed event: %v", err)
		if te.metricsRecorder != nil {
			te.metricsRecorder.RecordOrderPlacedPublishError()
		}
		return
	}
	ctx, cancel := context.WithTimeout(te.ctx, 5*time.Second)
	defer cancel()
	if err := te.orderPlacedProducer.Produce(ctx, []byte(orderID), payload); err != nil {
		te.logger.Printf("Failed to publish order-placed event: %v", err)
		if te.metricsRecorder != nil {
			te.metricsRecorder.RecordOrderPlacedPublishError()
		}
		return
	}
	if te.metricsRecorder != nil {
		te.metricsRecorder.RecordOrderPlacedPublished()
	}
}

// healthMonitor periodically checks engine health
func (te *TradingEngine) healthMonitor() {
	defer te.wg.Done()
	te.logger.Println("Health monitor started")

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-te.stopChan:
			te.logger.Println("Health monitor stopping")
			return

		case <-ticker.C:
			if err := te.performHealthCheck(); err != nil {
				te.logger.Printf("Health check failed: %v", err)
				te.recordError(err)
			}
		}
	}
}

// statisticsReporter periodically logs engine statistics
func (te *TradingEngine) statisticsReporter() {
	defer te.wg.Done()
	te.logger.Println("Statistics reporter started")

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-te.stopChan:
			te.logger.Println("Statistics reporter stopping")
			te.logStatistics() // Log final stats
			return

		case <-ticker.C:
			te.logStatistics()
		}
	}
}

// Helper methods

func (te *TradingEngine) fetchAndCacheBalances() error {
	te.logger.Println("Fetching account balances...")

	balances, err := te.bitsoClient.Balances(nil)
	if err != nil {
		if te.metricsRecorder != nil {
			te.metricsRecorder.RecordBalanceFetchError()
		}
		// For testing: if BALANCE_TEST_* env vars are set, record them so Grafana shows data without Bitso
		if testBalances := getTestBalancesFromEnv(); len(testBalances) > 0 {
			te.logger.Printf("Bitso fetch failed (%v); using test balances from env for metrics: %v", err, testBalances)
			if te.metricsRecorder != nil {
				te.metricsRecorder.RecordBalances(testBalances)
			}
			return nil
		}
		return fmt.Errorf("failed to fetch balances: %w", err)
	}

	te.logger.Printf("Retrieved %d currency balances", len(balances))
	if te.metricsRecorder != nil {
		te.metricsRecorder.SetBalanceLastSuccessTimestamp(float64(time.Now().Unix()))
	}

	// Cache balances in Redis and record for Prometheus
	currencyToAvailable := make(map[string]float64)
	for _, balance := range balances {
		if err := te.dbClient.SaveUserBalance(&balance); err != nil {
			te.logger.Printf("Warning: Failed to cache balance for %s: %v",
				balance.Currency.String(), err)
		}
		currencyToAvailable[balance.Currency.String()] = balance.Available.Float64()
	}
	if te.metricsRecorder != nil {
		te.metricsRecorder.RecordBalances(currencyToAvailable)
	}

	return nil
}

// getTestBalancesFromEnv returns BALANCE_TEST_* env vars for testing (e.g. BALANCE_TEST_MXN=50000).
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
	return out
}

func (te *TradingEngine) verifyBitsoConnection() error {
	_, err := te.bitsoClient.Ticker(te.book)
	return err
}

func (te *TradingEngine) parseTradeSignal(data []byte) (*models.TradeSignalEvent, error) {
	var signal models.TradeSignalEvent
	if err := json.Unmarshal(data, &signal); err != nil {
		return nil, fmt.Errorf("failed to unmarshal signal: %w", err)
	}
	return &signal, nil
}

func (te *TradingEngine) parseBook(bookStr string) (*bitso.Book, error) {
	// Parse book string format: "btc_mxn" -> BTC/MXN
	if bookStr == "" {
		return nil, fmt.Errorf("book string is empty")
	}

	// Split by underscore
	parts := strings.Split(strings.ToLower(bookStr), "_")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid book format: %s (expected format: major_minor, e.g., btc_mxn)", bookStr)
	}

	// Parse currencies using bitso.ToCurrency
	major := bitso.ToCurrency(parts[0])
	minor := bitso.ToCurrency(parts[1])

	// Validate currencies are not empty
	if major == bitso.CurrencyNone || minor == bitso.CurrencyNone {
		return nil, fmt.Errorf("invalid currency in book: %s", bookStr)
	}

	return bitso.NewBook(major, minor), nil
}

func (te *TradingEngine) validateSignalPrice(signal *models.TradeSignalEvent, ticker *bitso.Ticker) error {
	currentBid := ticker.Bid.Float64()
	currentAsk := ticker.Ask.Float64()

	// Check if signal price is within reasonable range (5% of current prices)
	tolerance := 0.05

	if signal.Signal == "BUY" {
		if signal.Price > currentAsk*(1+tolerance) {
			return fmt.Errorf("buy price %.2f too high (ask: %.2f)", signal.Price, currentAsk)
		}
	} else if signal.Signal == "SELL" {
		if signal.Price < currentBid*(1-tolerance) {
			return fmt.Errorf("sell price %.2f too low (bid: %.2f)", signal.Price, currentBid)
		}
	}

	return nil
}

func (te *TradingEngine) performHealthCheck() error {
	// Check Redis
	if err := te.dbClient.Ping(te.ctx); err != nil {
		if te.metricsRecorder != nil {
			te.metricsRecorder.RecordHealthCheckFailure()
		}
		return fmt.Errorf("redis health check failed: %w", err)
	}

	// Check Bitso API
	if _, err := te.bitsoClient.Ticker(te.book); err != nil {
		if te.metricsRecorder != nil {
			te.metricsRecorder.RecordHealthCheckFailure()
		}
		return fmt.Errorf("bitso health check failed: %w", err)
	}

	return nil
}

func (te *TradingEngine) recordError(err error) {
	te.statsMutex.Lock()
	defer te.statsMutex.Unlock()

	te.stats.LastErrorTime = time.Now()
	te.stats.LastError = err.Error()
	te.stats.SignalsFailed++

	// Try to send error to error channel (non-blocking)
	select {
	case te.errorChan <- err:
		// Error sent successfully
	default:
		// Channel is full or closed, log it
		te.logger.Printf("Error channel full or closed, couldn't send error: %v", err)
	}
}

func (te *TradingEngine) updateStats(success bool) {
	te.statsMutex.Lock()
	defer te.statsMutex.Unlock()

	te.stats.SignalsProcessed++
	te.stats.LastSignalTime = time.Now()

	if success {
		te.stats.SignalsSucceeded++
	} else {
		te.stats.SignalsFailed++
	}
}

func (te *TradingEngine) incrementOrdersPlaced() {
	te.statsMutex.Lock()
	defer te.statsMutex.Unlock()
	te.stats.OrdersPlaced++
}

func (te *TradingEngine) incrementOrdersFailed() {
	te.statsMutex.Lock()
	defer te.statsMutex.Unlock()
	te.stats.OrdersFailed++
}

func (te *TradingEngine) logStatistics() {
	te.statsMutex.RLock()
	defer te.statsMutex.RUnlock()

	uptime := time.Since(te.stats.StartTime)
	successRate := float64(0)
	if te.stats.SignalsProcessed > 0 {
		successRate = float64(te.stats.SignalsSucceeded) / float64(te.stats.SignalsProcessed) * 100
	}

	te.logger.Printf("=== Engine Statistics ===")
	te.logger.Printf("Uptime: %v", uptime.Round(time.Second))
	te.logger.Printf("Signals Processed: %d (Success: %d, Failed: %d, Rate: %.2f%%)",
		te.stats.SignalsProcessed,
		te.stats.SignalsSucceeded,
		te.stats.SignalsFailed,
		successRate)
	te.logger.Printf("Orders: Placed=%d, Failed=%d",
		te.stats.OrdersPlaced,
		te.stats.OrdersFailed)

	if !te.stats.LastSignalTime.IsZero() {
		te.logger.Printf("Last Signal: %v ago", time.Since(te.stats.LastSignalTime).Round(time.Second))
	}

	if !te.stats.LastErrorTime.IsZero() {
		te.logger.Printf("Last Error: %v ago - %s",
			time.Since(te.stats.LastErrorTime).Round(time.Second),
			te.stats.LastError)
	}
}

// Public getter methods

func (te *TradingEngine) GetState() EngineState {
	te.stateMutex.RLock()
	defer te.stateMutex.RUnlock()
	return te.state
}

func (te *TradingEngine) GetStatistics() EngineStatistics {
	te.statsMutex.RLock()
	defer te.statsMutex.RUnlock()
	return *te.stats
}

func (te *TradingEngine) IsRunning() bool {
	return te.GetState() == StateRunning
}
