package behaviors

import (
	"bitso_trading_bot/internal/database"
	"bitso_trading_bot/internal/order"
	"bitso_trading_bot/pkg/bitso"
	"bitso_trading_bot/table"
)

// TradingBehavior defines the common interface for all trading behaviors
type TradingBehavior interface {
	CheckFunds(min float64) error
	CalculateOptimalRate(ticker *bitso.Ticker, limit, amount float64, fee bitso.Fee, marketTrade string) (float64, error)
	ConfigureOrder(book bitso.Book, orderType bitso.OrderType, amount, rate float64)
	ExecuteOrder(ticker *bitso.Ticker, amount, rate float64, oid string, orderStatus bitso.OrderStatus, orderType bitso.OrderType, dbClient database.DatabaseClient) (string, error)
	ExecuteMakerStrategy(book bitso.Book, balance *bitso.Balance, ticker *bitso.Ticker, minTradeAmount float64, fee bitso.Fee, tableData *table.TableData) error
}

// TradingState tracks the state of trading
type TradingState struct {
	IsFirstOrder bool
	IsFirstTrade bool
}

// BaseOrderBehavior provides common functionality for all trading behaviors
type BaseOrderBehavior struct {
	orderManager *order.Manager
	side         bitso.OrderSide
	state        *TradingState
}

// NewBaseOrderBehavior creates a new base order behavior
func NewBaseOrderBehavior(side bitso.OrderSide, orderManager *order.Manager) *BaseOrderBehavior {
	return &BaseOrderBehavior{
		orderManager: orderManager,
		side:         side,
		state: &TradingState{
			IsFirstOrder: true,
			IsFirstTrade: true,
		},
	}
}

// IsFirstOrder returns whether this is the first order
func (b *BaseOrderBehavior) IsFirstOrder() bool {
	return b.state.IsFirstOrder
}

// SetFirstOrder sets the first order flag
func (b *BaseOrderBehavior) SetFirstOrder(value bool) {
	b.state.IsFirstOrder = value
}

// IsFirstTrade returns whether this is the first trade
func (b *BaseOrderBehavior) IsFirstTrade() bool {
	return b.state.IsFirstTrade
}

// SetFirstTrade sets the first trade flag
func (b *BaseOrderBehavior) SetFirstTrade(value bool) {
	b.state.IsFirstTrade = value
}

// GetSide returns the order side
func (b *BaseOrderBehavior) GetSide() bitso.OrderSide {
	return b.side
}
