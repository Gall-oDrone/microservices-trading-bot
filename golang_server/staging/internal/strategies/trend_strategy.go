package strategies

import (
	"bitso_trading_bot/pkg/bitso"
	"fmt"
	"time"
)

// TrendStrategy implements a trend following trading strategy
type TrendStrategy struct {
	*BaseStrategy
	lastTradeTime     time.Time
	tradeInterval     time.Duration
	trendPeriod       time.Duration
	momentumThreshold float64
	book              *bitso.Book
	priceHistory      []float64
}

// NewTrendStrategy creates a new trend strategy
func NewTrendStrategy(book *bitso.Book) *TrendStrategy {
	return &TrendStrategy{
		BaseStrategy:      NewBaseStrategy("trend_following"),
		book:              book,
		tradeInterval:     10 * time.Minute,
		trendPeriod:       30 * time.Minute,
		momentumThreshold: 0.015, // 1.5% momentum threshold
		priceHistory:      make([]float64, 0),
	}
}

// Execute runs the trend following strategy and sends signals through channels
func (s *TrendStrategy) Execute(ticker *bitso.Ticker) error {
	// Check if strategy is stopping
	if s.IsStopping() {
		return nil
	}

	// Check if enough time has passed since last trade
	if time.Since(s.lastTradeTime) < s.tradeInterval {
		return nil
	}

	// Add current price to history
	currentPrice := ticker.Bid.Float64()
	s.priceHistory = append(s.priceHistory, currentPrice)

	// Keep only recent price history (last 30 minutes worth)
	maxHistorySize := 30 // Assuming 1-minute intervals
	if len(s.priceHistory) > maxHistorySize {
		s.priceHistory = s.priceHistory[len(s.priceHistory)-maxHistorySize:]
	}

	// Calculate trend if we have enough data
	if len(s.priceHistory) >= 5 {
		trend := s.calculateTrend()
		momentum := s.calculateMomentum()

		// Generate signals based on trend and momentum
		if trend > 0 && momentum > s.momentumThreshold {
			// Bullish trend with strong momentum - send buy signal
			buySignal := TradingSignal{
				Type:      SignalBuy,
				Book:      s.book,
				Ticker:    ticker,
				Amount:    ticker.Bid.Float64(),
				Price:     ticker.Bid.Float64(),
				Reason:    fmt.Sprintf("Bullish trend (%.2f%%) with strong momentum (%.2f%%)", trend*100, momentum*100),
				Timestamp: time.Now().Unix(),
			}
			s.SendBuySignal(buySignal)
			fmt.Printf("- Trend Strategy: Sending BUY signal - %s\n", buySignal.Reason)

		} else if trend < 0 && momentum > s.momentumThreshold {
			// Bearish trend with strong momentum - send sell signal
			sellSignal := TradingSignal{
				Type:      SignalSell,
				Book:      s.book,
				Ticker:    ticker,
				Amount:    ticker.Ask.Float64(),
				Price:     ticker.Ask.Float64(),
				Reason:    fmt.Sprintf("Bearish trend (%.2f%%) with strong momentum (%.2f%%)", trend*100, momentum*100),
				Timestamp: time.Now().Unix(),
			}
			s.SendSellSignal(sellSignal)
			fmt.Printf("- Trend Strategy: Sending SELL signal - %s\n", sellSignal.Reason)

		} else {
			// No clear trend or weak momentum - send hold signal
			holdSignal := TradingSignal{
				Type:      SignalHold,
				Book:      s.book,
				Ticker:    ticker,
				Amount:    0,
				Price:     ticker.Bid.Float64(),
				Reason:    fmt.Sprintf("No clear trend (%.2f%%) or weak momentum (%.2f%%)", trend*100, momentum*100),
				Timestamp: time.Now().Unix(),
			}
			fmt.Printf("- Trend Strategy: Monitoring - %s\n", holdSignal.Reason)
		}
	} else {
		// Not enough data yet - send hold signal
		holdSignal := TradingSignal{
			Type:      SignalHold,
			Book:      s.book,
			Ticker:    ticker,
			Amount:    0,
			Price:     ticker.Bid.Float64(),
			Reason:    fmt.Sprintf("Insufficient data for trend analysis (%d/5 points)", len(s.priceHistory)),
			Timestamp: time.Now().Unix(),
		}
		fmt.Printf("- Trend Strategy: Collecting data - %s\n", holdSignal.Reason)
	}

	s.lastTradeTime = time.Now()
	return nil
}

// calculateTrend calculates the overall trend direction
func (s *TrendStrategy) calculateTrend() float64 {
	if len(s.priceHistory) < 2 {
		return 0
	}

	// Simple linear trend calculation
	firstPrice := s.priceHistory[0]
	lastPrice := s.priceHistory[len(s.priceHistory)-1]
	return (lastPrice - firstPrice) / firstPrice
}

// calculateMomentum calculates the recent price momentum
func (s *TrendStrategy) calculateMomentum() float64 {
	if len(s.priceHistory) < 3 {
		return 0
	}

	// Calculate momentum based on recent price changes
	recentPrices := s.priceHistory[len(s.priceHistory)-3:]
	momentum := 0.0

	for i := 1; i < len(recentPrices); i++ {
		change := (recentPrices[i] - recentPrices[i-1]) / recentPrices[i-1]
		momentum += change
	}

	return momentum / float64(len(recentPrices)-1)
}
