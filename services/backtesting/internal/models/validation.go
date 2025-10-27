package models

import (
	"fmt"
	"strings"
	"time"
)

// ValidateBacktestConfig validates a backtest configuration
func ValidateBacktestConfig(config *BacktestConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}
	
	return config.Validate()
}

// ValidateTimeRange validates a time range
func ValidateTimeRange(start, end time.Time) error {
	if start.IsZero() {
		return fmt.Errorf("start date cannot be zero")
	}
	
	if end.IsZero() {
		return fmt.Errorf("end date cannot be zero")
	}
	
	if end.Before(start) {
		return fmt.Errorf("end date (%s) must be after start date (%s)",
			end.Format("2006-01-02"), start.Format("2006-01-02"))
	}
	
	// Check if range is reasonable (not too long)
	duration := end.Sub(start)
	maxDuration := 5 * 365 * 24 * time.Hour // 5 years
	if duration > maxDuration {
		return fmt.Errorf("time range too long: %v (maximum: 5 years)", duration)
	}
	
	// Check if date is not in the future
	if start.After(time.Now()) {
		return fmt.Errorf("start date cannot be in the future")
	}
	
	return nil
}

// ValidateStrategy validates a strategy name and parameters
func ValidateStrategy(strategy string, params map[string]interface{}) error {
	if strategy == "" {
		return fmt.Errorf("strategy name is required")
	}
	
	// List of valid strategies
	validStrategies := map[string]bool{
		"basic":      true,
		"trend":      true,
		"arbitrage":  true,
		"mean_reversion": true,
	}
	
	if !validStrategies[strategy] {
		return fmt.Errorf("unknown strategy: %s (valid: basic, trend, arbitrage, mean_reversion)", strategy)
	}
	
	// Validate params is not nil
	if params == nil {
		return fmt.Errorf("strategy parameters cannot be nil")
	}
	
	// Strategy-specific validation
	switch strategy {
	case "basic":
		return validateBasicStrategyParams(params)
	case "trend":
		return validateTrendStrategyParams(params)
	case "arbitrage":
		return validateArbitrageStrategyParams(params)
	case "mean_reversion":
		return validateMeanReversionStrategyParams(params)
	}
	
	return nil
}

// ValidateBook validates a trading book/pair
func ValidateBook(book string) error {
	if book == "" {
		return fmt.Errorf("book is required")
	}
	
	// Normalize to lowercase
	book = strings.ToLower(book)
	
	// Check format: should be like "btc_mxn", "eth_mxn"
	parts := strings.Split(book, "_")
	if len(parts) != 2 {
		return fmt.Errorf("invalid book format: %s (expected format: base_quote, e.g., btc_mxn)", book)
	}
	
	base, quote := parts[0], parts[1]
	
	if base == "" || quote == "" {
		return fmt.Errorf("invalid book format: both base and quote currencies are required")
	}
	
	// Common quote currencies
	validQuotes := map[string]bool{
		"mxn": true,
		"usd": true,
		"btc": true,
		"eth": true,
	}
	
	if !validQuotes[quote] {
		return fmt.Errorf("unsupported quote currency: %s", quote)
	}
	
	return nil
}

// ValidateBalance validates an initial balance
func ValidateBalance(balance float64) error {
	if balance <= 0 {
		return fmt.Errorf("balance must be positive, got: %f", balance)
	}
	
	// Check if balance is reasonable (not too small or too large)
	minBalance := 100.0   // Minimum 100 units
	maxBalance := 1e10    // Maximum 10 billion units
	
	if balance < minBalance {
		return fmt.Errorf("balance too small: %f (minimum: %f)", balance, minBalance)
	}
	
	if balance > maxBalance {
		return fmt.Errorf("balance too large: %f (maximum: %f)", balance, maxBalance)
	}
	
	return nil
}

// ValidateSlippageModel validates a slippage model
func ValidateSlippageModel(model string) error {
	validModels := map[string]bool{
		"none":       true,
		"fixed":      true,
		"percentage": true,
		"volume":     true,
	}
	
	if !validModels[model] {
		return fmt.Errorf("invalid slippage model: %s (valid: none, fixed, percentage, volume)", model)
	}
	
	return nil
}

// ValidateCommissionRate validates a commission rate
func ValidateCommissionRate(rate float64) error {
	if rate < 0 {
		return fmt.Errorf("commission rate cannot be negative, got: %f", rate)
	}
	
	// Check if rate is reasonable (not more than 10%)
	maxRate := 0.1
	if rate > maxRate {
		return fmt.Errorf("commission rate too high: %f (maximum: %f)", rate, maxRate)
	}
	
	return nil
}

// Strategy-specific parameter validation

func validateBasicStrategyParams(params map[string]interface{}) error {
	// Example: RSI-based strategy
	// Required: rsi_period, rsi_oversold, rsi_overbought
	
	rsiPeriod, ok := params["rsi_period"]
	if !ok {
		return fmt.Errorf("basic strategy requires 'rsi_period' parameter")
	}
	
	// Validate RSI period
	period, ok := rsiPeriod.(float64)
	if !ok {
		period = float64(rsiPeriod.(int))
	}
	
	if period < 2 || period > 100 {
		return fmt.Errorf("rsi_period must be between 2 and 100, got: %v", rsiPeriod)
	}
	
	return nil
}

func validateTrendStrategyParams(params map[string]interface{}) error {
	// Example: Moving average-based
	// Required: short_ma, long_ma
	
	_, hasShort := params["short_ma"]
	_, hasLong := params["long_ma"]
	
	if !hasShort || !hasLong {
		return fmt.Errorf("trend strategy requires 'short_ma' and 'long_ma' parameters")
	}
	
	return nil
}

func validateArbitrageStrategyParams(params map[string]interface{}) error {
	// Example: Cross-exchange arbitrage
	// Required: min_spread
	
	_, hasSpread := params["min_spread"]
	if !hasSpread {
		return fmt.Errorf("arbitrage strategy requires 'min_spread' parameter")
	}
	
	return nil
}

func validateMeanReversionStrategyParams(params map[string]interface{}) error {
	// Example: Bollinger Bands-based
	// Required: period, std_dev
	
	_, hasPeriod := params["period"]
	_, hasStdDev := params["std_dev"]
	
	if !hasPeriod || !hasStdDev {
		return fmt.Errorf("mean_reversion strategy requires 'period' and 'std_dev' parameters")
	}
	
	return nil
}

// ValidateDataSource validates a data source
func ValidateDataSource(source string) error {
	validSources := map[string]bool{
		"market-data": true,
		"file":        true,
		"csv":         true,
	}
	
	if !validSources[source] {
		return fmt.Errorf("invalid data source: %s (valid: market-data, file, csv)", source)
	}
	
	return nil
}

// ValidateGranularity validates a data granularity
func ValidateGranularity(granularity string) error {
	validGranularities := map[string]bool{
		"tick": true,
		"1m":   true,
		"5m":   true,
		"15m":  true,
		"1h":   true,
		"1d":   true,
	}
	
	if !validGranularities[granularity] {
		return fmt.Errorf("invalid granularity: %s (valid: tick, 1m, 5m, 15m, 1h, 1d)", granularity)
	}
	
	return nil
}

