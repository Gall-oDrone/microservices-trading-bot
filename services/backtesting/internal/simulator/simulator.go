package simulator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/models"
	sharedModels "bitso-trading-platform/shared/pkg/models"
)

// MarketSimulator defines the interface for market simulation
type MarketSimulator interface {
	// Initialize initializes the simulator with configuration
	Initialize(ctx context.Context, config *SimulatorConfig) error

	// ProcessEvent processes a market event
	ProcessEvent(event *models.MarketEvent) error

	// ExecuteOrder executes an order and returns execution details
	ExecuteOrder(order *sharedModels.Order) (*OrderExecution, error)

	// GetCurrentPrice returns the current price for a book
	GetCurrentPrice(book string) (float64, error)

	// GetOrderBook returns the current order book (if available)
	GetOrderBook(book string) (*OrderBook, error)

	// GetState returns the current market state
	GetState() *MarketState

	// Reset resets the simulator to initial state
	Reset() error
}

// Simulator implements MarketSimulator
type Simulator struct {
	config        *SimulatorConfig
	currentPrices map[string]float64
	orderBooks    map[string]*OrderBook
	lastUpdate    time.Time
	slippageModel SlippageModel
	logger        logger.Logger
	mu            sync.RWMutex
}

// SimulatorConfig holds simulator configuration
type SimulatorConfig struct {
	SlippageModel   string  // "none", "fixed", "percentage"
	SlippageValue   float64 // Value depends on model
	CommissionRate  float64 // Legacy: single rate (used when MakerFee/TakerFee both 0)
	MakerFee        float64 // Maker fee decimal (Bitso btc_mxn: 0.005)
	TakerFee        float64 // Taker fee decimal (Bitso btc_mxn: 0.0065)
	EnableOrderBook bool    // Whether to simulate order book
}

// OrderExecution represents the result of executing an order
type OrderExecution struct {
	OrderID        string    `json:"order_id"`
	ExecutedPrice  float64   `json:"executed_price"`
	ExecutedAmount float64   `json:"executed_amount"`
	Commission     float64   `json:"commission"`
	Slippage       float64   `json:"slippage"`
	Timestamp      time.Time `json:"timestamp"`
	Success        bool      `json:"success"`
	Error          string    `json:"error,omitempty"`
}

// MarketState represents the current state of the market
type MarketState struct {
	Timestamp  time.Time             `json:"timestamp"`
	Prices     map[string]float64    `json:"prices"`
	OrderBooks map[string]*OrderBook `json:"order_books,omitempty"`
}

// NewSimulator creates a new market simulator
func NewSimulator(config *SimulatorConfig, log logger.Logger) *Simulator {
	return &Simulator{
		config:        config,
		currentPrices: make(map[string]float64),
		orderBooks:    make(map[string]*OrderBook),
		slippageModel: NewSlippageModel(config.SlippageModel, config.SlippageValue),
		logger:        log,
	}
}

// Initialize initializes the simulator
func (s *Simulator) Initialize(ctx context.Context, config *SimulatorConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config = config
	s.slippageModel = NewSlippageModel(config.SlippageModel, config.SlippageValue)

	if s.logger != nil {
		fields := map[string]interface{}{"slippage_model": config.SlippageModel}
		if config.MakerFee > 0 || config.TakerFee > 0 {
			fields["maker_fee"] = config.MakerFee
			fields["taker_fee"] = config.TakerFee
		} else {
			fields["commission_rate"] = config.CommissionRate
		}
		s.logger.Info("Simulator initialized", fields)
	}

	return nil
}

// ProcessEvent processes a market event and updates state
func (s *Simulator) ProcessEvent(event *models.MarketEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastUpdate = event.Timestamp

	// Update price based on event type
	price, err := event.GetPrice()
	if err != nil {
		return fmt.Errorf("failed to get price from event: %w", err)
	}

	s.currentPrices[event.Book] = price

	// Update order book if enabled
	if s.config.EnableOrderBook && event.IsOrderBookEvent() {
		// TODO: Update order book
	}

	return nil
}

// ExecuteOrder executes an order in the simulated market
func (s *Simulator) ExecuteOrder(order *sharedModels.Order) (*OrderExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get current price
	currentPrice, exists := s.currentPrices[order.Symbol]
	if !exists {
		return &OrderExecution{
			OrderID: order.ID,
			Success: false,
			Error:   fmt.Sprintf("no price available for book: %s", order.Symbol),
		}, fmt.Errorf("no price available for book: %s", order.Symbol)
	}

	// Calculate slippage
	slippage := s.slippageModel.Calculate(order, currentPrice)

	// Calculate execution price
	executionPrice := calculateExecutionPrice(currentPrice, order.Side, slippage)

	// Commission: use taker fee (market-style execution) when maker/taker set, else legacy single rate
	rate := s.config.TakerFee
	if s.config.MakerFee == 0 && s.config.TakerFee == 0 {
		rate = s.config.CommissionRate
	}
	commission := calculateCommission(order.Amount, executionPrice, rate)

	return &OrderExecution{
		OrderID:        order.ID,
		ExecutedPrice:  executionPrice,
		ExecutedAmount: order.Amount,
		Commission:     commission,
		Slippage:       slippage,
		Timestamp:      s.lastUpdate,
		Success:        true,
	}, nil
}

// GetCurrentPrice returns the current price for a book
func (s *Simulator) GetCurrentPrice(book string) (float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	price, exists := s.currentPrices[book]
	if !exists {
		return 0, fmt.Errorf("no price available for book: %s", book)
	}

	return price, nil
}

// GetOrderBook returns the current order book
func (s *Simulator) GetOrderBook(book string) (*OrderBook, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ob, exists := s.orderBooks[book]
	if !exists {
		return nil, fmt.Errorf("no order book available for book: %s", book)
	}

	return ob, nil
}

// GetState returns the current market state
func (s *Simulator) GetState() *MarketState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Clone prices
	prices := make(map[string]float64)
	for book, price := range s.currentPrices {
		prices[book] = price
	}

	// Clone order books
	orderBooks := make(map[string]*OrderBook)
	for book, ob := range s.orderBooks {
		orderBooks[book] = ob // TODO: Deep clone if needed
	}

	return &MarketState{
		Timestamp:  s.lastUpdate,
		Prices:     prices,
		OrderBooks: orderBooks,
	}
}

// Reset resets the simulator to initial state
func (s *Simulator) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.currentPrices = make(map[string]float64)
	s.orderBooks = make(map[string]*OrderBook)
	s.lastUpdate = time.Time{}

	if s.logger != nil {
		s.logger.Info("Simulator reset", nil)
	}

	return nil
}
