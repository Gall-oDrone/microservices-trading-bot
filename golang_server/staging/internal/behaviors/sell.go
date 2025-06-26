package behaviors

import (
	"fmt"

	"bitso_trading_bot/internal/database"
	"bitso_trading_bot/internal/order"
	"bitso_trading_bot/internal/utils"
	"bitso_trading_bot/pkg/bitso"
	"bitso_trading_bot/table"
)

// SellBehavior implements the trading behavior for selling
type SellBehavior struct {
	*BaseOrderBehavior
	orderType bitso.OrderType
	amount    float64
	rate      float64
	book      bitso.Book
}

// NewSellBehavior creates a new sell behavior
func NewSellBehavior(balance bitso.Balance, bitsoClient *bitso.Client, orderManager *order.Manager) *SellBehavior {
	return &SellBehavior{
		BaseOrderBehavior: NewBaseOrderBehavior(bitso.OrderSide(2), orderManager),
		orderType:         bitso.OrderTypeLimit,
		amount:            0,
		rate:              0,
		book:              bitso.Book{},
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
func (s *SellBehavior) CalculateOptimalRate(ticker *bitso.Ticker, limit, amount float64, fee bitso.Fee, marketTrade string, tableData *table.TableData) (float64, error) {
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

// ExecuteOrder places a sell order
func (s *SellBehavior) ExecuteOrder(ticker *bitso.Ticker, amount, rate float64, oid string, orderStatus bitso.OrderStatus, orderType bitso.OrderType, dbClient *database.RedisClient) (string, error) {
	if ticker == nil {
		return "", fmt.Errorf("ticker is nil")
	}

	// Create order placement object
	op := &bitso.OrderPlacement{
		Book:  s.book,
		Side:  s.side,
		Type:  s.orderType,
		Major: bitso.ToMonetary(amount),
		Minor: bitso.ToMonetary(amount * rate),
		Price: bitso.ToMonetary(rate),
	}
	// Place the order
	oid, err := s.orderManager.GetBitsoClient().PlaceOrder(op)
	if err != nil {
		return "", fmt.Errorf("failed to place order: %v", err)
	}

	// Save the order to DB
	err = s.orderManager.SaveOrder(oid, s.side, orderStatus)
	if err != nil {
		return "", fmt.Errorf("failed to save order: %v", err)
	}

	// Set order TTL
	err = s.orderManager.SetOrderWithTTL(oid)
	if err != nil {
		return "", fmt.Errorf("failed to set order TTL: %v", err)
	}

	return oid, nil
}

// ExecuteMakerStrategy implements the maker strategy for selling
func (s *SellBehavior) ExecuteMakerStrategy(balance *bitso.Balance, ticker *bitso.Ticker, minTradeAmount float64, fee bitso.Fee, tableData *table.TableData) error {
	// Get user balance
	balances, err := s.orderManager.GetBitsoClient().Balances(nil)
	if err != nil {
		return fmt.Errorf("failed to get user balances: %v", err)
	}
	balance, err = utils.GetBalance(s.book.Major(), balances)
	if err != nil {
		return fmt.Errorf("failed to get user balance: %v", err)
	}

	err = s.CheckFunds(minTradeAmount, balance)
	if err != nil {
		return fmt.Errorf("failed to check funds: %v", err)
	}

	// Get ticker
	ticker, err = s.orderManager.GetBitsoClient().Ticker(&s.book)
	if err != nil {
		return fmt.Errorf("failed to get API ticker: %v", err)
	}

	// Calculate optimal rate
	rate, err := s.CalculateOptimalRate(ticker, minTradeAmount, balance.Available.Float64(), fee, "", tableData)
	if err != nil {
		return fmt.Errorf("failed to calculate optimal rate: %v", err)
	}

	// Configure order
	s.ConfigureOrder(s.book, bitso.OrderTypeLimit, balance.Available.Float64(), rate)

	// Execute order
	oid, err := s.ExecuteOrder(ticker, balance.Available.Float64(), rate, "", bitso.OrderStatusOpen, bitso.OrderTypeLimit, s.orderManager.GetDBClient())
	if err != nil {
		return fmt.Errorf("failed to execute order: %v", err)
	}
	fmt.Println("Sell order placed: ", oid)

	return nil
}
