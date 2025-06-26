package models

import (
	"fmt"
	"time"

	"bitso_trading_bot/pkg/bitso"
)

// TradingConfig represents the configuration for the trading bot
type TradingConfig struct {
	// Book represents the trading pair (e.g., btc_mxn)
	Book *bitso.Book

	// Trading limits
	MaxTradeAmount float64 // Maximum amount to trade in major currency
	MinTradeAmount float64 // Minimum amount to trade in major currency
	MaxTradeValue  float64 // Maximum value in minor currency

	// Time parameters
	MaxTradingTime time.Duration // Maximum time to keep a position open
	StartTime      time.Time     // When to start trading
	EndTime        time.Time     // When to stop trading

	// Risk management
	MaxOpenPositions  int     // Maximum number of open positions
	StopLossPercent   float64 // Stop loss percentage
	TakeProfitPercent float64 // Take profit percentage

	// Trading strategy parameters
	StrategyType string                 // Type of strategy to use
	Parameters   map[string]interface{} // Strategy-specific parameters
}

// NewTradingConfig creates a new trading configuration with default values
func NewTradingConfig() *TradingConfig {
	return &TradingConfig{
		Book: bitso.NewBook(bitso.BTC, bitso.MXN), // Default to BTC/MXN

		// Default trading limits
		MaxTradeAmount: 0.1,   // 0.1 BTC
		MinTradeAmount: 0.001, // 0.001 BTC
		MaxTradeValue:  10000, // 10,000 MXN

		// Default time parameters
		MaxTradingTime: 24 * time.Hour, // 24 hours
		StartTime:      time.Now(),
		EndTime:        time.Now().Add(24 * time.Hour),

		// Default risk management
		MaxOpenPositions:  3,
		StopLossPercent:   2.0, // 2% stop loss
		TakeProfitPercent: 4.0, // 4% take profit

		// Default strategy
		StrategyType: "basic",
		Parameters:   make(map[string]interface{}),
	}
}

// Validate checks if the trading configuration is valid
func (tc *TradingConfig) Validate() error {
	if tc.Book == nil {
		return fmt.Errorf("trading book is required")
	}

	if tc.MaxTradeAmount <= 0 || tc.MinTradeAmount <= 0 {
		return fmt.Errorf("trade amounts must be positive")
	}

	if tc.MaxTradeAmount < tc.MinTradeAmount {
		return fmt.Errorf("max trade amount must be greater than min trade amount")
	}

	if tc.MaxTradingTime <= 0 {
		return fmt.Errorf("max trading time must be positive")
	}

	if tc.StartTime.After(tc.EndTime) {
		return fmt.Errorf("start time must be before end time")
	}

	if tc.MaxOpenPositions <= 0 {
		return fmt.Errorf("max open positions must be positive")
	}

	if tc.StopLossPercent <= 0 || tc.TakeProfitPercent <= 0 {
		return fmt.Errorf("stop loss and take profit percentages must be positive")
	}

	return nil
}

// IsWithinTradingHours checks if the current time is within trading hours
func (tc *TradingConfig) IsWithinTradingHours() bool {
	now := time.Now()
	return now.After(tc.StartTime) && now.Before(tc.EndTime)
}

// IsWithinTradeLimits checks if the trade amount is within configured limits
func (tc *TradingConfig) IsWithinTradeLimits(amount float64, price float64) bool {
	value := amount * price
	return amount >= tc.MinTradeAmount &&
		amount <= tc.MaxTradeAmount &&
		value <= tc.MaxTradeValue
}
