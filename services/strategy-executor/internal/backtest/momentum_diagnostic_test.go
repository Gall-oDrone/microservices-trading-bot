package backtest_test

import (
	"context"
	"os"
	"testing"
	"time"

	"bitso-trading-platform/strategy-executor/internal/backtest"
	"bitso-trading-platform/strategy-executor/internal/backtest/loader"
	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

// TestMomentumEntryConditionDiagnostic explains why momentum never enters.
//
// It replays an archive through the exact indicator pipeline RunHistorical
// uses and counts, per tick, how often each half of momentum's entry rule
// holds:
//
//	long : RSI < oversold   AND price > EMA
//	short: RSI > overbought AND price < EMA
//
// It is a diagnostic, not a regression test, so it only runs when pointed at
// a local archive sync:
//
//	MOMENTUM_DIAG_ARCHIVE=/path/to/archive go test ./internal/backtest \
//	  -run TestMomentumEntryConditionDiagnostic -v
//
// Optional: MOMENTUM_DIAG_FROM / MOMENTUM_DIAG_TO (RFC3339).
func TestMomentumEntryConditionDiagnostic(t *testing.T) {
	archive := os.Getenv("MOMENTUM_DIAG_ARCHIVE")
	if archive == "" {
		t.Skip("set MOMENTUM_DIAG_ARCHIVE to a local archive sync to run this diagnostic")
	}
	from := envTime(t, "MOMENTUM_DIAG_FROM", time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
	to := envTime(t, "MOMENTUM_DIAG_TO", time.Date(2026, 9, 22, 23, 59, 59, 0, time.UTC))

	ctx := context.Background()
	const book = "btc_mxn"
	src := loader.NewS3Archive(loader.NewLocalObjectStore(archive), loader.ArchiveConfig{Prefix: "trades", Concurrency: 64})
	trades, _, err := loader.LoadTradesOnly(ctx, src, book, from, to)
	if err != nil || len(trades) == 0 {
		t.Fatalf("load archive: %v (trades=%d)", err, len(trades))
	}

	mom := strategies.DefaultMomentumConfig()

	var (
		ticks, ready                 int
		rsiLow, aboveEMA, longBoth   int
		rsiHigh, belowEMA, shortBoth int
		minRSI, maxRSI               = 100.0, 0.0
		longGap, shortGap            = 1e18, 1e18 // closest miss, in bps of price
		longGatePass, shortGatePass  int
		roundTripBPS                 = 2 * (65.0 + 10.0) // backtest default legs incl. slippage
	)
	var cfg *indicators.ServiceConfig
	backtest.ReplayIndicators(ctx, trades, book, func(tr indicators.Trade, store indicators.IndicatorStore, c *indicators.ServiceConfig) {
		cfg = c
		ticks++

		rsiV, e1 := store.Get(ctx, book, "rsi", cfg.RSIPeriod)
		emaV, e2 := store.Get(ctx, book, "ema", cfg.EMAPeriod)
		if e1 != nil || e2 != nil || rsiV == nil || emaV == nil {
			return
		}
		ready++
		rsi, ema, p := rsiV.Value, emaV.Value, tr.Price
		if rsi < minRSI {
			minRSI = rsi
		}
		if rsi > maxRSI {
			maxRSI = rsi
		}
		atrBPS := 0.0
		if atrV, err := store.Get(ctx, book, "atr", cfg.ATRPeriod); err == nil && atrV != nil && p > 0 {
			atrBPS = atrV.Value / p * 1e4
		}

		if rsi < mom.OversoldLevel {
			rsiLow++
			if gap := (ema - p) / p * 1e4; gap < longGap {
				longGap = gap // how far price was BELOW ema when RSI was oversold
			}
		}
		if p > ema {
			aboveEMA++
		}
		if rsi < mom.OversoldLevel && p > ema {
			longBoth++
			if atrBPS*mom.ExpectedMoveATRMult >= roundTripBPS {
				longGatePass++
			}
		}

		if rsi > mom.OverboughtLevel {
			rsiHigh++
			if gap := (p - ema) / p * 1e4; gap < shortGap {
				shortGap = gap // how far price was ABOVE ema when RSI was overbought
			}
		}
		if p < ema {
			belowEMA++
		}
		if rsi > mom.OverboughtLevel && p < ema {
			shortBoth++
			if atrBPS*mom.ExpectedMoveATRMult >= roundTripBPS {
				shortGatePass++
			}
		}
	})

	pct := func(n int) float64 { return 100 * float64(n) / float64(max(ready, 1)) }
	t.Logf("window %s .. %s: %d ticks, %d with RSI+EMA ready; RSI range [%.2f, %.2f]",
		from.Format(time.RFC3339), to.Format(time.RFC3339), ticks, ready, minRSI, maxRSI)
	t.Logf("LONG : RSI<%.0f on %d ticks (%.2f%%); price>EMA on %d (%.2f%%); BOTH on %d (%.4f%%); of those, clear the %.0f bps gate: %d",
		mom.OversoldLevel, rsiLow, pct(rsiLow), aboveEMA, pct(aboveEMA), longBoth, pct(longBoth), roundTripBPS, longGatePass)
	t.Logf("       closest miss: when RSI was oversold, price was at least %.2f bps BELOW the EMA", longGap)
	t.Logf("SHORT: RSI>%.0f on %d ticks (%.2f%%); price<EMA on %d (%.2f%%); BOTH on %d (%.4f%%); of those, clear the %.0f bps gate: %d",
		mom.OverboughtLevel, rsiHigh, pct(rsiHigh), belowEMA, pct(belowEMA), shortBoth, pct(shortBoth), roundTripBPS, shortGatePass)
	t.Logf("       closest miss: when RSI was overbought, price was at least %.2f bps ABOVE the EMA", shortGap)
}

func envTime(t *testing.T, key string, def time.Time) time.Time {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	ts, err := time.Parse(time.RFC3339, v)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return ts
}
