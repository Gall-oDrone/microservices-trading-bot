package strategies

import (
	"context"
	"testing"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

const testBook = "btc_mxn"

// newGatedMomentum builds a momentum strategy over an in-memory indicator
// store the test can seed directly.
func newGatedMomentum(t *testing.T, provider MakerTakerFeeProvider, params map[string]interface{}) (*MomentumStrategy, indicators.IndicatorStore) {
	t.Helper()
	s := NewMomentumStrategy()
	store := indicators.NewInMemoryIndicatorStore()
	svc := indicators.NewService(nil, store, indicators.NewMockDataProvider(), nil)
	cfg := StrategyConfig{Name: "test_momentum", Type: "momentum", Version: "1.0.0", Enabled: true, Book: testBook, Parameters: params}
	if err := s.Initialize(cfg, svc); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if provider != nil {
		s.SetFeeRatesProvider(provider)
	}
	return s, store
}

func setIndicator(t *testing.T, store indicators.IndicatorStore, name string, period int, v float64) {
	t.Helper()
	if err := store.Set(context.Background(), testBook, name, period, &indicators.IndicatorValue{Name: name, Period: period, Value: v, Book: testBook}); err != nil {
		t.Fatalf("set %s: %v", name, err)
	}
}

// Service defaults when constructed with a nil config.
var defaultInd = indicators.DefaultServiceConfig()

// Long-entry setup used throughout: RSI oversold with price above EMA.
const (
	entryPrice = 1_000_000.0
	entryRSI   = 25.0
	entryEMA   = 990_000.0
)

func TestMomentum_GateOnByDefault(t *testing.T) {
	s, _ := newGatedMomentum(t, nil, nil)
	if !s.feeGate.HasRates() {
		t.Fatal("momentum fee gate must be active by default (DefaultFallbackRoundTripBPS)")
	}
}

// TestMomentum_EntryFeeGate: the expected move is ExpectedMoveATRMult x ATR.
// With Bitso retail rates (maker 0.50% buy, taker 0.65% sell) the round trip
// is ~115 bps, so a 50 bps ATR must be refused and a 300 bps ATR allowed.
func TestMomentum_EntryFeeGate(t *testing.T) {
	provider := &fakeFeeProvider{maker: testMakerRate, taker: testTakerRate, ok: true}
	tests := []struct {
		name   string
		atr    float64
		params map[string]interface{}
		want   bool
	}{
		{"ATR 50bps cannot pay the round trip", 5_000, nil, false},
		{"ATR 300bps clears the round trip", 30_000, nil, true},
		{"ATR 300bps but 250bps margin demanded", 30_000, map[string]interface{}{"min_net_profit_bps": float64(250)}, false},
		{"ATR 50bps with 3x multiplier (150bps) clears", 5_000, map[string]interface{}{"expected_move_atr_mult": float64(3)}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, store := newGatedMomentum(t, provider, tc.params)
			setIndicator(t, store, "atr", defaultInd.ATRPeriod, tc.atr)
			sig, err := s.generateEntrySignal(entryPrice, entryRSI, entryEMA)
			if err != nil {
				t.Fatalf("generateEntrySignal: %v", err)
			}
			if got := sig != nil; got != tc.want {
				t.Fatalf("signal emitted = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMomentum_ShortEntryUsesShortArithmetic guards against scoring shorts
// with long arithmetic, which would make every short look unprofitable.
func TestMomentum_ShortEntryUsesShortArithmetic(t *testing.T) {
	provider := &fakeFeeProvider{maker: testMakerRate, taker: testTakerRate, ok: true}
	s, store := newGatedMomentum(t, provider, nil)
	setIndicator(t, store, "atr", defaultInd.ATRPeriod, 30_000)
	// RSI overbought with price below EMA => short entry.
	sig, _ := s.generateEntrySignal(1_000_000, 75, 1_010_000)
	if sig == nil || sig.Side != "SELL" {
		t.Fatalf("expected a SELL entry for a 300bps expected down-move, got %+v", sig)
	}
}

func TestMomentum_MissingATRBlocksGatedEntry(t *testing.T) {
	s, _ := newGatedMomentum(t, nil, nil) // default gate, no ATR seeded
	if sig, _ := s.generateEntrySignal(entryPrice, entryRSI, entryEMA); sig != nil {
		t.Fatal("with the gate active and no ATR, there is no expected move to justify the cost; entry must be refused")
	}
}

func TestMomentum_ExplicitOptOutDisablesGate(t *testing.T) {
	s, _ := newGatedMomentum(t, nil, map[string]interface{}{"fallback_round_trip_bps": float64(0)})
	if s.feeGate.HasRates() {
		t.Fatal("fallback_round_trip_bps=0 with no provider must disable the gate")
	}
	if sig, _ := s.generateEntrySignal(entryPrice, entryRSI, entryEMA); sig == nil {
		t.Fatal("ungated, the RSI/EMA entry must fire regardless of ATR")
	}
}

// TestMomentum_TakeProfitRefusesNetLoss: the RSI-neutral exit is voluntary,
// so it must not close a long whose gain does not cover round-trip fees.
func TestMomentum_TakeProfitRefusesNetLoss(t *testing.T) {
	provider := &fakeFeeProvider{maker: testMakerRate, taker: testTakerRate, ok: true}
	s, _ := newGatedMomentum(t, provider, nil)
	state := StrategyState{HasPosition: true, PositionSide: "BUY", EntryPrice: entryPrice, PositionSize: 0.001}

	if sig, _ := s.generateExitSignal(1_005_000, 50, entryEMA, &state); sig != nil {
		t.Fatal("+50 bps cannot cover ~115 bps of fees; take_profit must hold rather than realize a net loss")
	}
	if sig, _ := s.generateExitSignal(1_020_000, 50, entryEMA, &state); sig == nil {
		t.Fatal("+200 bps clears fees; take_profit must fire")
	}
}

// TestMomentum_StopLossStillRealizesLoss: risk overrides are evaluated before
// the net-loss guard and must remain able to close at a loss.
func TestMomentum_StopLossStillRealizesLoss(t *testing.T) {
	provider := &fakeFeeProvider{maker: testMakerRate, taker: testTakerRate, ok: true}
	s, store := newGatedMomentum(t, provider, map[string]interface{}{
		"stop_loss_quote":     float64(5_000),
		"min_signal_interval": float64(0),
	})
	setIndicator(t, store, "rsi", defaultInd.RSIPeriod, 50)
	setIndicator(t, store, "ema", defaultInd.EMAPeriod, entryEMA)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.UpdateState(func(st *StrategyState) {
		st.HasPosition = true
		st.PositionSide = "BUY"
		st.EntryPrice = entryPrice
		st.PositionSize = 0.001
	})

	sig, err := s.OnTick(&indicators.Trade{Price: 990_000, Amount: 0.01})
	if err != nil {
		t.Fatalf("OnTick: %v", err)
	}
	if sig == nil || sig.Side != "SELL" {
		t.Fatalf("stop-loss must close the long at a loss despite the fee gate, got %+v", sig)
	}
	if reason, _ := sig.Metadata["exit_reason"].(string); reason != "stop_loss" {
		t.Errorf("exit_reason = %q, want stop_loss", reason)
	}
}
