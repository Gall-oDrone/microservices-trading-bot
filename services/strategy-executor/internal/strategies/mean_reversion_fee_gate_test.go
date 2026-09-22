package strategies

import (
	"testing"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// newGatedMeanReversion builds a strategy wired with a fee provider, ready to
// exercise the gates directly.
func newGatedMeanReversion(t *testing.T, provider MakerTakerFeeProvider, params map[string]interface{}) *MeanReversionStrategy {
	t.Helper()

	s := NewMeanReversionStrategy()
	cfg := StrategyConfig{
		Name:       "test_mean_reversion",
		Type:       "mean_reversion",
		Version:    "1.0.0",
		Enabled:    true,
		Book:       "btc_mxn",
		Parameters: params,
	}
	store := indicators.NewInMemoryIndicatorStore()
	dp := indicators.NewMockDataProvider()
	if err := s.Initialize(cfg, indicators.NewService(nil, store, dp, nil)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if provider != nil {
		s.SetFeeRatesProvider(provider)
	}
	return s
}

// TestMeanReversion_EntryFeeGate covers both directions around the cost
// threshold: a move too small to pay for itself must be suppressed, and a move
// that clearly clears cost must still fire.
func TestMeanReversion_EntryFeeGate(t *testing.T) {
	feeProvider := &fakeFeeProvider{maker: testMakerRate, taker: testTakerRate, ok: true}

	tests := []struct {
		name       string
		bb         *indicators.BollingerBands
		price      float64
		wantSignal bool
		wantSide   string
	}{
		{
			// Price is below the lower band, so the legacy logic would fire.
			// The reversion target is only ~0.2% away while the round trip
			// costs ~1.15%, so the trade cannot pay for itself.
			name:       "long entry suppressed when reversion target is inside the fee cost",
			bb:         &indicators.BollingerBands{Upper: 1_004_000, Middle: 1_002_000, Lower: 1_000_500},
			price:      1_000_000,
			wantSignal: false,
		},
		{
			// Same setup but the middle band is ~3% away — comfortably above cost.
			name:       "long entry fires when reversion target clears the fee cost",
			bb:         &indicators.BollingerBands{Upper: 1_060_000, Middle: 1_030_000, Lower: 1_000_500},
			price:      1_000_000,
			wantSignal: true,
			wantSide:   "BUY",
		},
		{
			name:       "short entry suppressed when reversion target is inside the fee cost",
			bb:         &indicators.BollingerBands{Upper: 1_000_500, Middle: 999_000, Lower: 996_000},
			price:      1_001_000,
			wantSignal: false,
		},
		{
			// Short entries must remain possible. This is the regression guard
			// for scoring a short with long arithmetic, which would suppress
			// every short regardless of how favourable it is.
			name:       "short entry fires when reversion target clears the fee cost",
			bb:         &indicators.BollingerBands{Upper: 1_000_500, Middle: 970_000, Lower: 940_000},
			price:      1_001_000,
			wantSignal: true,
			wantSide:   "SELL",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newGatedMeanReversion(t, feeProvider, nil)

			sig, err := s.generateEntrySignal(tc.price, tc.bb)
			if err != nil {
				t.Fatalf("generateEntrySignal: %v", err)
			}

			if !tc.wantSignal {
				if sig != nil {
					t.Fatalf("expected the fee gate to suppress the entry, got %s at %.2f (%s)", sig.Side, sig.Price, sig.Reason)
				}
				return
			}
			if sig == nil {
				t.Fatal("expected a signal — the expected move clears round-trip cost")
			}
			if sig.Side != tc.wantSide {
				t.Errorf("side = %q, want %q", sig.Side, tc.wantSide)
			}
		})
	}
}

// TestMeanReversion_EntryGateRespectsMinNetProfitBPS proves the configured
// margin, not just break-even, is enforced.
func TestMeanReversion_EntryGateRespectsMinNetProfitBPS(t *testing.T) {
	feeProvider := &fakeFeeProvider{maker: testMakerRate, taker: testTakerRate, ok: true}
	// Middle band ~1.5% above price: clears the ~1.15% round trip, but only by
	// about 35 bps.
	bb := &indicators.BollingerBands{Upper: 1_030_000, Middle: 1_015_000, Lower: 1_000_500}

	relaxed := newGatedMeanReversion(t, feeProvider, nil)
	if sig, _ := relaxed.generateEntrySignal(1_000_000, bb); sig == nil {
		t.Fatal("with no extra margin required, this entry should fire")
	}

	strict := newGatedMeanReversion(t, feeProvider, map[string]interface{}{
		"min_net_profit_bps": float64(100), // demand 1% of net edge on top of fees
	})
	if sig, _ := strict.generateEntrySignal(1_000_000, bb); sig != nil {
		t.Fatalf("with a 100 bps margin required, this entry should be suppressed, got %s", sig.Reason)
	}
}

// TestMeanReversion_UngatedBehaviourUnchanged is the regression guard for every
// existing deployment: with no fee provider and no fallback, the strategy must
// behave exactly as it did before fee gating existed.
func TestMeanReversion_UngatedBehaviourUnchanged(t *testing.T) {
	s := newGatedMeanReversion(t, nil, nil)

	// A move far too small to cover real fees. Ungated, it must still fire.
	bb := &indicators.BollingerBands{Upper: 1_004_000, Middle: 1_002_000, Lower: 1_000_500}
	sig, err := s.generateEntrySignal(1_000_000, bb)
	if err != nil {
		t.Fatalf("generateEntrySignal: %v", err)
	}
	if sig == nil {
		t.Fatal("with no fee provider configured the gate must be a no-op and preserve legacy behaviour")
	}
	if sig.Side != "BUY" {
		t.Errorf("side = %q, want BUY", sig.Side)
	}
}

// TestMeanReversion_ExitFeeGate proves a voluntary take-profit will not realize
// a net loss, while the stop-loss override still can.
func TestMeanReversion_ExitFeeGate(t *testing.T) {
	feeProvider := &fakeFeeProvider{maker: testMakerRate, taker: testTakerRate, ok: true}
	// Price sits at the middle band so the reversion exit condition is met.
	bb := &indicators.BollingerBands{Upper: 1_010_000, Middle: 1_000_000, Lower: 990_000}

	t.Run("take-profit suppressed when the close would be a net loss", func(t *testing.T) {
		s := newGatedMeanReversion(t, feeProvider, nil)
		state := StrategyState{
			HasPosition:  true,
			PositionSide: "LONG",
			PositionSize: 0.001,
			// Entry equals the exit price: gross flat, so net is negative once
			// both fee legs are paid.
			EntryPrice: 1_000_000,
		}
		sig, err := s.generateExitSignal(1_000_000, bb, &state)
		if err != nil {
			t.Fatalf("generateExitSignal: %v", err)
		}
		if sig != nil {
			t.Fatalf("expected suppression of a net-losing voluntary exit, got %q", sig.Reason)
		}
	})

	t.Run("take-profit fires when the close is genuinely net profitable", func(t *testing.T) {
		s := newGatedMeanReversion(t, feeProvider, nil)
		state := StrategyState{
			HasPosition:  true,
			PositionSide: "LONG",
			PositionSize: 0.001,
			EntryPrice:   980_000, // ~2% below the exit, clears ~1.15% of fees
		}
		sig, err := s.generateExitSignal(1_000_000, bb, &state)
		if err != nil {
			t.Fatalf("generateExitSignal: %v", err)
		}
		if sig == nil {
			t.Fatal("a net-profitable reversion exit must still fire")
		}
		if got := sig.Metadata["exit_reason"]; got != "take_profit" {
			t.Errorf("exit_reason = %v, want take_profit", got)
		}
	})

	t.Run("stop-loss overrides the net-loss guard", func(t *testing.T) {
		s := newGatedMeanReversion(t, feeProvider, map[string]interface{}{
			"stop_loss_bps": float64(200), // 2%
		})
		state := StrategyState{
			HasPosition:  true,
			PositionSide: "LONG",
			PositionSize: 0.001,
			EntryPrice:   1_050_000, // ~4.8% underwater: a clear net loss
		}
		sig, err := s.generateExitSignal(1_000_000, bb, &state)
		if err != nil {
			t.Fatalf("generateExitSignal: %v", err)
		}
		if sig == nil {
			t.Fatal("stop-loss must fire even though the close realizes a net loss — the guard must not trap a losing position")
		}
		if got := sig.Metadata["exit_reason"]; got != "stop_loss" {
			t.Errorf("exit_reason = %v, want stop_loss", got)
		}
	})

	t.Run("no entry price means the gate cannot block", func(t *testing.T) {
		s := newGatedMeanReversion(t, feeProvider, nil)
		state := StrategyState{
			HasPosition:  true,
			PositionSide: "LONG",
			PositionSize: 0.001,
			EntryPrice:   0,
		}
		sig, err := s.generateExitSignal(1_000_000, bb, &state)
		if err != nil {
			t.Fatalf("generateExitSignal: %v", err)
		}
		if sig == nil {
			t.Fatal("with no known entry price the exit must not be blocked")
		}
	})
}
