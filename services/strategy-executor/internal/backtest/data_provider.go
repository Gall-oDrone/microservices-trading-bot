// Package backtest provides infrastructure for running strategies on historical data.
package backtest

import (
	"context"
	"sort"
	"sync"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// DataProvider defines the interface for providing market data to strategies.
// Both live and backtest implementations use this interface.
type DataProvider interface {
	// GetTrades returns trades for a book within a time range.
	GetTrades(ctx context.Context, book string, from, to time.Time) ([]indicators.Trade, error)
	// GetIndicator returns an indicator value for a book.
	GetIndicator(ctx context.Context, book, indicator string, period int) (*indicators.IndicatorValue, error)
	// GetBookTicker returns bid, ask, last for a book.
	GetBookTicker(ctx context.Context, book string) (bid, ask, last float64, ok bool)
}

// BacktestDataProvider replays historical trades for backtesting strategies.
type BacktestDataProvider struct {
	trades     []indicators.Trade
	indicators map[string]*indicators.IndicatorValue
	ticker     *BookTicker

	cursor int
	mu     sync.RWMutex
}

// BookTicker holds book ticker data for backtesting.
type BookTicker struct {
	Bid  float64
	Ask  float64
	Last float64
}

// NewBacktestDataProvider creates a new backtest data provider with historical trades.
func NewBacktestDataProvider(trades []indicators.Trade) *BacktestDataProvider {
	// Sort trades by timestamp.
	sorted := make([]indicators.Trade, len(trades))
	copy(sorted, trades)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	return &BacktestDataProvider{
		trades:     sorted,
		indicators: make(map[string]*indicators.IndicatorValue),
		cursor:     0,
	}
}

// SetIndicators sets precomputed indicator values for the backtest.
func (p *BacktestDataProvider) SetIndicators(indicators map[string]*indicators.IndicatorValue) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.indicators = indicators
}

// SetTicker sets the book ticker for exit price reference calculations.
func (p *BacktestDataProvider) SetTicker(ticker *BookTicker) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ticker = ticker
}

// GetTrades returns trades within the specified time range.
func (p *BacktestDataProvider) GetTrades(ctx context.Context, book string, from, to time.Time) ([]indicators.Trade, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []indicators.Trade
	for _, t := range p.trades {
		if !t.Timestamp.Before(from) && !t.Timestamp.After(to) {
			result = append(result, t)
		}
	}
	return result, nil
}

// GetIndicator returns a precomputed indicator value.
func (p *BacktestDataProvider) GetIndicator(ctx context.Context, book, indicator string, period int) (*indicators.IndicatorValue, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	key := indicator
	if period > 0 {
		key = indicator + "_" + string(rune(period))
	}
	if v, ok := p.indicators[key]; ok {
		return v, nil
	}
	return nil, nil
}

// GetBookTicker returns the current ticker values.
func (p *BacktestDataProvider) GetBookTicker(ctx context.Context, book string) (bid, ask, last float64, ok bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.ticker == nil {
		return 0, 0, 0, false
	}
	return p.ticker.Bid, p.ticker.Ask, p.ticker.Last, true
}

// NextTrade advances the cursor and returns the next trade, or nil if exhausted.
func (p *BacktestDataProvider) NextTrade() *indicators.Trade {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cursor >= len(p.trades) {
		return nil
	}
	trade := &p.trades[p.cursor]
	p.cursor++

	// Update ticker based on trade.
	if p.ticker == nil {
		p.ticker = &BookTicker{}
	}
	p.ticker.Last = trade.Price
	// Simulate bid/ask around last price (simple spread model).
	spread := trade.Price * 0.001 // 0.1% spread
	p.ticker.Bid = trade.Price - spread/2
	p.ticker.Ask = trade.Price + spread/2

	return trade
}

// Reset resets the cursor to the beginning.
func (p *BacktestDataProvider) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cursor = 0
	p.ticker = nil
}

// TradeCount returns the total number of trades.
func (p *BacktestDataProvider) TradeCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.trades)
}

// CurrentPosition returns the current cursor position.
func (p *BacktestDataProvider) CurrentPosition() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cursor
}

// IsExhausted returns true if all trades have been consumed.
func (p *BacktestDataProvider) IsExhausted() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cursor >= len(p.trades)
}
