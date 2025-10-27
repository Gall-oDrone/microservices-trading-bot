package models

import (
	"fmt"
	"time"
)

// BacktestConfig represents the configuration for a backtest
type BacktestConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Time range
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`

	// Trading parameters
	Book           string  `json:"book"`            // Trading pair (e.g., "btc_mxn")
	InitialBalance float64 `json:"initial_balance"` // Initial balance in quote currency

	// Strategy configuration
	Strategy       string                 `json:"strategy"`        // Strategy name
	StrategyParams map[string]interface{} `json:"strategy_params"` // Strategy-specific parameters

	// Execution settings
	SlippageModel  string  `json:"slippage_model"`  // "none", "fixed", "percentage"
	SlippageValue  float64 `json:"slippage_value"`  // Value depends on model
	CommissionRate float64 `json:"commission_rate"` // Commission as decimal (0.001 = 0.1%)

	// Data settings
	DataSource      string `json:"data_source"`      // "market-data", "file"
	DataGranularity string `json:"data_granularity"` // "tick", "1m", "5m", etc.

	// Metadata
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by,omitempty"`
}

// Range represents a parameter range for optimization
type Range struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

// NewBacktestConfig creates a new backtest configuration
func NewBacktestConfig(name, book string, startDate, endDate time.Time) *BacktestConfig {
	return &BacktestConfig{
		ID:              generateConfigID(),
		Name:            name,
		StartDate:       startDate,
		EndDate:         endDate,
		Book:            book,
		InitialBalance:  100000.0, // Default 100k
		Strategy:        "basic",
		StrategyParams:  make(map[string]interface{}),
		SlippageModel:   "percentage",
		SlippageValue:   0.001, // 0.1%
		CommissionRate:  0.001, // 0.1%
		DataSource:      "market-data",
		DataGranularity: "tick",
		CreatedAt:       time.Now(),
	}
}

// Validate validates the backtest configuration
func (c *BacktestConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}

	if c.Book == "" {
		return fmt.Errorf("book is required")
	}

	if c.StartDate.IsZero() {
		return fmt.Errorf("start_date is required")
	}

	if c.EndDate.IsZero() {
		return fmt.Errorf("end_date is required")
	}

	if c.EndDate.Before(c.StartDate) {
		return fmt.Errorf("end_date must be after start_date")
	}

	if c.InitialBalance <= 0 {
		return fmt.Errorf("initial_balance must be positive, got %f", c.InitialBalance)
	}

	if c.Strategy == "" {
		return fmt.Errorf("strategy is required")
	}

	// Validate slippage model
	validSlippageModels := map[string]bool{
		"none":       true,
		"fixed":      true,
		"percentage": true,
	}
	if !validSlippageModels[c.SlippageModel] {
		return fmt.Errorf("invalid slippage_model: %s (must be: none, fixed, percentage)", c.SlippageModel)
	}

	if c.SlippageModel != "none" && c.SlippageValue < 0 {
		return fmt.Errorf("slippage_value must be non-negative")
	}

	if c.CommissionRate < 0 {
		return fmt.Errorf("commission_rate must be non-negative")
	}

	// Validate data source
	validDataSources := map[string]bool{
		"market-data": true,
		"file":        true,
	}
	if !validDataSources[c.DataSource] {
		return fmt.Errorf("invalid data_source: %s (must be: market-data, file)", c.DataSource)
	}

	return nil
}

// GetDuration returns the duration of the backtest period
func (c *BacktestConfig) GetDuration() time.Duration {
	return c.EndDate.Sub(c.StartDate)
}

// GetDays returns the number of days in the backtest period
func (c *BacktestConfig) GetDays() int {
	return int(c.GetDuration().Hours() / 24)
}

// Clone creates a deep copy of the configuration
func (c *BacktestConfig) Clone() *BacktestConfig {
	clone := *c
	
	// Deep copy strategy params
	if c.StrategyParams != nil {
		clone.StrategyParams = make(map[string]interface{})
		for k, v := range c.StrategyParams {
			clone.StrategyParams[k] = v
		}
	}
	
	return &clone
}

// WithStrategy sets the strategy and parameters
func (c *BacktestConfig) WithStrategy(strategy string, params map[string]interface{}) *BacktestConfig {
	c.Strategy = strategy
	c.StrategyParams = params
	return c
}

// WithInitialBalance sets the initial balance
func (c *BacktestConfig) WithInitialBalance(balance float64) *BacktestConfig {
	c.InitialBalance = balance
	return c
}

// WithSlippage sets the slippage configuration
func (c *BacktestConfig) WithSlippage(model string, value float64) *BacktestConfig {
	c.SlippageModel = model
	c.SlippageValue = value
	return c
}

// WithCommission sets the commission rate
func (c *BacktestConfig) WithCommission(rate float64) *BacktestConfig {
	c.CommissionRate = rate
	return c
}

// generateConfigID generates a unique ID for a configuration
func generateConfigID() string {
	return fmt.Sprintf("cfg-%d", time.Now().UnixNano())
}

