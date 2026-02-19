package validation

import (
	"fmt"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
)

// Validator defines the interface for data validation
type Validator interface {
	ValidateTrade(trade *models.TradeEvent) error
	ValidateOrderBook(orderBook *bitso.OrderBook) error
	ValidateTicker(ticker *bitso.Ticker) error
	ValidateWebSocketTrade(wsTrade *bitso.WebSocketTrade) error
	ValidateWebSocketOrder(wsOrder *bitso.WebSocketOrder) error
	ValidateWebSocketDiffOrder(wsDiffOrder *bitso.WebSocketDiffOrder) error
}

// MarketDataValidator implements the Validator interface
type MarketDataValidator struct {
	config *ValidationConfig
}

// ValidationConfig holds configuration for validation
type ValidationConfig struct {
	// Price validation
	MinPrice float64 `json:"min_price"`
	MaxPrice float64 `json:"max_price"`

	// Volume validation
	MinVolume float64 `json:"min_volume"`
	MaxVolume float64 `json:"max_volume"`

	// Amount validation
	MinAmount float64 `json:"min_amount"`
	MaxAmount float64 `json:"max_amount"`

	// Time validation
	MaxAgeMinutes int `json:"max_age_minutes"`

	// Order book validation
	MaxOrderBookDepth  int `json:"max_orderbook_depth"`
	MaxOrderBookLevels int `json:"max_orderbook_levels"`

	// String validation
	MaxStringLength int `json:"max_string_length"`

	// Enable validation
	EnablePriceValidation     bool `json:"enable_price_validation"`
	EnableVolumeValidation    bool `json:"enable_volume_validation"`
	EnableTimeValidation      bool `json:"enable_time_validation"`
	EnableStringValidation    bool `json:"enable_string_validation"`
	EnableOrderBookValidation bool `json:"enable_orderbook_validation"`
}

// DefaultValidationConfig returns default validation configuration
func DefaultValidationConfig() *ValidationConfig {
	return &ValidationConfig{
		MinPrice:                  0.01,
		MaxPrice:                  1000000.0,
		MinVolume:                 0.0,
		MaxVolume:                 1000000.0,
		MinAmount:                 0.00000001,
		MaxAmount:                 1000000.0,
		MaxAgeMinutes:             60,
		MaxOrderBookDepth:         100,
		MaxOrderBookLevels:        50,
		MaxStringLength:           1000,
		EnablePriceValidation:     true,
		EnableVolumeValidation:    true,
		EnableTimeValidation:      true,
		EnableStringValidation:    true,
		EnableOrderBookValidation: true,
	}
}

// NewMarketDataValidator creates a new market data validator
func NewMarketDataValidator(config *ValidationConfig) *MarketDataValidator {
	if config == nil {
		config = DefaultValidationConfig()
	}

	return &MarketDataValidator{
		config: config,
	}
}

// ValidateTrade validates a trade event
func (v *MarketDataValidator) ValidateTrade(trade *models.TradeEvent) error {
	if trade == nil {
		return fmt.Errorf("trade cannot be nil")
	}

	// Validate trade ID
	if trade.ID == 0 {
		return fmt.Errorf("trade ID cannot be zero")
	}

	// Validate book
	if trade.Book == "" {
		return fmt.Errorf("trade book cannot be empty")
	}

	if v.config.EnableStringValidation && len(trade.Book) > v.config.MaxStringLength {
		return fmt.Errorf("trade book length exceeds maximum: %d", v.config.MaxStringLength)
	}

	// Validate price
	if v.config.EnablePriceValidation {
		if trade.Price <= 0 {
			return fmt.Errorf("trade price must be positive: %f", trade.Price)
		}
		if trade.Price < v.config.MinPrice {
			return fmt.Errorf("trade price below minimum: %f < %f", trade.Price, v.config.MinPrice)
		}
		if trade.Price > v.config.MaxPrice {
			return fmt.Errorf("trade price above maximum: %f > %f", trade.Price, v.config.MaxPrice)
		}
	}

	// Validate amount
	if v.config.EnableVolumeValidation {
		if trade.Amount <= 0 {
			return fmt.Errorf("trade amount must be positive: %f", trade.Amount)
		}
		if trade.Amount < v.config.MinAmount {
			return fmt.Errorf("trade amount below minimum: %f < %f", trade.Amount, v.config.MinAmount)
		}
		if trade.Amount > v.config.MaxAmount {
			return fmt.Errorf("trade amount above maximum: %f > %f", trade.Amount, v.config.MaxAmount)
		}
	}

	// Validate value
	if trade.Value < 0 {
		return fmt.Errorf("trade value cannot be negative: %f", trade.Value)
	}

	// Validate maker side
	if trade.MakerSide != "buy" && trade.MakerSide != "sell" {
		return fmt.Errorf("invalid maker side: %s", trade.MakerSide)
	}

	// Validate timestamps
	if v.config.EnableTimeValidation {
		if trade.Timestamp.IsZero() {
			return fmt.Errorf("trade timestamp cannot be zero")
		}
		if trade.ReceivedAt.IsZero() {
			return fmt.Errorf("trade received at cannot be zero")
		}
		if trade.CreatedAtMillis == 0 {
			return fmt.Errorf("trade created at cannot be zero")
		}

		// Check if trade is too old
		maxAge := time.Duration(v.config.MaxAgeMinutes) * time.Minute
		if time.Since(trade.Timestamp) > maxAge {
			return fmt.Errorf("trade is too old: %v > %v", time.Since(trade.Timestamp), maxAge)
		}
	}

	return nil
}

// ValidateOrderBook validates an order book
func (v *MarketDataValidator) ValidateOrderBook(orderBook *bitso.OrderBook) error {
	if orderBook == nil {
		return fmt.Errorf("order book cannot be nil")
	}

	if v.config.EnableOrderBookValidation {
		// Validate bids
		if err := v.validateOrderBookLevels(orderBook.Bids, "bid"); err != nil {
			return fmt.Errorf("invalid bids: %w", err)
		}

		// Validate asks
		if err := v.validateOrderBookLevels(orderBook.Asks, "ask"); err != nil {
			return fmt.Errorf("invalid asks: %w", err)
		}

		// Check if order book is empty
		if len(orderBook.Bids) == 0 && len(orderBook.Asks) == 0 {
			return fmt.Errorf("order book cannot be empty")
		}

		// Check depth limits
		totalDepth := len(orderBook.Bids) + len(orderBook.Asks)
		if totalDepth > v.config.MaxOrderBookDepth {
			return fmt.Errorf("order book depth exceeds maximum: %d > %d", totalDepth, v.config.MaxOrderBookDepth)
		}

		// Validate bid prices are in descending order
		for i := 1; i < len(orderBook.Bids); i++ {
			if orderBook.Bids[i-1].Price.Float64() < orderBook.Bids[i].Price.Float64() {
				return fmt.Errorf("bid prices not in descending order: %f < %f", orderBook.Bids[i-1].Price.Float64(), orderBook.Bids[i].Price.Float64())
			}
		}

		// Validate ask prices are in ascending order
		for i := 1; i < len(orderBook.Asks); i++ {
			if orderBook.Asks[i-1].Price.Float64() > orderBook.Asks[i].Price.Float64() {
				return fmt.Errorf("ask prices not in ascending order: %f > %f", orderBook.Asks[i-1].Price.Float64(), orderBook.Asks[i].Price.Float64())
			}
		}
	}

	return nil
}

// ValidateTicker validates a ticker
func (v *MarketDataValidator) ValidateTicker(ticker *bitso.Ticker) error {
	if ticker == nil {
		return fmt.Errorf("ticker cannot be nil")
	}

	// Validate book (Book is value type; check for zero)
	if ticker.Book.Major() == bitso.CurrencyNone && ticker.Book.Minor() == bitso.CurrencyNone {
		return fmt.Errorf("ticker book cannot be zero")
	}

	last := ticker.Last.Float64()
	high := ticker.High.Float64()
	low := ticker.Low.Float64()
	volume := ticker.Volume.Float64()
	vwap := ticker.Vwap.Float64()

	// Validate last price
	if v.config.EnablePriceValidation {
		if last <= 0 {
			return fmt.Errorf("ticker last price must be positive: %f", last)
		}
		if last < v.config.MinPrice {
			return fmt.Errorf("ticker last price below minimum: %f < %f", last, v.config.MinPrice)
		}
		if last > v.config.MaxPrice {
			return fmt.Errorf("ticker last price above maximum: %f > %f", last, v.config.MaxPrice)
		}
	}

	// Validate high price
	if v.config.EnablePriceValidation {
		if high <= 0 {
			return fmt.Errorf("ticker high price must be positive: %f", high)
		}
		if high < v.config.MinPrice {
			return fmt.Errorf("ticker high price below minimum: %f < %f", high, v.config.MinPrice)
		}
		if high > v.config.MaxPrice {
			return fmt.Errorf("ticker high price above maximum: %f > %f", high, v.config.MaxPrice)
		}
	}

	// Validate low price
	if v.config.EnablePriceValidation {
		if low <= 0 {
			return fmt.Errorf("ticker low price must be positive: %f", low)
		}
		if low < v.config.MinPrice {
			return fmt.Errorf("ticker low price below minimum: %f < %f", low, v.config.MinPrice)
		}
		if low > v.config.MaxPrice {
			return fmt.Errorf("ticker low price above maximum: %f > %f", low, v.config.MaxPrice)
		}
	}

	// Validate price relationships
	if low > high {
		return fmt.Errorf("ticker low price (%f) is greater than high price (%f)", low, high)
	}

	if last < low || last > high {
		return fmt.Errorf("ticker last price (%f) is outside high/low range (%f-%f)", last, low, high)
	}

	// Validate volume
	if v.config.EnableVolumeValidation {
		if volume < 0 {
			return fmt.Errorf("ticker volume cannot be negative: %f", volume)
		}
		if volume > v.config.MaxVolume {
			return fmt.Errorf("ticker volume above maximum: %f > %f", volume, v.config.MaxVolume)
		}
	}

	// Validate VWAP
	if vwap < 0 {
		return fmt.Errorf("ticker VWAP cannot be negative: %f", vwap)
	}

	return nil
}

// ValidateWebSocketTrade validates a WebSocket trade
func (v *MarketDataValidator) ValidateWebSocketTrade(wsTrade *bitso.WebSocketTrade) error {
	if wsTrade == nil {
		return fmt.Errorf("WebSocket trade cannot be nil")
	}

	// Validate payload
	if len(wsTrade.Payload) == 0 {
		return fmt.Errorf("WebSocket trade payload cannot be empty")
	}

	// Validate each payload entry
	for i := range wsTrade.Payload {
		if err := v.validateWebSocketTradePayloadAt(wsTrade, i); err != nil {
			return fmt.Errorf("invalid payload entry %d: %w", i, err)
		}
	}

	// Validate sent timestamp
	if wsTrade.Sent == 0 {
		return fmt.Errorf("WebSocket trade sent timestamp cannot be zero")
	}

	return nil
}

// ValidateWebSocketOrder validates a WebSocket order
func (v *MarketDataValidator) ValidateWebSocketOrder(wsOrder *bitso.WebSocketOrder) error {
	if wsOrder == nil {
		return fmt.Errorf("WebSocket order cannot be nil")
	}

	// Validate payload (Payload is a struct with Bids and Asks slices)
	bidCount := len(wsOrder.Payload.Bids)
	askCount := len(wsOrder.Payload.Asks)
	for i := 0; i < bidCount; i++ {
		if err := v.validateWebSocketOrderBidAsk(wsOrder.Payload.Bids[i], i, "bid"); err != nil {
			return fmt.Errorf("invalid bid %d: %w", i, err)
		}
	}
	for i := 0; i < askCount; i++ {
		if err := v.validateWebSocketOrderBidAsk(wsOrder.Payload.Asks[i], i, "ask"); err != nil {
			return fmt.Errorf("invalid ask %d: %w", i, err)
		}
	}

	return nil
}

// ValidateWebSocketDiffOrder validates a WebSocket diff order
func (v *MarketDataValidator) ValidateWebSocketDiffOrder(wsDiffOrder *bitso.WebSocketDiffOrder) error {
	if wsDiffOrder == nil {
		return fmt.Errorf("WebSocket diff order cannot be nil")
	}

	// Validate payload
	if len(wsDiffOrder.Payload) == 0 {
		return fmt.Errorf("WebSocket diff order payload cannot be empty")
	}

	// Validate each payload entry
	for i := range wsDiffOrder.Payload {
		if err := v.validateWebSocketDiffOrderPayloadAt(wsDiffOrder, i); err != nil {
			return fmt.Errorf("invalid payload entry %d: %w", i, err)
		}
	}

	return nil
}

// validateOrderBookLevels validates order book levels (bitso.Order has Price, Amount as Monetary)
func (v *MarketDataValidator) validateOrderBookLevels(levels []bitso.Order, levelType string) error {
	if len(levels) > v.config.MaxOrderBookLevels {
		return fmt.Errorf("too many %s levels: %d > %d", levelType, len(levels), v.config.MaxOrderBookLevels)
	}

	for i, level := range levels {
		price := level.Price.Float64()
		amount := level.Amount.Float64()
		if price <= 0 {
			return fmt.Errorf("invalid %s level %d price: %f", levelType, i, price)
		}
		if amount <= 0 {
			return fmt.Errorf("invalid %s level %d amount: %f", levelType, i, amount)
		}
	}

	return nil
}

// validateWebSocketTradePayloadAt validates WebSocketTrade.Payload[index]
func (v *MarketDataValidator) validateWebSocketTradePayloadAt(wsTrade *bitso.WebSocketTrade, index int) error {
	payload := wsTrade.Payload[index]
	price := payload.Price.Float64()
	amount := payload.Amount.Float64()
	value := payload.Value.Float64()

	if payload.TID == 0 {
		return fmt.Errorf("TID cannot be zero")
	}
	if v.config.EnablePriceValidation {
		if price <= 0 {
			return fmt.Errorf("price must be positive: %f", price)
		}
		if price < v.config.MinPrice || price > v.config.MaxPrice {
			return fmt.Errorf("price out of range: %f", price)
		}
	}
	if v.config.EnableVolumeValidation {
		if amount <= 0 {
			return fmt.Errorf("amount must be positive: %f", amount)
		}
		if amount < v.config.MinAmount || amount > v.config.MaxAmount {
			return fmt.Errorf("amount out of range: %f", amount)
		}
	}
	if value < 0 {
		return fmt.Errorf("value cannot be negative: %f", value)
	}
	if payload.MakerSide != 0 && payload.MakerSide != 1 {
		return fmt.Errorf("invalid maker side: %d", payload.MakerSide)
	}
	if v.config.EnableStringValidation {
		if len(payload.MakerOrderID) > v.config.MaxStringLength || len(payload.TakerOrderID) > v.config.MaxStringLength {
			return fmt.Errorf("order ID length exceeds maximum")
		}
	}
	if payload.CreationTimestamp == 0 {
		return fmt.Errorf("creation timestamp cannot be zero")
	}
	return nil
}

// validateWebSocketOrderBidAsk validates a single bid or ask level from WebSocketOrder.Payload
func (v *MarketDataValidator) validateWebSocketOrderBidAsk(level struct {
	Amount    bitso.Monetary `json:"a"`
	OrderID   string         `json:"o"`
	Position  int            `json:"t"`
	Price     bitso.Monetary `json:"r"`
	Status    string         `json:"s"`
	Timestamp uint64         `json:"d"`
	Value     bitso.Monetary `json:"v"`
}, index int, levelType string) error {
	if level.OrderID == "" {
		return fmt.Errorf("%s level %d order ID cannot be empty", levelType, index)
	}
	if v.config.EnableStringValidation && len(level.OrderID) > v.config.MaxStringLength {
		return fmt.Errorf("%s level %d order ID length exceeds maximum", levelType, index)
	}
	if level.Status == "" {
		return fmt.Errorf("%s level %d status cannot be empty", levelType, index)
	}
	price := level.Price.Float64()
	amount := level.Amount.Float64()
	if v.config.EnablePriceValidation && price <= 0 {
		return fmt.Errorf("%s level %d price must be positive: %f", levelType, index, price)
	}
	if v.config.EnableVolumeValidation && amount <= 0 {
		return fmt.Errorf("%s level %d amount must be positive: %f", levelType, index, amount)
	}
	return nil
}

// validateWebSocketDiffOrderPayloadAt validates WebSocketDiffOrder.Payload[index]
func (v *MarketDataValidator) validateWebSocketDiffOrderPayloadAt(wsDiffOrder *bitso.WebSocketDiffOrder, index int) error {
	payload := wsDiffOrder.Payload[index]
	if payload.OrderID == "" {
		return fmt.Errorf("order ID cannot be empty")
	}
	if v.config.EnableStringValidation && len(payload.OrderID) > v.config.MaxStringLength {
		return fmt.Errorf("order ID length exceeds maximum: %d", v.config.MaxStringLength)
	}
	if payload.Status == "" {
		return fmt.Errorf("status cannot be empty")
	}
	price := payload.Price.Float64()
	amount := payload.Amount.Float64()
	if v.config.EnablePriceValidation && price < 0 {
		return fmt.Errorf("price cannot be negative: %f", price)
	}
	if v.config.EnableVolumeValidation && amount < 0 {
		return fmt.Errorf("amount cannot be negative: %f", amount)
	}
	return nil
}
