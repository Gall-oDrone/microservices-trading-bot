package behaviors

import (
	"bitso_trading_bot/internal/database"
	"bitso_trading_bot/internal/order"
	"bitso_trading_bot/internal/utils"
	"bitso_trading_bot/pkg/bitso"
	"bitso_trading_bot/table"
	"fmt"
)

// BuyBehavior implements the trading behavior for buying
type BuyBehavior struct {
	*BaseOrderBehavior
	orderType bitso.OrderType
	amount    float64
	rate      float64
	book      bitso.Book
}

// NewBuyBehavior creates a new buy behavior
func NewBuyBehavior(balance bitso.Balance, bitsoClient *bitso.Client, orderManager *order.Manager) *BuyBehavior {
	return &BuyBehavior{
		BaseOrderBehavior: NewBaseOrderBehavior(bitso.OrderSide(1), orderManager),
		orderType:         bitso.OrderTypeLimit,
		amount:            0,
		rate:              0,
		book:              bitso.Book{},
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
func (b *BuyBehavior) CalculateOptimalRate(ticker *bitso.Ticker, limit, amount float64, fee bitso.Fee, marketTrade string, tableData *table.TableData) (float64, error) {
	if ticker == nil {
		return 0, fmt.Errorf("ticker is nil")
	}

	// For buy orders, we want to place the order slightly below the current ask
	optimalRate := ticker.Ask.Float64() * (1 - fee.TakerFeeDecimal.Float64())

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

// ExecuteOrder places a buy order
func (b *BuyBehavior) ExecuteOrder(ticker *bitso.Ticker, amount, rate float64, oid string, orderStatus bitso.OrderStatus, orderType bitso.OrderType, dbClient *database.RedisClient) (string, error) {
	if ticker == nil {
		return "", fmt.Errorf("ticker is nil")
	}

	// Create order placement object
	op := &bitso.OrderPlacement{
		Book:  b.book,
		Side:  b.side,
		Type:  b.orderType,
		Major: bitso.ToMonetary(amount),
		Minor: bitso.ToMonetary(amount * rate),
		Price: bitso.ToMonetary(rate),
	}
	// Place the order
	oid, err := b.orderManager.GetBitsoClient().PlaceOrder(op)
	if err != nil {
		return "", fmt.Errorf("failed to place order: %v", err)
	}

	// Save the order to DB
	err = b.orderManager.SaveOrder(oid, b.side, orderStatus)
	if err != nil {
		return "", fmt.Errorf("failed to save order: %v", err)
	}

	// Set order TTL
	err = b.orderManager.SetOrderWithTTL(oid)
	if err != nil {
		return "", fmt.Errorf("failed to set order TTL: %v", err)
	}

	return oid, nil
}

// ExecuteMakerStrategy implements the maker strategy for buying
func (b *BuyBehavior) ExecuteMakerStrategy(balance *bitso.Balance, ticker *bitso.Ticker, minTradeAmount float64, fee bitso.Fee, tableData *table.TableData) error {
	// Get user balance
	balances, err := b.orderManager.GetBitsoClient().Balances(nil)
	if err != nil {
		return fmt.Errorf("failed to get user balances: %v", err)
	}
	balance, err = utils.GetBalance(b.book.Minor(), balances)
	if err != nil {
		return fmt.Errorf("failed to get user balance: %v", err)
	}

	err = b.CheckFunds(minTradeAmount, balance)
	if err != nil {
		return fmt.Errorf("failed to check funds: %v", err)
	}

	// Get ticker
	ticker, err = b.orderManager.GetBitsoClient().Ticker(&b.book)
	if err != nil {
		return fmt.Errorf("failed to get API ticker: %v", err)
	}

	// Calculate optimal rate
	rate, err := b.CalculateOptimalRate(ticker, minTradeAmount, balance.Available.Float64(), fee, "", tableData)
	if err != nil {
		return fmt.Errorf("failed to calculate optimal rate: %v", err)
	}

	// Configure order
	b.ConfigureOrder(b.book, bitso.OrderTypeLimit, balance.Available.Float64(), rate)

	// Execute order
	oid, err := b.ExecuteOrder(ticker, balance.Available.Float64(), rate, "", bitso.OrderStatusOpen, bitso.OrderTypeLimit, b.orderManager.GetDBClient())
	if err != nil {
		return fmt.Errorf("failed to execute order: %v", err)
	}
	fmt.Println("Buy order placed: ", oid)

	return nil
}
