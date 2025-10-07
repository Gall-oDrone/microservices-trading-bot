package behaviors

import (
	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/database"
	"fmt"
)

// BuyBehavior implements the trading behavior for buying
type BuyBehavior struct {
	*BaseOrderBehavior
	orderType   bitso.OrderType
	amount      float64
	rate        float64
	book        bitso.Book
	bitsoClient *bitso.Client
}

// NewBuyBehavior creates a new buy behavior
func NewBuyBehavior(bitsoClient *bitso.Client, dbClient database.Client) *BuyBehavior {
	return &BuyBehavior{
		BaseOrderBehavior: NewBaseOrderBehavior(bitso.OrderSide(1), dbClient),
		orderType:         bitso.OrderTypeLimit,
		amount:            0,
		rate:              0,
		book:              bitso.Book{},
		bitsoClient:       bitsoClient,
	}
}

// CheckFunds verifies if there are sufficient funds for the trade
func (b *BuyBehavior) CheckFunds(min float64, balance *bitso.Balance) error {
	// For buy orders, we need to check if we have enough funds to cover the order
	if balance.Available.Float64() < min {
		return fmt.Errorf("insufficient funds: %v", balance.Available.Float64())
	}
	return nil
}

// CalculateOptimalRate calculates the optimal rate for a buy order
func (b *BuyBehavior) CalculateOptimalRate(ticker *bitso.Ticker, limit, amount float64, fee bitso.Fee, marketTrade string) (float64, error) {
	if ticker == nil {
		return 0, fmt.Errorf("ticker is nil")
	}

	// For buy orders, we want to place the order slightly below the current ask
	optimalRate := ticker.Ask.Float64() // * (1 - fee.TakerFeeDecimal.Float64())

	// Ensure the rate is not below the limit
	if limit > 0 && optimalRate < limit {
		optimalRate = limit
	}

	return optimalRate, nil
}

// ConfigureOrder sets up the order parameters
func (b *BuyBehavior) ConfigureOrder(book bitso.Book, orderType bitso.OrderType, amount, rate float64) {
	b.book = book
	b.orderType = orderType
	b.amount = amount
	b.rate = rate
}

// GetBook returns the configured book
func (b *BuyBehavior) GetBook() bitso.Book {
	return b.book
}

// GetOrderType returns the configured order type
func (b *BuyBehavior) GetOrderType() bitso.OrderType {
	return b.orderType
}

// GetAmount returns the configured amount
func (b *BuyBehavior) GetAmount() float64 {
	return b.amount
}

// GetRate returns the configured rate
func (b *BuyBehavior) GetRate() float64 {
	return b.rate
}
