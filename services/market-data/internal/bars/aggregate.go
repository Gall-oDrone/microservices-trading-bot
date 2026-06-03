// Package bars builds OHLCV candles from trade ticks for ATR and other bar-based indicators.
package bars

import (
	"fmt"
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/models"
)

// Bar is one OHLCV candle (strategy-executor HTTPDataProvider wire shape).
type Bar struct {
	Timestamp time.Time `json:"timestamp"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
}

// ParseInterval returns the bar duration for supported interval strings (e.g. "1m", "5m").
func ParseInterval(interval string) (time.Duration, error) {
	switch interval {
	case "1m":
		return time.Minute, nil
	case "5m":
		return 5 * time.Minute, nil
	case "15m":
		return 15 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported interval %q (use 1m, 5m, 15m, 1h)", interval)
	}
}

// BuildFromTrades aggregates trades into OHLCV bars aligned to UTC bucket starts.
// Trades with zero price are skipped. Returns at most limit most-recent complete buckets.
func BuildFromTrades(trades []*models.TradeEvent, interval time.Duration, limit int) []Bar {
	if len(trades) == 0 || limit <= 0 || interval <= 0 {
		return nil
	}

	sorted := make([]*models.TradeEvent, 0, len(trades))
	for _, t := range trades {
		if t == nil || t.Price <= 0 {
			continue
		}
		sorted = append(sorted, t)
	}
	if len(sorted) == 0 {
		return nil
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	type bucket struct {
		open, high, low, close, volume float64
		start                          time.Time
		set                            bool
	}

	buckets := make(map[int64]*bucket)
	for _, t := range sorted {
		start := t.Timestamp.Truncate(interval)
		key := start.Unix()
		b, ok := buckets[key]
		if !ok {
			b = &bucket{start: start, open: t.Price, high: t.Price, low: t.Price, close: t.Price, volume: t.Amount, set: true}
			buckets[key] = b
			continue
		}
		if t.Price > b.high {
			b.high = t.Price
		}
		if t.Price < b.low {
			b.low = t.Price
		}
		b.close = t.Price
		b.volume += t.Amount
	}

	keys := make([]int64, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	out := make([]Bar, 0, len(keys))
	for _, k := range keys {
		b := buckets[k]
		if !b.set {
			continue
		}
		out = append(out, Bar{
			Timestamp: b.start.UTC(),
			Open:      b.open,
			High:      b.high,
			Low:       b.low,
			Close:     b.close,
			Volume:    b.volume,
		})
	}

	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// WindowStart returns the earliest time to load trades for limit bars of interval ending at end.
func WindowStart(end time.Time, interval time.Duration, limit int) time.Time {
	return end.Add(-time.Duration(limit+2) * interval)
}
