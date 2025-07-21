package execution

import (
	"bitso_trading_bot/internal/behaviors"
	"bitso_trading_bot/internal/strategies"
	"bitso_trading_bot/pkg/bitso"
	"bitso_trading_bot/table"
	"fmt"
	"log"
)

// Executor is the interface that wraps the Execute method.
type Executor interface {
	Execute(book *bitso.Book, ticker *bitso.Ticker, fee bitso.Fee, minMinorAmount, minMajorAmount float64, buyBehavior *behaviors.BuyBehavior, sellBehavior *behaviors.SellBehavior) error
	ExecuteBuySignal(signal strategies.TradingSignal, buyBehavior *behaviors.BuyBehavior, fee bitso.Fee) error
	ExecuteSellSignal(signal strategies.TradingSignal, sellBehavior *behaviors.SellBehavior, fee bitso.Fee) error
}

// BasicExecutor implements a basic trading strategy
type BasicExecutor struct {
	tableData *table.TableData
	logger    *log.Logger
}

// NewBasicExecutor creates a new basic executor
func NewBasicExecutor() *BasicExecutor {
	logger := log.New(log.Writer(), "[EXECUTOR] ", log.LstdFlags|log.Lshortfile)
	return &BasicExecutor{
		tableData: table.NewTableData(),
		logger:    logger,
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

// ExecuteBuySignal handles buy signals from the strategy
func (e *BasicExecutor) ExecuteBuySignal(signal strategies.TradingSignal, buyBehavior *behaviors.BuyBehavior, fee bitso.Fee) error {
	e.logger.Printf("Executing BUY signal for %s at price %.8f, amount: %.8f, reason: %s",
		signal.Book.String(), signal.Price, signal.Amount, signal.Reason)

	// Configure the buy behavior with the signal data
	buyBehavior.ConfigureOrder(*signal.Book, bitso.OrderTypeLimit, signal.Amount, signal.Price)

	// Execute the buy order using the behavior's ExecuteMakerStrategy
	// We pass nil for balance as it will be fetched within the behavior
	if err := buyBehavior.ExecuteMakerStrategy(nil, signal.Ticker, signal.Amount, fee, e.tableData); err != nil {
		return fmt.Errorf("failed to execute buy signal: %w", err)
	}

	e.logger.Printf("Successfully executed BUY signal for %s", signal.Book.String())
	return nil
}

// ExecuteSellSignal handles sell signals from the strategy
func (e *BasicExecutor) ExecuteSellSignal(signal strategies.TradingSignal, sellBehavior *behaviors.SellBehavior, fee bitso.Fee) error {
	e.logger.Printf("Executing SELL signal for %s at price %.8f, amount: %.8f, reason: %s",
		signal.Book.String(), signal.Price, signal.Amount, signal.Reason)

	// Configure the sell behavior with the signal data
	sellBehavior.ConfigureOrder(*signal.Book, bitso.OrderTypeLimit, signal.Amount, signal.Price)

	// Execute the sell order using the behavior's ExecuteMakerStrategy
	// We pass nil for balance as it will be fetched within the behavior
	if err := sellBehavior.ExecuteMakerStrategy(nil, signal.Ticker, signal.Amount, fee, e.tableData); err != nil {
		return fmt.Errorf("failed to execute sell signal: %w", err)
	}

	e.logger.Printf("Successfully executed SELL signal for %s", signal.Book.String())
	return nil
}

// TrendFollowingExecutor implements a trend following trading strategy
type TrendFollowingExecutor struct {
	tableData *table.TableData
	logger    *log.Logger
}

// NewTrendFollowingExecutor creates a new trend following executor
func NewTrendFollowingExecutor() *TrendFollowingExecutor {
	logger := log.New(log.Writer(), "[TREND-EXECUTOR] ", log.LstdFlags|log.Lshortfile)
	return &TrendFollowingExecutor{
		tableData: table.NewTableData(),
		logger:    logger,
	}
}

// Execute implements the trend following strategy
func (e *TrendFollowingExecutor) Execute(book *bitso.Book, ticker *bitso.Ticker, fee bitso.Fee, minMinorAmount, minMajorAmount float64, buyBehavior *behaviors.BuyBehavior, sellBehavior *behaviors.SellBehavior) error {
	// TODO: Implement trend following logic
	return nil
}

// ExecuteBuySignal handles buy signals from the strategy
func (e *TrendFollowingExecutor) ExecuteBuySignal(signal strategies.TradingSignal, buyBehavior *behaviors.BuyBehavior, fee bitso.Fee) error {
	e.logger.Printf("Executing TREND BUY signal for %s at price %.8f, amount: %.8f, reason: %s",
		signal.Book.String(), signal.Price, signal.Amount, signal.Reason)

	// Configure the buy behavior with the signal data
	buyBehavior.ConfigureOrder(*signal.Book, bitso.OrderTypeLimit, signal.Amount, signal.Price)

	// Execute the buy order using the behavior's ExecuteMakerStrategy
	if err := buyBehavior.ExecuteMakerStrategy(nil, signal.Ticker, signal.Amount, fee, e.tableData); err != nil {
		return fmt.Errorf("failed to execute trend buy signal: %w", err)
	}

	e.logger.Printf("Successfully executed TREND BUY signal for %s", signal.Book.String())
	return nil
}

// ExecuteSellSignal handles sell signals from the strategy
func (e *TrendFollowingExecutor) ExecuteSellSignal(signal strategies.TradingSignal, sellBehavior *behaviors.SellBehavior, fee bitso.Fee) error {
	e.logger.Printf("Executing TREND SELL signal for %s at price %.8f, amount: %.8f, reason: %s",
		signal.Book.String(), signal.Price, signal.Amount, signal.Reason)

	// Configure the sell behavior with the signal data
	sellBehavior.ConfigureOrder(*signal.Book, bitso.OrderTypeLimit, signal.Amount, signal.Price)

	// Execute the sell order using the behavior's ExecuteMakerStrategy
	if err := sellBehavior.ExecuteMakerStrategy(nil, signal.Ticker, signal.Amount, fee, e.tableData); err != nil {
		return fmt.Errorf("failed to execute trend sell signal: %w", err)
	}

	e.logger.Printf("Successfully executed TREND SELL signal for %s", signal.Book.String())
	return nil
}

// ArbitrageExecutor implements an arbitrage trading strategy
type ArbitrageExecutor struct {
	tableData *table.TableData
	logger    *log.Logger
}

// NewArbitrageExecutor creates a new arbitrage executor
func NewArbitrageExecutor() *ArbitrageExecutor {
	logger := log.New(log.Writer(), "[ARBITRAGE-EXECUTOR] ", log.LstdFlags|log.Lshortfile)
	return &ArbitrageExecutor{
		tableData: table.NewTableData(),
		logger:    logger,
	}
}

// Execute implements the arbitrage strategy
func (e *ArbitrageExecutor) Execute(book *bitso.Book, ticker *bitso.Ticker, fee bitso.Fee, minMinorAmount, minMajorAmount float64, buyBehavior *behaviors.BuyBehavior, sellBehavior *behaviors.SellBehavior) error {
	// TODO: Implement arbitrage logic
	return nil
}

// ExecuteBuySignal handles buy signals from the strategy
func (e *ArbitrageExecutor) ExecuteBuySignal(signal strategies.TradingSignal, buyBehavior *behaviors.BuyBehavior, fee bitso.Fee) error {
	e.logger.Printf("Executing ARBITRAGE BUY signal for %s at price %.8f, amount: %.8f, reason: %s",
		signal.Book.String(), signal.Price, signal.Amount, signal.Reason)

	// Configure the buy behavior with the signal data
	buyBehavior.ConfigureOrder(*signal.Book, bitso.OrderTypeLimit, signal.Amount, signal.Price)

	// Execute the buy order using the behavior's ExecuteMakerStrategy
	if err := buyBehavior.ExecuteMakerStrategy(nil, signal.Ticker, signal.Amount, fee, e.tableData); err != nil {
		return fmt.Errorf("failed to execute arbitrage buy signal: %w", err)
	}

	e.logger.Printf("Successfully executed ARBITRAGE BUY signal for %s", signal.Book.String())
	return nil
}

// ExecuteSellSignal handles sell signals from the strategy
func (e *ArbitrageExecutor) ExecuteSellSignal(signal strategies.TradingSignal, sellBehavior *behaviors.SellBehavior, fee bitso.Fee) error {
	e.logger.Printf("Executing ARBITRAGE SELL signal for %s at price %.8f, amount: %.8f, reason: %s",
		signal.Book.String(), signal.Price, signal.Amount, signal.Reason)

	// Configure the sell behavior with the signal data
	sellBehavior.ConfigureOrder(*signal.Book, bitso.OrderTypeLimit, signal.Amount, signal.Price)

	// Execute the sell order using the behavior's ExecuteMakerStrategy
	if err := sellBehavior.ExecuteMakerStrategy(nil, signal.Ticker, signal.Amount, fee, e.tableData); err != nil {
		return fmt.Errorf("failed to execute arbitrage sell signal: %w", err)
	}

	e.logger.Printf("Successfully executed ARBITRAGE SELL signal for %s", signal.Book.String())
	return nil
}
