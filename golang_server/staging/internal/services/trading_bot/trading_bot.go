package trading_bot

import (
	"fmt"
	"log"
	"time"

	"bitso_trading_bot/internal/services/trading_bot/models"
)

// TradingBot represents the main trading bot structure
type TradingBot struct {
	logger *log.Logger
	config *models.TradingConfig
}

// NewTradingBot creates a new instance of the trading bot
func NewTradingBot(config *models.TradingConfig) *TradingBot {
	logger := log.New(log.Writer(), "[TRADING-BOT] ", log.LstdFlags|log.Lshortfile)
	return &TradingBot{
		logger: logger,
		config: config,
	}
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

	tb.logger.Printf("Trading pair: %s", tb.config.Book.String())
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

	for {
		select {
		case <-ticker.C:
			if !tb.config.IsWithinTradingHours() {
				tb.logger.Println("Outside trading hours, waiting...")
				continue
			}

			// TODO: Implement trading logic here
			tb.logger.Println("Checking for trading opportunities...")
		}
	}
}

// Stop gracefully stops the trading bot
func (tb *TradingBot) Stop() error {
	tb.logger.Println("Stopping trading bot...")
	// TODO: Implement cleanup and position closing logic
	return nil
}
