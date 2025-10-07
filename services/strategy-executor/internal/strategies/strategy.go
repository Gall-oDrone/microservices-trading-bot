package strategies

import (
	"bitso-trading-platform/shared/pkg/bitso"
)

// SignalType represents the type of trading signal
type SignalType int

const (
	SignalNone SignalType = iota
	SignalBuy
	SignalSell
	SignalHold
)

// TradingSignal represents a trading signal with metadata
type TradingSignal struct {
	Type      SignalType
	Book      *bitso.Book
	Ticker    *bitso.Ticker
	Amount    float64
	Price     float64
	Reason    string
	Timestamp int64
}

// Strategy is the main interface that all trading strategies must implement
type Strategy interface {
	// GetName returns the strategy name
	GetName() string

	// Execute runs the strategy analysis and sends signals through channels
	Execute(ticker *bitso.Ticker) error

	// GetBuySignalChannel returns the channel for buy signals
	GetBuySignalChannel() chan TradingSignal

	// GetSellSignalChannel returns the channel for sell signals
	GetSellSignalChannel() chan TradingSignal

	// Stop gracefully stops the strategy
	Stop() error
}

// BaseStrategy provides common functionality for all strategies
type BaseStrategy struct {
	name           string
	buySignalChan  chan TradingSignal
	sellSignalChan chan TradingSignal
	stopChan       chan struct{}
}

// NewBaseStrategy creates a new base strategy with channels
func NewBaseStrategy(name string) *BaseStrategy {
	return &BaseStrategy{
		name:           name,
		buySignalChan:  make(chan TradingSignal, 10), // Buffered channel for signals
		sellSignalChan: make(chan TradingSignal, 10), // Buffered channel for signals
		stopChan:       make(chan struct{}),
	}
}

// GetName returns the strategy name
func (bs *BaseStrategy) GetName() string {
	return bs.name
}

// GetBuySignalChannel returns the channel for buy signals
func (bs *BaseStrategy) GetBuySignalChannel() chan TradingSignal {
	return bs.buySignalChan
}

// GetSellSignalChannel returns the channel for sell signals
func (bs *BaseStrategy) GetSellSignalChannel() chan TradingSignal {
	return bs.sellSignalChan
}

// Stop gracefully stops the strategy
func (bs *BaseStrategy) Stop() error {
	close(bs.stopChan)
	close(bs.buySignalChan)
	close(bs.sellSignalChan)
	return nil
}

// SendBuySignal sends a buy signal through the channel
func (bs *BaseStrategy) SendBuySignal(signal TradingSignal) {
	select {
	case bs.buySignalChan <- signal:
		// Signal sent successfully
	case <-bs.stopChan:
		// Strategy is stopping, don't send signal
	default:
		// Channel is full, skip signal
	}
}

// SendSellSignal sends a sell signal through the channel
func (bs *BaseStrategy) SendSellSignal(signal TradingSignal) {
	select {
	case bs.sellSignalChan <- signal:
		// Signal sent successfully
	case <-bs.stopChan:
		// Strategy is stopping, don't send signal
	default:
		// Channel is full, skip signal
	}
}

// IsStopping checks if the strategy is in the process of stopping
func (bs *BaseStrategy) IsStopping() bool {
	select {
	case <-bs.stopChan:
		return true
	default:
		return false
	}
}
