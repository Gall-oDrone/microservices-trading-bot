package backtest

import (
	"context"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// incrementalIndicators computes the same indicator values as a full
// recomputation over every replayed bar, in O(1) amortised work per tick.
//
// The previous implementation re-fetched every bar since the start of the run
// and recomputed each indicator from scratch on every trade, which is O(n^2)
// over a replay: ~6 minutes for 34 days, hours for a year.
//
// Exactness matters more than speed here: a faster harness that shifts results
// even slightly would invalidate every comparison against earlier runs. So:
//
//   - SMA and Bollinger only read the last `period` closes, so they are handed
//     exactly that tail and run the unchanged library code.
//   - EMA, RSI (Wilder) and ATR (Wilder) are recursive from the FIRST bar, so
//     their value depends on the whole path and cannot simply be windowed.
//     Their state is folded forward once per CLOSED bar, using the same
//     floating-point expressions in the same order as the library functions.
//     The still-forming last bar (whose close/high/low change with every trade)
//     is applied to a copy of that state on each tick.
//
// Bars before the last are immutable once a newer bar exists (ReplayProvider
// only ever mutates the last bar), which is what makes folding them safe.
// TestIncrementalIndicatorsMatchFullRecompute asserts bit-for-bit equality
// with the full recomputation at every tick.
type incrementalIndicators struct {
	cfg  *indicators.ServiceConfig
	sma  *indicators.SMA
	bb   *indicators.Bollinger
	atr  *indicators.ATR
	vwap *indicators.VWAP

	emaAlpha float64

	closed     int       // number of closed bars folded into the state below
	closes     []float64 // closes of closed bars (tail used for SMA/Bollinger)
	prevClose  float64   // close of the last closed bar
	emaSeedSum float64   // running sum of the first EMAPeriod closes
	ema        float64   // EMA after the last closed bar (valid once closed >= EMAPeriod)
	rsiSumGain float64
	rsiSumLoss float64
	rsiAvgGain float64
	rsiAvgLoss float64
	rsiChanges int // number of close-to-close changes folded
	atrSum     float64
	atrValue   float64
	atrTRs     int // number of true ranges folded
}

func newIncrementalIndicators(cfg *indicators.ServiceConfig) *incrementalIndicators {
	return &incrementalIndicators{
		cfg:  cfg,
		sma:  indicators.NewSMA(cfg.SMAPeriod),
		bb:   indicators.NewBollinger(cfg.BollingerPeriod, cfg.BollingerStdDev),
		atr:  indicators.NewATR(cfg.ATRPeriod),
		vwap: indicators.NewVWAP(cfg.VWAPPeriod),
		// Same expression as indicators.NewEMA, so alpha is bit-identical.
		emaAlpha: 2.0 / float64(cfg.EMAPeriod+1),
	}
}

// ReplayIndicators feeds trades one at a time through the same no-look-ahead
// indicator pipeline RunHistorical uses and calls fn after each trade with the
// store holding the indicator values as of that trade. It exists for offline
// diagnostics that need to inspect indicator behaviour without running a
// strategy.
func ReplayIndicators(ctx context.Context, trades []indicators.Trade, book string, fn func(t indicators.Trade, store indicators.IndicatorStore, cfg *indicators.ServiceConfig)) {
	cfg := indicators.DefaultServiceConfig()
	replay := NewReplayProvider()
	store := indicators.NewInMemoryIndicatorStore()
	comp := newIncrementalIndicators(cfg)
	for _, t := range trades {
		replay.Observe(t)
		comp.computeInto(ctx, store, replay, book)
		fn(t, store, cfg)
	}
}

// foldClosedBar advances the recursive state by one immutable bar.
func (c *incrementalIndicators) foldClosedBar(b indicators.OHLCV) {
	i := c.closed // index of this bar in the full series
	p := b.Close

	// EMA: SMA seed over the first period closes, then the recursion.
	ep := c.cfg.EMAPeriod
	switch {
	case i < ep:
		c.emaSeedSum += p
		if i == ep-1 {
			c.ema = c.emaSeedSum / float64(ep)
		}
	default:
		c.ema = c.emaAlpha*p + (1-c.emaAlpha)*c.ema
	}

	if i > 0 {
		c.rsiSumGain, c.rsiSumLoss, c.rsiAvgGain, c.rsiAvgLoss, c.rsiChanges =
			rsiStep(c.cfg.RSIPeriod, c.rsiSumGain, c.rsiSumLoss, c.rsiAvgGain, c.rsiAvgLoss, c.rsiChanges, p-c.prevClose)

		tr := c.atr.ComputeTrueRange(b.High, b.Low, c.prevClose)
		c.atrSum, c.atrValue, c.atrTRs = wilderStep(c.cfg.ATRPeriod, c.atrSum, c.atrValue, c.atrTRs, tr)
	}

	c.prevClose = p
	c.closes = append(c.closes, p)
	c.closed++
}

// rsiStep folds one close-to-close change, mirroring RSI.computeAvgGainLoss.
func rsiStep(period int, sumGain, sumLoss, avgGain, avgLoss float64, n int, change float64) (float64, float64, float64, float64, int) {
	var gain, loss float64
	if change > 0 {
		gain, loss = change, 0
	} else {
		gain, loss = 0, -change
	}
	switch {
	case n < period:
		sumGain += gain
		sumLoss += loss
		if n == period-1 {
			avgGain = sumGain / float64(period)
			avgLoss = sumLoss / float64(period)
		}
	default:
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
	}
	return sumGain, sumLoss, avgGain, avgLoss, n + 1
}

// wilderStep folds one true range, mirroring ATR.ComputeFromBars.
func wilderStep(period int, sum, value float64, n int, x float64) (float64, float64, int) {
	switch {
	case n < period:
		sum += x
		if n == period-1 {
			value = sum / float64(period)
		}
	default:
		value = (value*float64(period-1) + x) / float64(period)
	}
	return sum, value, n + 1
}

func rsiFromAverages(avgGain, avgLoss float64) float64 {
	if avgLoss == 0 {
		if avgGain == 0 {
			return 50
		}
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

// computeInto is a drop-in replacement for indicatorComputer.computeInto.
func (c *incrementalIndicators) computeInto(ctx context.Context, store indicators.IndicatorStore, replay *ReplayProvider, book string) {
	tail, total := replay.barsFrom(c.closed)
	if total == 0 || len(tail) == 0 {
		return
	}
	// Every bar except the last is closed; fold any newly closed ones.
	for len(tail) > 1 {
		c.foldClosedBar(tail[0])
		tail = tail[1:]
	}
	cur := tail[0] // the forming bar, index total-1
	now := time.Now()
	cfg := c.cfg

	// SMA / Bollinger: exact library code over the exact tail it would read.
	window := c.closeWindow(cur.Close)
	if v, err := c.sma.Compute(window); err == nil {
		_ = store.Set(ctx, book, "sma", cfg.SMAPeriod, &indicators.IndicatorValue{Name: "sma", Period: cfg.SMAPeriod, Value: v, Timestamp: now, Book: book})
	}

	// EMA over total closes: needs total >= period.
	if ep := cfg.EMAPeriod; total >= ep {
		var v float64
		if c.closed >= ep {
			v = c.emaAlpha*cur.Close + (1-c.emaAlpha)*c.ema
		} else { // forming bar completes the seed (total == ep)
			v = (c.emaSeedSum + cur.Close) / float64(ep)
		}
		_ = store.Set(ctx, book, "ema", ep, &indicators.IndicatorValue{Name: "ema", Period: ep, Value: v, Timestamp: now, Book: book})
	}

	// RSI over total-1 changes: needs total >= period+1.
	if rp := cfg.RSIPeriod; total >= rp+1 {
		_, _, ag, al, _ := rsiStep(rp, c.rsiSumGain, c.rsiSumLoss, c.rsiAvgGain, c.rsiAvgLoss, c.rsiChanges, cur.Close-c.prevClose)
		v := rsiFromAverages(ag, al)
		_ = store.Set(ctx, book, "rsi", rp, &indicators.IndicatorValue{Name: "rsi", Period: rp, Value: v, Timestamp: now, Book: book})
	}

	if bb, err := c.bb.ComputeBands(window); err == nil {
		_ = store.SetBollinger(ctx, book, cfg.BollingerPeriod, bb)
		_ = store.Set(ctx, book, "bollinger_middle", cfg.BollingerPeriod, &indicators.IndicatorValue{
			Name: "bollinger_middle", Period: cfg.BollingerPeriod, Value: bb.Middle, Timestamp: now, Book: book,
			Extra: map[string]float64{"upper": bb.Upper, "lower": bb.Lower, "stddev": bb.StdDev},
		})
	}

	// ATR over total-1 true ranges: needs total >= period+1.
	if ap := cfg.ATRPeriod; total >= ap+1 {
		tr := c.atr.ComputeTrueRange(cur.High, cur.Low, c.prevClose)
		_, v, _ := wilderStep(ap, c.atrSum, c.atrValue, c.atrTRs, tr)
		_ = store.Set(ctx, book, "atr", ap, &indicators.IndicatorValue{Name: "atr", Period: ap, Value: v, Timestamp: now, Book: book})
	}

	if trades, err := replay.GetRecentTrades(ctx, book, 100); err == nil && len(trades) > 0 {
		if v, err := c.vwap.ComputeFromTrades(trades); err == nil {
			_ = store.Set(ctx, book, "vwap", cfg.VWAPPeriod, &indicators.IndicatorValue{Name: "vwap", Period: cfg.VWAPPeriod, Value: v, Timestamp: now, Book: book})
		}
	}
}

// closeWindow returns the last max(SMA, Bollinger) period closes including the
// forming bar -- exactly the values SMA/Bollinger read from the full series.
func (c *incrementalIndicators) closeWindow(formingClose float64) []float64 {
	n := c.cfg.SMAPeriod
	if c.cfg.BollingerPeriod > n {
		n = c.cfg.BollingerPeriod
	}
	take := n - 1
	if take > len(c.closes) {
		take = len(c.closes)
	}
	w := make([]float64, 0, take+1)
	w = append(w, c.closes[len(c.closes)-take:]...)
	return append(w, formingClose)
}
