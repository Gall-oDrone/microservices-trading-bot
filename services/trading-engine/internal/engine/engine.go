package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/config"
	"bitso-trading-platform/shared/pkg/database"
	"bitso-trading-platform/shared/pkg/kafka"
	"bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/trading-engine/internal/execution"
)

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
	bitsoClient   *bitso.Client
	dbClient      *database.RedisClient
	kafkaConsumer *kafka.Consumer

	// Internal components
	executor execution.Executor
	book     *bitso.Book

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

// NewTradingEngine creates a new trading engine instance
func NewTradingEngine(
	tradingConfig *models.TradingConfig,
	appConfig *config.Config,
	bitsoClient *bitso.Client,
	dbClient *database.RedisClient,
	kafkaConsumer *kafka.Consumer,
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

	// Create executor
	executor := execution.NewBasicExecutor(bitsoClient)

	engine := &TradingEngine{
		config:        tradingConfig,
		appConfig:     appConfig,
		bitsoClient:   bitsoClient,
		dbClient:      dbClient,
		kafkaConsumer: kafkaConsumer,
		executor:      executor,
		book:          tradingConfig.Book,
		state:         StateInitializing,
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

	return engine, nil
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
			// Consume message with timeout
			ctx, cancel := context.WithTimeout(te.ctx, 5*time.Second)
			msg, err := te.kafkaConsumer.Consume(ctx)
			cancel()

			if err != nil {
				// Check if it's a timeout or context cancellation
				if err == context.DeadlineExceeded || err == context.Canceled {
					continue
				}
				te.logger.Printf("Error consuming message: %v", err)
				te.recordError(err)
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

			// Process the signal
			if err := te.processTradeSignal(signal); err != nil {
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
		return fmt.Errorf("signal received outside trading hours")
	}

	// Check current state
	if te.GetState() != StateRunning {
		return fmt.Errorf("engine not in running state")
	}

	// Parse book from signal
	book, err := te.parseBook(signal.Book)
	if err != nil {
		return fmt.Errorf("invalid book: %w", err)
	}

	// Get current ticker for validation
	ticker, err := te.bitsoClient.Ticker(book)
	if err != nil {
		return fmt.Errorf("failed to get ticker: %w", err)
	}

	// Validate signal price is reasonable
	if err := te.validateSignalPrice(signal, ticker); err != nil {
		return fmt.Errorf("signal validation failed: %w", err)
	}

	// Create trading signal for executor
	tradeSignal := execution.TradingSignal{
		Book:      book,
		Amount:    signal.Amount,
		Price:     signal.Price,
		Reason:    fmt.Sprintf("%v", signal.Metadata["reason"]),
		Timestamp: signal.Timestamp,
	}

	// Execute based on signal type
	var execErr error
	switch signal.Signal {
	case "BUY":
		tradeSignal.Type = execution.SignalBuy
		execErr = te.executor.ExecuteBuySignal(tradeSignal)
		if execErr == nil {
			te.incrementOrdersPlaced()
		} else {
			te.incrementOrdersFailed()
		}

	case "SELL":
		tradeSignal.Type = execution.SignalSell
		execErr = te.executor.ExecuteSellSignal(tradeSignal)
		if execErr == nil {
			te.incrementOrdersPlaced()
		} else {
			te.incrementOrdersFailed()
		}

	default:
		return fmt.Errorf("unknown signal type: %s", signal.Signal)
	}

	return execErr
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
		return fmt.Errorf("failed to fetch balances: %w", err)
	}

	te.logger.Printf("Retrieved %d currency balances", len(balances))

	// Cache balances in Redis
	for _, balance := range balances {
		if err := te.dbClient.SaveUserBalance(&balance); err != nil {
			te.logger.Printf("Warning: Failed to cache balance for %s: %v",
				balance.Currency.String(), err)
		}
	}

	return nil
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
		return fmt.Errorf("redis health check failed: %w", err)
	}

	// Check Bitso API
	if _, err := te.bitsoClient.Ticker(te.book); err != nil {
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
