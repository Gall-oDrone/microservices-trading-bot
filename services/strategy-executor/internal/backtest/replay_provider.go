package backtest

import (
	"context"
	"sync"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// ReplayProvider implements indicators.DataProvider over a fixed historical trade
// slice, exposing only the trades observed so far (a growing window). It is the
// data source for the indicator service during a historical backtest so that, at
// each replayed tick, indicators are computed from past data only — never future
// data (no look-ahead bias).
//
// Bars are aggregated incrementally into 1-minute OHLCV candles to keep Observe
// O(1) and avoid O(n^2) recomputation across the replay.
type ReplayProvider struct {
	mu      sync.RWMutex
	visible []indicators.Trade
	bars    []indicators.OHLCV
}

// NewReplayProvider creates an empty replay provider. Call Observe to grow the
// visible window one trade at a time as the backtest progresses.
func NewReplayProvider() *ReplayProvider {
	return &ReplayProvider{}
}

// Observe appends a trade to the visible window and updates the 1m bar series.
func (p *ReplayProvider) Observe(t indicators.Trade) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.visible = append(p.visible, t)

	minute := t.Timestamp.Truncate(time.Minute)
	if n := len(p.bars); n > 0 && p.bars[n-1].Timestamp.Equal(minute) {
		b := &p.bars[n-1]
		if t.Price > b.High {
			b.High = t.Price
		}
		if t.Price < b.Low {
			b.Low = t.Price
		}
		b.Close = t.Price
		b.Volume += t.Amount
		return
	}

	p.bars = append(p.bars, indicators.OHLCV{
		Timestamp: minute,
		Open:      t.Price,
		High:      t.Price,
		Low:       t.Price,
		Close:     t.Price,
		Volume:    t.Amount,
	})
}

// GetRecentTrades returns up to limit most-recent observed trades.
func (p *ReplayProvider) GetRecentTrades(ctx context.Context, book string, limit int) ([]indicators.Trade, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if limit <= 0 || limit > len(p.visible) {
		limit = len(p.visible)
	}
	start := len(p.visible) - limit
	out := make([]indicators.Trade, limit)
	copy(out, p.visible[start:])
	return out, nil
}

// GetRecentBars returns up to limit most-recent observed 1m bars. The interval
// argument is accepted for interface compatibility; only 1m aggregation is built.
func (p *ReplayProvider) GetRecentBars(ctx context.Context, book, interval string, limit int) ([]indicators.OHLCV, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if limit <= 0 || limit > len(p.bars) {
		limit = len(p.bars)
	}
	start := len(p.bars) - limit
	out := make([]indicators.OHLCV, limit)
	copy(out, p.bars[start:])
	return out, nil
}

// GetBookTicker returns the last observed trade price as last, with a synthetic
// symmetric spread for strategies that compare against bid/ask.
func (p *ReplayProvider) GetBookTicker(ctx context.Context, book string) (bid, ask, last float64, ok bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.visible) == 0 {
		return 0, 0, 0, false
	}
	last = p.visible[len(p.visible)-1].Price
	const halfSpread = 0.0005 // 0.05% each side
	return last * (1 - halfSpread), last * (1 + halfSpread), last, true
}
