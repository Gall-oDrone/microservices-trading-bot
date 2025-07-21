package trading_bot

import (
	"fmt"
	"log"
	"time"

	"bitso_trading_bot/internal/behaviors"
	"bitso_trading_bot/internal/database"
	"bitso_trading_bot/internal/execution"
	"bitso_trading_bot/internal/models"
	"bitso_trading_bot/internal/order"
	"bitso_trading_bot/internal/strategies"
	"bitso_trading_bot/pkg/bitso"
	"bitso_trading_bot/table"
)

// TradingBot represents the main trading bot structure
type TradingBot struct {
	logger       *log.Logger
	config       *models.TradingConfig
	bitsoClient  *bitso.Client
	dbClient     *database.RedisClient
	book         *bitso.Book
	sellBehavior *behaviors.SellBehavior
	buyBehavior  *behaviors.BuyBehavior
	strategy     strategies.Strategy
	orderManager *order.Manager
	executor     execution.Executor
	stopChan     chan struct{}
}

// NewTradingBot creates a new instance of the trading bot
func NewTradingBot(config *models.TradingConfig, bitsoClient *bitso.Client, dbClient *database.RedisClient) *TradingBot {
	logger := log.New(log.Writer(), "[TRADING-BOT] ", log.LstdFlags|log.Lshortfile)
	return &TradingBot{
		logger:      logger,
		config:      config,
		bitsoClient: bitsoClient,
		dbClient:    dbClient,
		book:        config.Book,
		strategy:    strategies.NewBasicStrategy(config.Book),
		executor:    execution.NewBasicExecutor(),
		stopChan:    make(chan struct{}),
	}
}

// fetchAndStoreBalances fetches balances from Bitso API and stores them in Redis
func (tb *TradingBot) fetchAndStoreBalances() error {
	tb.logger.Println("Fetching balances from Bitso API...")

	// Fetch balances from Bitso API
	balances, err := tb.bitsoClient.Balances(nil)
	if err != nil {
		return fmt.Errorf("failed to fetch balances from Bitso: %w", err)
	}

	tb.logger.Printf("Retrieved %d balances from Bitso API", len(balances))

	// Store each balance in Redis
	for _, balance := range balances {
		if err := tb.dbClient.SaveUserBalance(&balance); err != nil {
			tb.logger.Printf("Warning: failed to save balance for %s: %v", balance.Currency.String(), err)
			continue
		}
		tb.logger.Printf("Saved balance for %s: Available=%.8f, Locked=%.8f",
			balance.Currency.String(), balance.Available.Float64(), balance.Locked.Float64())
	}

	return nil
}

// Initialize sets up the trading bot
func (tb *TradingBot) Initialize() error {
	tb.logger.Println("Initializing trading bot...")

	// Validate the trading configuration
	if err := tb.config.Validate(); err != nil {
		return fmt.Errorf("invalid trading configuration: %w", err)
	}

	// Check if we're within trading hours
	if !tb.config.IsWithinTradingHours() {
		return fmt.Errorf("current time is outside trading hours")
	}

	// Fetch and store balances from Bitso API
	if err := tb.fetchAndStoreBalances(); err != nil {
		return fmt.Errorf("failed to fetch and store balances: %w", err)
	}

	// Initialize order manager
	tb.orderManager = order.NewManager(tb.bitsoClient, *tb.dbClient, tb.book)

	// Initialize behaviors
	majorBalance, err := tb.dbClient.GetUserBalance(tb.book.Major().String())
	if err != nil {
		return fmt.Errorf("failed to get major balance: %w", err)
	}

	minorBalance, err := tb.dbClient.GetUserBalance(tb.book.Minor().String())
	if err != nil {
		return fmt.Errorf("failed to get minor balance: %w", err)
	}

	tb.sellBehavior = behaviors.NewSellBehavior(majorBalance, tb.bitsoClient, tb.orderManager)
	tb.buyBehavior = behaviors.NewBuyBehavior(minorBalance, tb.bitsoClient, tb.orderManager)

	tb.logger.Printf("Trading pair: %s", tb.book.String())
	tb.logger.Printf("Trading limits: Min=%.8f, Max=%.8f, MaxValue=%.2f",
		tb.config.MinTradeAmount,
		tb.config.MaxTradeAmount,
		tb.config.MaxTradeValue)
	tb.logger.Printf("Risk management: StopLoss=%.1f%%, TakeProfit=%.1f%%",
		tb.config.StopLossPercent,
		tb.config.TakeProfitPercent)

	return nil
}

// Start begins the trading bot's operations
func (tb *TradingBot) Start() error {
	tb.logger.Println("Starting trading bot...")

	// Validate configuration before starting
	if err := tb.config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Start the main trading loop
	go tb.tradingLoop()

	return nil
}

// tradingLoop is the main trading loop that runs continuously
func (tb *TradingBot) tradingLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Start signal listening goroutine
	go tb.listenToSignals()

	for range ticker.C {
		if !tb.config.IsWithinTradingHours() {
			tb.logger.Println("Outside trading hours, waiting...")
			continue
		}

		tb.logger.Println("Checking for trading opportunities...")

		// Get current ticker
		ticker := tb.GetTicker(tb.book)
		if ticker == nil {
			tb.logger.Println("Failed to get ticker, skipping this iteration")
			continue
		}

		// Execute the trading strategy
		if err := tb.strategy.Execute(ticker); err != nil {
			tb.logger.Printf("Strategy execution error: %v", err)
			continue
		}
	}
}

// listenToSignals listens to the strategy's signal channels and executes orders
func (tb *TradingBot) listenToSignals() {
	tb.logger.Println("Starting signal listener...")

	// Get the signal channels from the strategy
	buySignalChan := tb.strategy.GetBuySignalChannel()
	sellSignalChan := tb.strategy.GetSellSignalChannel()

	// Get current fee structure
	fee := tb.GetFee()

	for {
		select {
		case buySignal := <-buySignalChan:
			tb.logger.Printf("Received BUY signal: %s at price %.8f, amount: %.8f",
				buySignal.Book.String(), buySignal.Price, buySignal.Amount)

			// Execute the buy signal
			if err := tb.executor.ExecuteBuySignal(buySignal, tb.buyBehavior, fee); err != nil {
				tb.logger.Printf("Failed to execute buy signal: %v", err)
			}

		case sellSignal := <-sellSignalChan:
			tb.logger.Printf("Received SELL signal: %s at price %.8f, amount: %.8f",
				sellSignal.Book.String(), sellSignal.Price, sellSignal.Amount)

			// Execute the sell signal
			if err := tb.executor.ExecuteSellSignal(sellSignal, tb.sellBehavior, fee); err != nil {
				tb.logger.Printf("Failed to execute sell signal: %v", err)
			}

		case <-time.After(5 * time.Second):
			// Refresh fee structure periodically
			fee = tb.GetFee()

		case <-tb.stopChan:
			tb.logger.Println("Signal listener stopped")
			return
		}
	}
}

// Stop gracefully stops the trading bot
func (tb *TradingBot) Stop() error {
	tb.logger.Println("Stopping trading bot...")

	// Stop the strategy
	if err := tb.strategy.Stop(); err != nil {
		tb.logger.Printf("Error stopping strategy: %v", err)
	}

	// Stop the signal listener
	close(tb.stopChan)

	// TODO: Implement cleanup and position closing logic
	return nil
}

// GetTicker returns the current market ticker
func (tb *TradingBot) GetTicker(book *bitso.Book) *bitso.Ticker {
	ticker, err := tb.bitsoClient.Ticker(book)
	if err != nil {
		tb.logger.Printf("Error getting ticker: %v", err)
		return nil
	}
	return ticker
}

// MinMinorAllowToTrade returns the minimum amount allowed to trade
func (tb *TradingBot) MinMinorAllowToTrade() float64 {
	return tb.config.MinTradeAmount
}

// MinMajorAllowToTrade returns the minimum amount allowed to trade
func (tb *TradingBot) MinMajorAllowToTrade() float64 {
	return tb.config.MinTradeAmount
}

// GetFee returns the current fee structure
func (tb *TradingBot) GetFee() bitso.Fee {
	fees, err := tb.bitsoClient.Fees(nil)
	if err != nil {
		tb.logger.Printf("Error getting fees: %v", err)
		return bitso.Fee{}
	}
	for _, fee := range fees.Fees {
		if fee.Book.String() == tb.book.String() {
			return fee
		}
	}
	return bitso.Fee{}
}

// GetBook returns the current trading book
func (tb *TradingBot) GetBook() *bitso.Book {
	return tb.book
}

// GetSellBehavior returns the sell behavior
func (tb *TradingBot) GetSellBehavior() *behaviors.SellBehavior {
	return tb.sellBehavior
}

// GetBuyBehavior returns the buy behavior
func (tb *TradingBot) GetBuyBehavior() *behaviors.BuyBehavior {
	return tb.buyBehavior
}

// GetConfig returns the trading configuration
func (tb *TradingBot) GetConfig() interface{} {
	return tb.config
}

// SetOrderWithTTL sets an order with a time-to-live in the database
func (tb *TradingBot) SetOrderWithTTL(oid string, timeout int) error {
	return tb.orderManager.SetOrderWithTTL(oid)
}

// GetDBClient returns the database client
func (tb *TradingBot) GetDBClient() *database.RedisClient {
	return tb.dbClient
}

// GetExecutor returns the executor
func (tb *TradingBot) GetExecutor() execution.Executor {
	return tb.executor
}

// GetTableData returns a new instance of TableData
func (tb *TradingBot) GetTableData() *table.TableData {
	return table.NewTableData()
}
