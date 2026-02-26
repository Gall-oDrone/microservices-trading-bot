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
	SlippageModel   string  `json:"slippage_model"`   // "none", "fixed", "percentage"
	SlippageValue   float64 `json:"slippage_value"`   // Value depends on model
	CommissionRate  float64 `json:"commission_rate"`  // Legacy: single rate (used for both when MakerFee/TakerFee not set)
	MakerFee        float64 `json:"maker_fee"`        // Maker fee as decimal (Bitso btc_mxn: 0.005). See https://docs.bitso.com/bitso-api/docs/list-fees
	TakerFee        float64 `json:"taker_fee"`       // Taker fee as decimal (Bitso btc_mxn: 0.0065)

	// Data settings
	DataSource      string `json:"data_source"`      // "market-data", "file"
	DataGranularity string `json:"data_granularity"` // "tick", "1m", "5m", etc.

	// Metadata
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by,omitempty"`

	// Optional success criteria for "strategy worked" evaluation (all zero = not set)
	SuccessCriteria *SuccessCriteria `json:"success_criteria,omitempty"`
}

// SuccessCriteria defines optional thresholds to evaluate if a backtest "succeeded"
// All fields are optional; zero value means "not set" and the check is skipped
type SuccessCriteria struct {
	MinSharpeRatio      float64 `json:"min_sharpe_ratio"`      // e.g. 1.0 = require Sharpe >= 1.0
	MaxDrawdownPercent  float64 `json:"max_drawdown_percent"`  // e.g. 10 = require drawdown >= -10% (stored as positive)
	MinTotalTrades      int     `json:"min_total_trades"`     // e.g. 10 = require at least 10 trades
	MinWinRate          float64 `json:"min_win_rate"`          // e.g. 0.5 = require win rate >= 50% (0-1)
	MinTotalReturnPct   float64 `json:"min_total_return_percent"` // e.g. 5 = require total return >= 5%
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
		SlippageValue:   0.001,  // 0.1%
		CommissionRate:  0,      // When 0, MakerFee/TakerFee are used
		MakerFee:        0.005, // Bitso btc_mxn maker (https://docs.bitso.com/bitso-api/docs/list-fees)
		TakerFee:        0.0065,
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
	if c.MakerFee < 0 || c.MakerFee > 0.1 {
		return fmt.Errorf("maker_fee must be in [0, 0.1], got %f", c.MakerFee)
	}
	if c.TakerFee < 0 || c.TakerFee > 0.1 {
		return fmt.Errorf("taker_fee must be in [0, 0.1], got %f", c.TakerFee)
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

// WithCommission sets the commission rate (legacy: used for both maker and taker when set)
func (c *BacktestConfig) WithCommission(rate float64) *BacktestConfig {
	c.CommissionRate = rate
	c.MakerFee = 0
	c.TakerFee = 0
	return c
}

// WithMakerTakerFees sets maker and taker fees (Bitso-style). See https://docs.bitso.com/bitso-api/docs/list-fees
func (c *BacktestConfig) WithMakerTakerFees(maker, taker float64) *BacktestConfig {
	c.MakerFee = maker
	c.TakerFee = taker
	c.CommissionRate = 0
	return c
}

// generateConfigID generates a unique ID for a configuration
func generateConfigID() string {
	return fmt.Sprintf("cfg-%d", time.Now().UnixNano())
}
