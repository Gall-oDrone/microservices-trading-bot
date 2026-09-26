package backtest

import (
	"context"
	"math"
	"math/rand"
	"testing"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// burstyTrades builds a random-walk trade stream with bursts of several
// trades per minute, idle minutes, and exact price repeats (zero changes, which
// exercise RSI's gain/loss tie branch).
func burstyTrades(n int, seed int64) []indicators.Trade {
	r := rand.New(rand.NewSource(seed))
	out := make([]indicators.Trade, 0, n)
	ts := time.Date(2026, 8, 19, 15, 40, 0, 0, time.UTC)
	price := 1_200_000.0
	for len(out) < n {
		switch r.Intn(10) {
		case 0:
			ts = ts.Add(time.Duration(1+r.Intn(5)) * time.Minute) // idle gap
		default:
			ts = ts.Add(time.Duration(r.Intn(40)) * time.Second)
		}
		if r.Intn(4) != 0 { // 25% of trades repeat the last price exactly
			price *= 1 + r.NormFloat64()*0.0008
		}
		out = append(out, indicators.Trade{Timestamp: ts, Price: math.Round(price*100) / 100, Amount: 0.001 + r.Float64()*0.05})
	}
	return out
}

// TestIncrementalIndicatorsMatchFullRecompute is the guard that makes the
// O(n) rewrite safe: at EVERY tick, every indicator written by the incremental
// computer must be bit-for-bit identical to the full-history recomputation it
// replaced. Any drift, even in the last ulp, fails the test.
func TestIncrementalIndicatorsMatchFullRecompute(t *testing.T) {
	ctx := context.Background()
	const book = "btc_mxn"
	cfg := indicators.DefaultServiceConfig()

	for _, seed := range []int64{1, 2, 3} {
		trades := burstyTrades(3000, seed)

		refReplay, incReplay := NewReplayProvider(), NewReplayProvider()
		refStore, incStore := indicators.NewInMemoryIndicatorStore(), indicators.NewInMemoryIndicatorStore()
		ref, inc := newIndicatorComputer(cfg), newIncrementalIndicators(cfg)

		type key struct {
			name   string
			period int
		}
		keys := []key{
			{"sma", cfg.SMAPeriod}, {"ema", cfg.EMAPeriod}, {"rsi", cfg.RSIPeriod},
			{"atr", cfg.ATRPeriod}, {"vwap", cfg.VWAPPeriod}, {"bollinger_middle", cfg.BollingerPeriod},
		}
		populated := map[string]bool{}

		for i, tr := range trades {
			refReplay.Observe(tr)
			incReplay.Observe(tr)
			ref.computeInto(ctx, refStore, refReplay, book)
			inc.computeInto(ctx, incStore, incReplay, book)

			for _, k := range keys {
				rv, rerr := refStore.Get(ctx, book, k.name, k.period)
				iv, ierr := incStore.Get(ctx, book, k.name, k.period)
				if (rerr == nil && rv != nil) != (ierr == nil && iv != nil) {
					t.Fatalf("seed %d tick %d %s: presence differs (ref=%v inc=%v)", seed, i, k.name, rv, iv)
				}
				if rv == nil || rerr != nil {
					continue
				}
				populated[k.name] = true
				if math.Float64bits(rv.Value) != math.Float64bits(iv.Value) {
					t.Fatalf("seed %d tick %d %s: ref=%.17g inc=%.17g", seed, i, k.name, rv.Value, iv.Value)
				}
				for ek, ev := range rv.Extra {
					if math.Float64bits(ev) != math.Float64bits(iv.Extra[ek]) {
						t.Fatalf("seed %d tick %d %s.%s: ref=%.17g inc=%.17g", seed, i, k.name, ek, ev, iv.Extra[ek])
					}
				}
			}
			rb, _ := refStore.GetBollinger(ctx, book, cfg.BollingerPeriod)
			ib, _ := incStore.GetBollinger(ctx, book, cfg.BollingerPeriod)
			if (rb == nil) != (ib == nil) || (rb != nil && *rb != *ib) {
				t.Fatalf("seed %d tick %d bollinger: ref=%+v inc=%+v", seed, i, rb, ib)
			}
		}

		for _, k := range keys {
			if !populated[k.name] {
				t.Fatalf("seed %d: %s was never computed; the test would be vacuous", seed, k.name)
			}
		}
	}
}
