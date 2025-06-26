package strategies

import (
	"bitso_trading_bot/pkg/bitso"
	"fmt"
	"time"
)

// BasicStrategy implements a simple trading strategy
type BasicStrategy struct {
	*BaseStrategy
	lastTradeTime time.Time
	tradeInterval time.Duration
	profitTarget  float64
	stopLoss      float64
	book          *bitso.Book
}

// NewBasicStrategy creates a new basic strategy
func NewBasicStrategy(book *bitso.Book) *BasicStrategy {
	return &BasicStrategy{
		BaseStrategy:  NewBaseStrategy("basic"),
		book:          book,
		tradeInterval: 5 * time.Minute,
		profitTarget:  0.02, // 2% profit target
		stopLoss:      0.01, // 1% stop loss
	}
}

// Execute runs the strategy and sends signals through channels
func (s *BasicStrategy) Execute(ticker *bitso.Ticker) error {
	// Check if strategy is stopping
	if s.IsStopping() {
		return nil
	}

	// Check if enough time has passed since last trade
	if time.Since(s.lastTradeTime) < s.tradeInterval {
		return nil
	}

	// Calculate price change
	priceChange := (ticker.Ask.Float64() - ticker.Bid.Float64()) / ticker.Bid.Float64()

	// Trading logic with signal generation
	if priceChange > s.profitTarget {
		// Price increased significantly, send sell signal
		sellSignal := TradingSignal{
			Type:      SignalSell,
			Book:      s.book,
			Ticker:    ticker,
			Amount:    ticker.Ask.Float64(), // Use ask price for selling
			Price:     ticker.Ask.Float64(),
			Reason:    fmt.Sprintf("Price increased by %.2f%% (above %.2f%% target)", priceChange*100, s.profitTarget*100),
			Timestamp: time.Now().Unix(),
		}
		s.SendSellSignal(sellSignal)
		fmt.Printf("- Basic Strategy: Sending SELL signal - %s\n", sellSignal.Reason)

	} else if priceChange < -s.stopLoss {
		// Price decreased significantly, send buy signal
		buySignal := TradingSignal{
			Type:      SignalBuy,
			Book:      s.book,
			Ticker:    ticker,
			Amount:    ticker.Bid.Float64(), // Use bid price for buying
			Price:     ticker.Bid.Float64(),
			Reason:    fmt.Sprintf("Price decreased by %.2f%% (below %.2f%% stop loss)", -priceChange*100, s.stopLoss*100),
			Timestamp: time.Now().Unix(),
		}
		s.SendBuySignal(buySignal)
		fmt.Printf("- Basic Strategy: Sending BUY signal - %s\n", buySignal.Reason)
	} else {
		// Send hold signal for monitoring
		holdSignal := TradingSignal{
			Type:      SignalHold,
			Book:      s.book,
			Ticker:    ticker,
			Amount:    0,
			Price:     ticker.Bid.Float64(),
			Reason:    fmt.Sprintf("Price change %.2f%% within acceptable range", priceChange*100),
			Timestamp: time.Now().Unix(),
		}
		fmt.Printf("- Basic Strategy: Monitoring - %s\n", holdSignal.Reason)
	}

	s.lastTradeTime = time.Now()
	return nil
}
