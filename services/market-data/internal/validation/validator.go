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

	// Validate order IDs
	if v.config.EnableStringValidation {
		if len(trade.MakerOrderID) > v.config.MaxStringLength {
			return fmt.Errorf("maker order ID length exceeds maximum: %d", v.config.MaxStringLength)
		}
		if len(trade.TakerOrderID) > v.config.MaxStringLength {
			return fmt.Errorf("taker order ID length exceeds maximum: %d", v.config.MaxStringLength)
		}
	}

	// Validate timestamps
	if v.config.EnableTimeValidation {
		if trade.Timestamp.IsZero() {
			return fmt.Errorf("trade timestamp cannot be zero")
		}
		if trade.ReceivedAt.IsZero() {
			return fmt.Errorf("trade received at cannot be zero")
		}
		if trade.CreatedAt == 0 {
			return fmt.Errorf("trade created at cannot be zero")
		}

		// Check if trade is too old
		maxAge := time.Duration(v.config.MaxAgeMinutes) * time.Minute
		if time.Since(trade.Timestamp) > maxAge {
			return fmt.Errorf("trade is too old: %v > %v", time.Since(trade.Timestamp), maxAge)
		}
	}

	// Validate source
	if trade.Source == "" {
		return fmt.Errorf("trade source cannot be empty")
	}

	return nil
}

// ValidateOrderBook validates an order book
func (v *MarketDataValidator) ValidateOrderBook(orderBook *bitso.OrderBook) error {
	if orderBook == nil {
		return fmt.Errorf("order book cannot be nil")
	}

	// Validate book
	if orderBook.Book == nil {
		return fmt.Errorf("order book book cannot be nil")
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
			if orderBook.Bids[i-1].Price < orderBook.Bids[i].Price {
				return fmt.Errorf("bid prices not in descending order: %f < %f", orderBook.Bids[i-1].Price, orderBook.Bids[i].Price)
			}
		}

		// Validate ask prices are in ascending order
		for i := 1; i < len(orderBook.Asks); i++ {
			if orderBook.Asks[i-1].Price > orderBook.Asks[i].Price {
				return fmt.Errorf("ask prices not in ascending order: %f > %f", orderBook.Asks[i-1].Price, orderBook.Asks[i].Price)
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

	// Validate book
	if ticker.Book == nil {
		return fmt.Errorf("ticker book cannot be nil")
	}

	// Validate last price
	if v.config.EnablePriceValidation {
		if ticker.Last <= 0 {
			return fmt.Errorf("ticker last price must be positive: %f", ticker.Last)
		}
		if ticker.Last < v.config.MinPrice {
			return fmt.Errorf("ticker last price below minimum: %f < %f", ticker.Last, v.config.MinPrice)
		}
		if ticker.Last > v.config.MaxPrice {
			return fmt.Errorf("ticker last price above maximum: %f > %f", ticker.Last, v.config.MaxPrice)
		}
	}

	// Validate high price
	if v.config.EnablePriceValidation {
		if ticker.High <= 0 {
			return fmt.Errorf("ticker high price must be positive: %f", ticker.High)
		}
		if ticker.High < v.config.MinPrice {
			return fmt.Errorf("ticker high price below minimum: %f < %f", ticker.High, v.config.MinPrice)
		}
		if ticker.High > v.config.MaxPrice {
			return fmt.Errorf("ticker high price above maximum: %f > %f", ticker.High, v.config.MaxPrice)
		}
	}

	// Validate low price
	if v.config.EnablePriceValidation {
		if ticker.Low <= 0 {
			return fmt.Errorf("ticker low price must be positive: %f", ticker.Low)
		}
		if ticker.Low < v.config.MinPrice {
			return fmt.Errorf("ticker low price below minimum: %f < %f", ticker.Low, v.config.MinPrice)
		}
		if ticker.Low > v.config.MaxPrice {
			return fmt.Errorf("ticker low price above maximum: %f > %f", ticker.Low, v.config.MaxPrice)
		}
	}

	// Validate price relationships
	if ticker.Low > ticker.High {
		return fmt.Errorf("ticker low price (%f) is greater than high price (%f)", ticker.Low, ticker.High)
	}

	if ticker.Last < ticker.Low || ticker.Last > ticker.High {
		return fmt.Errorf("ticker last price (%f) is outside high/low range (%f-%f)", ticker.Last, ticker.Low, ticker.High)
	}

	// Validate volume
	if v.config.EnableVolumeValidation {
		if ticker.Volume < 0 {
			return fmt.Errorf("ticker volume cannot be negative: %f", ticker.Volume)
		}
		if ticker.Volume > v.config.MaxVolume {
			return fmt.Errorf("ticker volume above maximum: %f > %f", ticker.Volume, v.config.MaxVolume)
		}
	}

	// Validate VWAP
	if ticker.VWAP < 0 {
		return fmt.Errorf("ticker VWAP cannot be negative: %f", ticker.VWAP)
	}

	return nil
}

// ValidateWebSocketTrade validates a WebSocket trade
func (v *MarketDataValidator) ValidateWebSocketTrade(wsTrade *bitso.WebSocketTrade) error {
	if wsTrade == nil {
		return fmt.Errorf("WebSocket trade cannot be nil")
	}

	// Validate book
	if wsTrade.Book == nil {
		return fmt.Errorf("WebSocket trade book cannot be nil")
	}

	// Validate payload
	if len(wsTrade.Payload) == 0 {
		return fmt.Errorf("WebSocket trade payload cannot be empty")
	}

	// Validate each payload entry
	for i, payload := range wsTrade.Payload {
		if err := v.validateWebSocketTradePayload(payload, i); err != nil {
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

	// Validate book
	if wsOrder.Book == nil {
		return fmt.Errorf("WebSocket order book cannot be nil")
	}

	// Validate payload
	if len(wsOrder.Payload) == 0 {
		return fmt.Errorf("WebSocket order payload cannot be empty")
	}

	// Validate each payload entry
	for i, payload := range wsOrder.Payload {
		if err := v.validateWebSocketOrderPayload(payload, i); err != nil {
			return fmt.Errorf("invalid payload entry %d: %w", i, err)
		}
	}

	// Validate sent timestamp
	if wsOrder.Sent == 0 {
		return fmt.Errorf("WebSocket order sent timestamp cannot be zero")
	}

	return nil
}

// ValidateWebSocketDiffOrder validates a WebSocket diff order
func (v *MarketDataValidator) ValidateWebSocketDiffOrder(wsDiffOrder *bitso.WebSocketDiffOrder) error {
	if wsDiffOrder == nil {
		return fmt.Errorf("WebSocket diff order cannot be nil")
	}

	// Validate book
	if wsDiffOrder.Book == nil {
		return fmt.Errorf("WebSocket diff order book cannot be nil")
	}

	// Validate payload
	if len(wsDiffOrder.Payload) == 0 {
		return fmt.Errorf("WebSocket diff order payload cannot be empty")
	}

	// Validate each payload entry
	for i, payload := range wsDiffOrder.Payload {
		if err := v.validateWebSocketDiffOrderPayload(payload, i); err != nil {
			return fmt.Errorf("invalid payload entry %d: %w", i, err)
		}
	}

	// Validate sent timestamp
	if wsDiffOrder.Sent == 0 {
		return fmt.Errorf("WebSocket diff order sent timestamp cannot be zero")
	}

	return nil
}

// validateOrderBookLevels validates order book levels
func (v *MarketDataValidator) validateOrderBookLevels(levels []bitso.OrderBookLevel, levelType string) error {
	if len(levels) > v.config.MaxOrderBookLevels {
		return fmt.Errorf("too many %s levels: %d > %d", levelType, len(levels), v.config.MaxOrderBookLevels)
	}

	for i, level := range levels {
		if level.Price <= 0 {
			return fmt.Errorf("invalid %s level %d price: %f", levelType, i, level.Price)
		}
		if level.Amount <= 0 {
			return fmt.Errorf("invalid %s level %d amount: %f", levelType, i, level.Amount)
		}
	}

	return nil
}

// validateWebSocketTradePayload validates a WebSocket trade payload
func (v *MarketDataValidator) validateWebSocketTradePayload(payload bitso.WebSocketTradePayload, index int) error {
	// Validate TID
	if payload.TID == 0 {
		return fmt.Errorf("TID cannot be zero")
	}

	// Validate price
	if v.config.EnablePriceValidation {
		if payload.Price.Value <= 0 {
			return fmt.Errorf("price must be positive: %f", payload.Price.Value)
		}
		if payload.Price.Value < v.config.MinPrice {
			return fmt.Errorf("price below minimum: %f < %f", payload.Price.Value, v.config.MinPrice)
		}
		if payload.Price.Value > v.config.MaxPrice {
			return fmt.Errorf("price above maximum: %f > %f", payload.Price.Value, v.config.MaxPrice)
		}
	}

	// Validate amount
	if v.config.EnableVolumeValidation {
		if payload.Amount.Value <= 0 {
			return fmt.Errorf("amount must be positive: %f", payload.Amount.Value)
		}
		if payload.Amount.Value < v.config.MinAmount {
			return fmt.Errorf("amount below minimum: %f < %f", payload.Amount.Value, v.config.MinAmount)
		}
		if payload.Amount.Value > v.config.MaxAmount {
			return fmt.Errorf("amount above maximum: %f > %f", payload.Amount.Value, v.config.MaxAmount)
		}
	}

	// Validate value
	if payload.Value.Value < 0 {
		return fmt.Errorf("value cannot be negative: %f", payload.Value.Value)
	}

	// Validate maker side
	if payload.MakerSide != "0" && payload.MakerSide != "1" {
		return fmt.Errorf("invalid maker side: %s", payload.MakerSide)
	}

	// Validate order IDs
	if v.config.EnableStringValidation {
		if len(payload.MakerOrderID) > v.config.MaxStringLength {
			return fmt.Errorf("maker order ID length exceeds maximum: %d", v.config.MaxStringLength)
		}
		if len(payload.TakerOrderID) > v.config.MaxStringLength {
			return fmt.Errorf("taker order ID length exceeds maximum: %d", v.config.MaxStringLength)
		}
	}

	// Validate creation timestamp
	if payload.CreationTimestamp == 0 {
		return fmt.Errorf("creation timestamp cannot be zero")
	}

	return nil
}

// validateWebSocketOrderPayload validates a WebSocket order payload
func (v *MarketDataValidator) validateWebSocketOrderPayload(payload bitso.WebSocketOrderPayload, index int) error {
	// Validate OID
	if payload.OID == "" {
		return fmt.Errorf("OID cannot be empty")
	}

	if v.config.EnableStringValidation && len(payload.OID) > v.config.MaxStringLength {
		return fmt.Errorf("OID length exceeds maximum: %d", v.config.MaxStringLength)
	}

	// Validate side
	if payload.Side != "0" && payload.Side != "1" {
		return fmt.Errorf("invalid side: %s", payload.Side)
	}

	// Validate price
	if v.config.EnablePriceValidation {
		if payload.Price.Value <= 0 {
			return fmt.Errorf("price must be positive: %f", payload.Price.Value)
		}
		if payload.Price.Value < v.config.MinPrice {
			return fmt.Errorf("price below minimum: %f < %f", payload.Price.Value, v.config.MinPrice)
		}
		if payload.Price.Value > v.config.MaxPrice {
			return fmt.Errorf("price above maximum: %f > %f", payload.Price.Value, v.config.MaxPrice)
		}
	}

	// Validate amount
	if v.config.EnableVolumeValidation {
		if payload.Amount.Value <= 0 {
			return fmt.Errorf("amount must be positive: %f", payload.Amount.Value)
		}
		if payload.Amount.Value < v.config.MinAmount {
			return fmt.Errorf("amount below minimum: %f < %f", payload.Amount.Value, v.config.MinAmount)
		}
		if payload.Amount.Value > v.config.MaxAmount {
			return fmt.Errorf("amount above maximum: %f > %f", payload.Amount.Value, v.config.MaxAmount)
		}
	}

	// Validate status
	if payload.Status == "" {
		return fmt.Errorf("status cannot be empty")
	}

	if v.config.EnableStringValidation && len(payload.Status) > v.config.MaxStringLength {
		return fmt.Errorf("status length exceeds maximum: %d", v.config.MaxStringLength)
	}

	return nil
}

// validateWebSocketDiffOrderPayload validates a WebSocket diff order payload
func (v *MarketDataValidator) validateWebSocketDiffOrderPayload(payload bitso.WebSocketDiffOrderPayload, index int) error {
	// Validate OID
	if payload.OID == "" {
		return fmt.Errorf("OID cannot be empty")
	}

	if v.config.EnableStringValidation && len(payload.OID) > v.config.MaxStringLength {
		return fmt.Errorf("OID length exceeds maximum: %d", v.config.MaxStringLength)
	}

	// Validate side
	if payload.Side != "0" && payload.Side != "1" {
		return fmt.Errorf("invalid side: %s", payload.Side)
	}

	// Validate price (can be 0 for market orders)
	if v.config.EnablePriceValidation && payload.Price.Value > 0 {
		if payload.Price.Value < v.config.MinPrice {
			return fmt.Errorf("price below minimum: %f < %f", payload.Price.Value, v.config.MinPrice)
		}
		if payload.Price.Value > v.config.MaxPrice {
			return fmt.Errorf("price above maximum: %f > %f", payload.Price.Value, v.config.MaxPrice)
		}
	}

	// Validate amount
	if v.config.EnableVolumeValidation {
		if payload.Amount.Value < 0 {
			return fmt.Errorf("amount cannot be negative: %f", payload.Amount.Value)
		}
		if payload.Amount.Value > v.config.MaxAmount {
			return fmt.Errorf("amount above maximum: %f > %f", payload.Amount.Value, v.config.MaxAmount)
		}
	}

	// Validate status
	if payload.Status == "" {
		return fmt.Errorf("status cannot be empty")
	}

	if v.config.EnableStringValidation && len(payload.Status) > v.config.MaxStringLength {
		return fmt.Errorf("status length exceeds maximum: %d", v.config.MaxStringLength)
	}

	return nil
}
