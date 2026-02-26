package strategies

import (
	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
	"fmt"
	"time"
)

// BasicStrategy implements a simple trading strategy.
// Parameters are config-driven for intraday tuning (see INTRADAY-STRATEGIES.md).
type BasicStrategy struct {
	*BaseStrategy
	lastTradeTime time.Time
	tradeInterval time.Duration
	profitTarget  float64
	stopLoss      float64
	book          *bitso.Book
}

// NewBasicStrategy creates a new basic strategy from a trading config.
// Reads from config.Parameters: trade_interval_minutes, profit_target_pct, stop_loss_pct.
// Falls back to config.StopLossPercent/TakeProfitPercent (as decimals) if params not set.
func NewBasicStrategy(config *models.TradingConfig) *BasicStrategy {
	if config == nil || config.Book == nil {
		return &BasicStrategy{
			BaseStrategy:  NewBaseStrategy("basic"),
			book:          bitso.NewBook(bitso.BTC, bitso.MXN),
			tradeInterval: 5 * time.Minute,
			profitTarget:  0.02,
			stopLoss:      0.01,
		}
	}
	p := NewParamReader(config.Parameters)
	tradeInterval := p.DurationMinutes("trade_interval_minutes", 5*time.Minute)
	profitTarget := p.Float64("profit_target_pct", 0.02)
	stopLoss := p.Float64("stop_loss_pct", 0.01)
	if profitTarget <= 0 && config.TakeProfitPercent > 0 {
		profitTarget = config.TakeProfitPercent / 100
	}
	if stopLoss <= 0 && config.StopLossPercent > 0 {
		stopLoss = config.StopLossPercent / 100
	}
	return &BasicStrategy{
		BaseStrategy:  NewBaseStrategy("basic"),
		book:          config.Book,
		tradeInterval: tradeInterval,
		profitTarget:  profitTarget,
		stopLoss:      stopLoss,
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
