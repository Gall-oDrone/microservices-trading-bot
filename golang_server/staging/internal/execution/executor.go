package execution

import (
	"bitso_trading_bot/internal/behaviors"
	"bitso_trading_bot/pkg/bitso"
	"bitso_trading_bot/table"
	"fmt"
)

// Executor is the interface that wraps the Execute method.
type Executor interface {
	Execute(book *bitso.Book, ticker *bitso.Ticker, fee bitso.Fee, minMinorAmount, minMajorAmount float64, buyBehavior *behaviors.BuyBehavior, sellBehavior *behaviors.SellBehavior) error
}

// BasicExecutor implements a basic trading strategy
type BasicExecutor struct {
	tableData *table.TableData
}

// NewBasicExecutor creates a new basic executor
func NewBasicExecutor() *BasicExecutor {
	return &BasicExecutor{
		tableData: table.NewTableData(),
	}
}

// Execute implements the basic trading strategy
func (e *BasicExecutor) Execute(book *bitso.Book, ticker *bitso.Ticker, fee bitso.Fee, minMinorAmount, minMajorAmount float64, buyBehavior *behaviors.BuyBehavior, sellBehavior *behaviors.SellBehavior) error {
	if ticker == nil {
		return fmt.Errorf("ticker is nil")
	}

	// Execute buy strategy if conditions are met
	if err := buyBehavior.ExecuteMakerStrategy(nil, ticker, minMinorAmount, fee, e.tableData); err != nil {
		return fmt.Errorf("buy strategy failed: %w", err)
	}

	// Execute sell strategy if conditions are met
	if err := sellBehavior.ExecuteMakerStrategy(nil, ticker, minMajorAmount, fee, e.tableData); err != nil {
		return fmt.Errorf("sell strategy failed: %w", err)
	}

	return nil
}

// TrendFollowingExecutor implements a trend following trading strategy
type TrendFollowingExecutor struct {
	tableData *table.TableData
}

// NewTrendFollowingExecutor creates a new trend following executor
func NewTrendFollowingExecutor() *TrendFollowingExecutor {
	return &TrendFollowingExecutor{
		tableData: table.NewTableData(),
	}
}

// Execute implements the trend following strategy
func (e *TrendFollowingExecutor) Execute(book *bitso.Book, ticker *bitso.Ticker, fee bitso.Fee, minMinorAmount, minMajorAmount float64, buyBehavior *behaviors.BuyBehavior, sellBehavior *behaviors.SellBehavior) error {
	// TODO: Implement trend following logic
	return nil
}

// ArbitrageExecutor implements an arbitrage trading strategy
type ArbitrageExecutor struct {
	tableData *table.TableData
}

// NewArbitrageExecutor creates a new arbitrage executor
func NewArbitrageExecutor() *ArbitrageExecutor {
	return &ArbitrageExecutor{
		tableData: table.NewTableData(),
	}
}

// Execute implements the arbitrage strategy
func (e *ArbitrageExecutor) Execute(book *bitso.Book, ticker *bitso.Ticker, fee bitso.Fee, minMinorAmount, minMajorAmount float64, buyBehavior *behaviors.BuyBehavior, sellBehavior *behaviors.SellBehavior) error {
	// TODO: Implement arbitrage logic
	return nil
}
