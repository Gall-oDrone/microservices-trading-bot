package engine

import (
	"context"
	"fmt"
	"log"
	"time"

	"bitso-trading-platform/services/trading-engine/internal/execution"
	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/config"
	"bitso-trading-platform/shared/pkg/database"
	"bitso-trading-platform/shared/pkg/models"
)

// TradingEngine represents the main trading engine
type TradingEngine struct {
	logger      *log.Logger
	config      *models.TradingConfig
	appConfig   *config.Config
	bitsoClient *bitso.Client
	dbClient    *database.RedisClient
	book        *bitso.Book
	executor    execution.Executor
	stopChan    chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewTradingEngine creates a new trading engine instance
func NewTradingEngine(tradingConfig *models.TradingConfig, appConfig *config.Config, bitsoClient *bitso.Client, dbClient *database.RedisClient) *TradingEngine {
	logger := log.New(log.Writer(), "[TRADING-ENGINE] ", log.LstdFlags|log.Lshortfile)
	ctx, cancel := context.WithCancel(context.Background())

	return &TradingEngine{
		logger:      logger,
		config:      tradingConfig,
		appConfig:   appConfig,
		bitsoClient: bitsoClient,
		dbClient:    dbClient,
		book:        tradingConfig.Book,
		executor:    execution.NewBasicExecutor(bitsoClient),
		stopChan:    make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Initialize sets up the trading engine
func (te *TradingEngine) Initialize() error {
	te.logger.Println("Initializing trading engine...")

	// Validate the trading configuration
	if err := te.config.Validate(); err != nil {
		return fmt.Errorf("invalid trading configuration: %w", err)
	}

	// Check if we're within trading hours
	if !te.config.IsWithinTradingHours() {
		return fmt.Errorf("current time is outside trading hours")
	}

	// Fetch and store balances from Bitso API
	if err := te.fetchAndStoreBalances(); err != nil {
		return fmt.Errorf("failed to fetch and store balances: %w", err)
	}

	te.logger.Printf("Trading pair: %s", te.book.String())
	te.logger.Printf("Trading limits: Min=%.8f, Max=%.8f, MaxValue=%.2f",
		te.config.MinTradeAmount,
		te.config.MaxTradeAmount,
		te.config.MaxTradeValue)
	te.logger.Printf("Risk management: StopLoss=%.1f%%, TakeProfit=%.1f%%",
		te.config.StopLossPercent,
		te.config.TakeProfitPercent)

	return nil
}

// fetchAndStoreBalances fetches balances from Bitso API and stores them in Redis
func (te *TradingEngine) fetchAndStoreBalances() error {
	te.logger.Println("Fetching balances from Bitso API...")

	// Fetch balances from Bitso API
	balances, err := te.bitsoClient.Balances(nil)
	if err != nil {
		return fmt.Errorf("failed to fetch balances from Bitso: %w", err)
	}

	te.logger.Printf("Retrieved %d balances from Bitso API", len(balances))

	// Store each balance in Redis
	for _, balance := range balances {
		if err := te.dbClient.SaveUserBalance(&balance); err != nil {
			te.logger.Printf("Warning: failed to save balance for %s: %v", balance.Currency.String(), err)
			continue
		}
		te.logger.Printf("Saved balance for %s: Available=%.8f, Locked=%.8f",
			balance.Currency.String(), balance.Available.Float64(), balance.Locked.Float64())
	}

	return nil
}

// Start begins the trading engine's operations
func (te *TradingEngine) Start() error {
	te.logger.Println("Starting trading engine...")

	// Validate configuration before starting
	if err := te.config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Start the main trading loop
	go te.tradingLoop()

	return nil
}

// tradingLoop is the main trading loop that monitors for signals
func (te *TradingEngine) tradingLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !te.config.IsWithinTradingHours() {
				te.logger.Println("Outside trading hours, waiting...")
				continue
			}

			// Check system health
			if err := te.healthCheck(); err != nil {
				te.logger.Printf("Health check failed: %v", err)
				continue
			}

		case <-te.stopChan:
			te.logger.Println("Trading loop stopped")
			return
		}
	}
}

// healthCheck performs a health check on the trading engine
func (te *TradingEngine) healthCheck() error {
	// Check Redis connection
	if err := te.dbClient.Ping(te.ctx); err != nil {
		return fmt.Errorf("Redis connection failed: %w", err)
	}

	// Check Bitso API connection by fetching ticker
	_, err := te.bitsoClient.Ticker(te.book)
	if err != nil {
		return fmt.Errorf("Bitso API connection failed: %w", err)
	}

	return nil
}

// Stop gracefully stops the trading engine
func (te *TradingEngine) Stop() error {
	te.logger.Println("Stopping trading engine...")

	// Cancel context
	te.cancel()

	// Close stop channel
	close(te.stopChan)

	// Close database connection
	if err := te.dbClient.Close(); err != nil {
		return fmt.Errorf("error closing database connection: %w", err)
	}

	te.logger.Println("Trading engine stopped successfully")
	return nil
}

// ProcessTradeSignal processes incoming trade signals from the strategy service
func (te *TradingEngine) ProcessTradeSignal(signal *models.TradeSignalEvent) error {
	te.logger.Printf("Processing trade signal: %s %s at %.2f", signal.Signal, signal.Book, signal.Price)

	// Convert to internal signal format
	book := bitso.NewBook(bitso.BTC, bitso.MXN) // TODO: Parse from signal.Book
	tradeSignal := execution.TradingSignal{
		Book:      &book,
		Amount:    signal.Amount,
		Price:     signal.Price,
		Reason:    fmt.Sprintf("%v", signal.Metadata["reason"]),
		Timestamp: signal.Timestamp,
	}

	// Execute based on signal type
	switch signal.Signal {
	case "BUY":
		tradeSignal.Type = execution.SignalBuy
		return te.executor.ExecuteBuySignal(tradeSignal)
	case "SELL":
		tradeSignal.Type = execution.SignalSell
		return te.executor.ExecuteSellSignal(tradeSignal)
	default:
		return fmt.Errorf("unknown signal type: %s", signal.Signal)
	}
}
