package behaviors

import (
	"fmt"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/database"
)

// SellBehavior implements the trading behavior for selling
type SellBehavior struct {
	*BaseOrderBehavior
	orderType   bitso.OrderType
	amount      float64
	rate        float64
	book        bitso.Book
	bitsoClient *bitso.Client
}

// NewSellBehavior creates a new sell behavior
func NewSellBehavior(bitsoClient *bitso.Client, dbClient database.Client) *SellBehavior {
	return &SellBehavior{
		BaseOrderBehavior: NewBaseOrderBehavior(bitso.OrderSide(2), dbClient),
		orderType:         bitso.OrderTypeLimit,
		amount:            0,
		rate:              0,
		book:              bitso.Book{},
		bitsoClient:       bitsoClient,
	}
}

// CheckFunds verifies if there are sufficient funds for the trade
func (s *SellBehavior) CheckFunds(min float64, balance *bitso.Balance) error {
	// For sell orders, we need to check if we have enough major currency to sell
	if balance.Available.Float64() < min {
		return fmt.Errorf("insufficient funds: %v", balance.Available.Float64())
	}
	return nil
}

// CalculateOptimalRate calculates the optimal rate for a sell order
func (s *SellBehavior) CalculateOptimalRate(ticker *bitso.Ticker, limit, amount float64, fee bitso.Fee, marketTrade string) (float64, error) {
	if ticker == nil {
		return 0, fmt.Errorf("ticker is nil")
	}

	// For sell orders, we want to place the order slightly above the current bid
	optimalRate := ticker.Bid.Float64() * (1 + fee.TakerFeeDecimal.Float64())

	// Ensure the rate is not below the limit
	if limit > 0 && optimalRate < limit {
		optimalRate = limit
	}

	return optimalRate, nil
}

// ConfigureOrder sets up the order parameters
func (s *SellBehavior) ConfigureOrder(book bitso.Book, orderType bitso.OrderType, amount, rate float64) {
	s.book = book
	s.orderType = orderType
	s.amount = amount
	s.rate = rate
}

// GetBook returns the configured book
func (s *SellBehavior) GetBook() bitso.Book {
	return s.book
}

// GetOrderType returns the configured order type
func (s *SellBehavior) GetOrderType() bitso.OrderType {
	return s.orderType
}

// GetAmount returns the configured amount
func (s *SellBehavior) GetAmount() float64 {
	return s.amount
}

// GetRate returns the configured rate
func (s *SellBehavior) GetRate() float64 {
	return s.rate
}
