package loader

import (
	"sort"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// AggregateBars buckets trades into OHLCV candles of the given interval.
//
// The bucketing semantics deliberately match ReplayProvider.Observe in the
// parent backtest package (Timestamp.Truncate(interval), open = first trade in
// the bucket, close = last, volume = summed base amount). ReplayProvider does
// the same job incrementally and is hardcoded to one minute; this is the
// batch-mode equivalent for offline analysis, where we need a configurable
// interval and the whole series at once.
//
// Empty buckets are NOT emitted. This matters: btc_mxn is thin enough that
// quiet periods produce minutes with no trades at all, and synthesizing
// flat-price filler bars there would understate ATR and manufacture a
// low-volatility reading that the live system never sees. Callers that need a
// gap-aware view should inspect the bar timestamps.
func AggregateBars(trades []indicators.Trade, interval time.Duration) []indicators.OHLCV {
	if len(trades) == 0 || interval <= 0 {
		return nil
	}

	sorted := make([]indicators.Trade, len(trades))
	copy(sorted, trades)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	var bars []indicators.OHLCV
	for _, t := range sorted {
		if t.Price <= 0 {
			continue
		}
		bucket := t.Timestamp.UTC().Truncate(interval)

		if n := len(bars); n > 0 && bars[n-1].Timestamp.Equal(bucket) {
			b := &bars[n-1]
			if t.Price > b.High {
				b.High = t.Price
			}
			if t.Price < b.Low {
				b.Low = t.Price
			}
			b.Close = t.Price
			b.Volume += t.Amount
			continue
		}

		bars = append(bars, indicators.OHLCV{
			Timestamp: bucket,
			Open:      t.Price,
			High:      t.Price,
			Low:       t.Price,
			Close:     t.Price,
			Volume:    t.Amount,
		})
	}
	return bars
}

// BarsUpTo returns the bars at or before cutoff.
//
// This is the guard against look-ahead bias in offline analysis: when taking a
// snapshot at time T, only bars that had already closed by T may feed the
// indicators. Bars are assumed sorted ascending, as AggregateBars returns them.
func BarsUpTo(bars []indicators.OHLCV, cutoff time.Time) []indicators.OHLCV {
	i := sort.Search(len(bars), func(i int) bool {
		return bars[i].Timestamp.After(cutoff)
	})
	return bars[:i]
}
