package strategies

import (
	"bitso_trading_bot/pkg/bitso"
	"fmt"
	"time"
)

// ArbitrageStrategy implements an arbitrage trading strategy
type ArbitrageStrategy struct {
	*BaseStrategy
	lastTradeTime   time.Time
	tradeInterval   time.Duration
	profitThreshold float64
	spreadThreshold float64
	book            *bitso.Book
	lastBidPrice    float64
	lastAskPrice    float64
}

// NewArbitrageStrategy creates a new arbitrage strategy
func NewArbitrageStrategy(book *bitso.Book) *ArbitrageStrategy {
	return &ArbitrageStrategy{
		BaseStrategy:    NewBaseStrategy("arbitrage"),
		book:            book,
		tradeInterval:   1 * time.Minute, // Check frequently for arbitrage opportunities
		profitThreshold: 0.005,           // 0.5% minimum profit threshold
		spreadThreshold: 0.002,           // 0.2% minimum spread threshold
	}
}

// Execute runs the arbitrage strategy and sends signals through channels
func (s *ArbitrageStrategy) Execute(ticker *bitso.Ticker) error {
	// Check if strategy is stopping
	if s.IsStopping() {
		return nil
	}

	// Check if enough time has passed since last trade
	if time.Since(s.lastTradeTime) < s.tradeInterval {
		return nil
	}

	currentBid := ticker.Bid.Float64()
	currentAsk := ticker.Ask.Float64()

	// Calculate spread
	spread := (currentAsk - currentBid) / currentBid

	// Check if we have previous prices to compare
	if s.lastBidPrice > 0 && s.lastAskPrice > 0 {
		// Calculate price changes
		bidChange := (currentBid - s.lastBidPrice) / s.lastBidPrice
		askChange := (currentAsk - s.lastAskPrice) / s.lastAskPrice

		// Detect arbitrage opportunities
		if spread > s.spreadThreshold {
			// High spread detected - potential arbitrage opportunity
			if bidChange > s.profitThreshold {
				// Bid price increased significantly - send buy signal
				buySignal := TradingSignal{
					Type:      SignalBuy,
					Book:      s.book,
					Ticker:    ticker,
					Amount:    currentBid,
					Price:     currentBid,
					Reason:    fmt.Sprintf("Arbitrage opportunity: Bid increased %.2f%% with spread %.2f%%", bidChange*100, spread*100),
					Timestamp: time.Now().Unix(),
				}
				s.SendBuySignal(buySignal)
				fmt.Printf("- Arbitrage Strategy: Sending BUY signal - %s\n", buySignal.Reason)

			} else if askChange < -s.profitThreshold {
				// Ask price decreased significantly - send sell signal
				sellSignal := TradingSignal{
					Type:      SignalSell,
					Book:      s.book,
					Ticker:    ticker,
					Amount:    currentAsk,
					Price:     currentAsk,
					Reason:    fmt.Sprintf("Arbitrage opportunity: Ask decreased %.2f%% with spread %.2f%%", -askChange*100, spread*100),
					Timestamp: time.Now().Unix(),
				}
				s.SendSellSignal(sellSignal)
				fmt.Printf("- Arbitrage Strategy: Sending SELL signal - %s\n", sellSignal.Reason)

			} else {
				// High spread but no clear arbitrage opportunity - send hold signal
				holdSignal := TradingSignal{
					Type:      SignalHold,
					Book:      s.book,
					Ticker:    ticker,
					Amount:    0,
					Price:     currentBid,
					Reason:    fmt.Sprintf("High spread (%.2f%%) but no arbitrage opportunity", spread*100),
					Timestamp: time.Now().Unix(),
				}
				fmt.Printf("- Arbitrage Strategy: Monitoring - %s\n", holdSignal.Reason)
			}
		} else {
			// Low spread - no arbitrage opportunity
			holdSignal := TradingSignal{
				Type:      SignalHold,
				Book:      s.book,
				Ticker:    ticker,
				Amount:    0,
				Price:     currentBid,
				Reason:    fmt.Sprintf("Low spread (%.2f%%) - no arbitrage opportunity", spread*100),
				Timestamp: time.Now().Unix(),
			}
			fmt.Printf("- Arbitrage Strategy: Low spread - %s\n", holdSignal.Reason)
		}
	} else {
		// First execution - just collect initial prices
		holdSignal := TradingSignal{
			Type:      SignalHold,
			Book:      s.book,
			Ticker:    ticker,
			Amount:    0,
			Price:     currentBid,
			Reason:    "Initializing arbitrage strategy - collecting price data",
			Timestamp: time.Now().Unix(),
		}
		fmt.Printf("- Arbitrage Strategy: Initializing - %s\n", holdSignal.Reason)
	}

	// Update last prices
	s.lastBidPrice = currentBid
	s.lastAskPrice = currentAsk
	s.lastTradeTime = time.Now()

	return nil
}
