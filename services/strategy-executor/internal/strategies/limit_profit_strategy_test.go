package strategies

import (
	"context"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// stubBookFees implements MakerTakerFeeProvider and BookFeeResolver for tests.
type stubBookFees struct {
	maker, taker float64
}

func (s *stubBookFees) MakerTakerRatesForBook(context.Context, string) (float64, float64, bool) {
	return s.maker, s.taker, true
}

func (s *stubBookFees) FeeDecimalsForLegs(_ context.Context, _ string, buyLiq, sellLiq string) (float64, float64, bool) {
	br, sr := s.maker, s.taker
	if strings.EqualFold(buyLiq, "taker") {
		br = s.taker
	}
	if strings.EqualFold(sellLiq, "maker") {
		sr = s.maker
	}
	return br, sr, true
}

func TestLimitProfitStrategy_EntrySignal(t *testing.T) {
	s := NewLimitProfitStrategy()
	cfg := StrategyConfig{
		Name:    "lp_entry",
		Type:    "limit_profit",
		Book:    "btc_mxn",
		Enabled: true,
		Parameters: map[string]interface{}{
			"entry_offset":        float64(100),
			"min_profit":          float64(500),
			"min_signal_interval": float64(0),
			"position_size":       float64(0.001),
			"reference":           "last_trade",
		},
	}
	store := indicators.NewInMemoryIndicatorStore()
	prov := indicators.NewMockDataProvider()
	svc := indicators.NewService(nil, store, prov, nil)
	ctx := context.Background()
	if err := s.Initialize(cfg, svc); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}

	sig, err := s.OnTick(&indicators.Trade{Timestamp: time.Now(), Price: 1_000_000, Amount: 0.01, Side: "buy"})
	if err != nil {
		t.Fatal(err)
	}
	if sig == nil || sig.Side != "BUY" {
		t.Fatalf("expected BUY, got %+v", sig)
	}
	want := 1_000_100.0
	if sig.Price != want {
		t.Errorf("buy price: want %v got %v", want, sig.Price)
	}
	st := s.GetState()
	if st.HasPosition {
		t.Error("position should open only after BUY fill, not on signal emission")
	}
	if !st.PendingBuy {
		t.Error("expected pending_buy after entry signal")
	}
	ev, _ := sig.Metadata["event_id"].(string)
	if ev == "" {
		t.Fatal("expected metadata event_id on BUY signal")
	}
	s.OnOrderFilled(OrderFill{EventID: ev, Book: "btc_mxn", Side: "buy", AveragePrice: want, FilledAmount: 0.001})
	if !s.GetState().HasPosition {
		t.Error("expected position after fill notification")
	}
}

func TestLimitProfitStrategy_ExitSignal(t *testing.T) {
	s := NewLimitProfitStrategy()
	cfg := StrategyConfig{
		Name:    "lp_exit",
		Type:    "limit_profit",
		Book:    "btc_mxn",
		Enabled: true,
		Parameters: map[string]interface{}{
			"entry_offset":        float64(100),
			"min_profit":          float64(200),
			"min_signal_interval": float64(0),
			"position_size":       float64(0.001),
		},
	}
	store := indicators.NewInMemoryIndicatorStore()
	prov := indicators.NewMockDataProvider()
	svc := indicators.NewService(nil, store, prov, nil)
	ctx := context.Background()
	if err := s.Initialize(cfg, svc); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}

	sig0, err := s.OnTick(&indicators.Trade{Price: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if sig0 == nil {
		t.Fatal("expected entry BUY signal")
	}
	ev, _ := sig0.Metadata["event_id"].(string)
	s.OnOrderFilled(OrderFill{EventID: ev, Book: "btc_mxn", Side: "buy", AveragePrice: 1_000_100, FilledAmount: 0.001})
	// entry 1_000_100, need price >= 1_000_300 for min_profit 200
	sig, err := s.OnTick(&indicators.Trade{Price: 1_000_400})
	if err != nil {
		t.Fatal(err)
	}
	if sig == nil || sig.Side != "SELL" {
		t.Fatalf("expected SELL, got %+v", sig)
	}
	if s.GetState().HasPosition {
		t.Error("position should clear after exit")
	}
}

func TestLimitProfitStrategy_ExitBlockedUntilFeeCovered(t *testing.T) {
	s := NewLimitProfitStrategy()
	cfg := StrategyConfig{
		Name:    "lp_fee",
		Type:    "limit_profit",
		Book:    "btc_mxn",
		Enabled: true,
		Parameters: map[string]interface{}{
			"entry_offset":        float64(100),
			"min_profit":          float64(200),
			"fee":                 float64(500),
			"min_signal_interval": float64(0),
			"position_size":       float64(0.001),
		},
	}
	store := indicators.NewInMemoryIndicatorStore()
	prov := indicators.NewMockDataProvider()
	svc := indicators.NewService(nil, store, prov, nil)
	ctx := context.Background()
	if err := s.Initialize(cfg, svc); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}

	sig0, err := s.OnTick(&indicators.Trade{Price: 1_000_000})
	if err != nil || sig0 == nil {
		t.Fatalf("entry: err=%v sig=%v", err, sig0)
	}
	ev, _ := sig0.Metadata["event_id"].(string)
	s.OnOrderFilled(OrderFill{EventID: ev, Book: "btc_mxn", Side: "buy", AveragePrice: 1_000_100, FilledAmount: 0.001})
	// threshold = 1_000_100 + 200 + 500 = 1_000_800
	sig, err := s.OnTick(&indicators.Trade{Price: 1_000_400})
	if err != nil {
		t.Fatal(err)
	}
	if sig != nil {
		t.Fatalf("expected no SELL before threshold, got %+v", sig)
	}
	sig, err = s.OnTick(&indicators.Trade{Price: 1_000_850})
	if err != nil {
		t.Fatal(err)
	}
	if sig == nil || sig.Side != "SELL" {
		t.Fatalf("expected SELL once fee included in threshold, got %+v", sig)
	}
}

func TestLimitProfitStrategy_FeeBPSAddon(t *testing.T) {
	s := NewLimitProfitStrategy()
	// entry 1e6, fee_bps 10 → addon += 1e6 * 2 * 10 / 10000 = 2000
	cfg := StrategyConfig{
		Name:    "lp_bps",
		Type:    "limit_profit",
		Book:    "btc_mxn",
		Enabled: true,
		Parameters: map[string]interface{}{
			"entry_offset":        float64(100),
			"min_profit":          float64(0),
			"fee_bps":             float64(10),
			"min_signal_interval": float64(0),
			"position_size":       float64(0.001),
		},
	}
	store := indicators.NewInMemoryIndicatorStore()
	prov := indicators.NewMockDataProvider()
	svc := indicators.NewService(nil, store, prov, nil)
	if err := s.Initialize(cfg, svc); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	sig0, _ := s.OnTick(&indicators.Trade{Price: 1_000_000})
	ev, _ := sig0.Metadata["event_id"].(string)
	s.OnOrderFilled(OrderFill{EventID: ev, Book: "btc_mxn", Side: "buy", AveragePrice: 1_000_000, FilledAmount: 0.001})
	// threshold = 1_000_000 + 2000 = 1_002_000
	sig, err := s.OnTick(&indicators.Trade{Price: 1_001_500})
	if err != nil {
		t.Fatal(err)
	}
	if sig != nil {
		t.Fatalf("unexpected signal below threshold: %+v", sig)
	}
	if !s.GetState().HasPosition {
		t.Fatal("expected still in position below bps threshold")
	}
	sig, err = s.OnTick(&indicators.Trade{Price: 1_002_100})
	if err != nil {
		t.Fatal(err)
	}
	if sig == nil || sig.Side != "SELL" {
		t.Fatalf("expected SELL after bps threshold, got %+v", sig)
	}
}

func TestLimitProfitStrategy_BitsoFeeProviderThreshold(t *testing.T) {
	s := NewLimitProfitStrategy()
	cfg := StrategyConfig{
		Name:    "lp_bitso_fees",
		Type:    "limit_profit",
		Book:    "btc_mxn",
		Enabled: true,
		Parameters: map[string]interface{}{
			"entry_offset":        float64(100),
			"min_profit":          float64(0),
			"min_signal_interval": float64(0),
			"position_size":       float64(0.001),
			"use_bitso_fees":      true,
		},
	}
	store := indicators.NewInMemoryIndicatorStore()
	prov := indicators.NewMockDataProvider()
	svc := indicators.NewService(nil, store, prov, nil)
	if err := s.Initialize(cfg, svc); err != nil {
		t.Fatal(err)
	}
	s.SetFeeRatesProvider(&stubBookFees{maker: 0.005, taker: 0.0065})
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	sig0, _ := s.OnTick(&indicators.Trade{Price: 1_000_000})
	ev, _ := sig0.Metadata["event_id"].(string)
	entry := 1_000_000.0
	s.OnOrderFilled(OrderFill{EventID: ev, Book: "btc_mxn", Side: "buy", AveragePrice: entry, FilledAmount: 0.001})

	wantThresh := bitso.MinExitPriceAfterRoundTrip(entry, 0.005, 0.0065)
	if _, err := s.OnTick(&indicators.Trade{Price: wantThresh - 50}); err != nil {
		t.Fatal(err)
	}
	if !s.GetState().HasPosition {
		t.Fatal("expected still in position below Bitso threshold")
	}
	sig, err := s.OnTick(&indicators.Trade{Price: wantThresh + 100})
	if err != nil {
		t.Fatal(err)
	}
	if sig == nil || sig.Side != "SELL" {
		t.Fatalf("expected SELL above threshold, got %+v", sig)
	}
	if sig.Metadata["fee_model"] != "bitso_api" {
		t.Fatalf("expected bitso_api fee model, got %v", sig.Metadata["fee_model"])
	}
}

func TestLimitProfitStrategy_MeasuredBuyFeeRateOnFill(t *testing.T) {
	s := NewLimitProfitStrategy()
	cfg := StrategyConfig{
		Name:    "lp_measured_buy_fee",
		Type:    "limit_profit",
		Book:    "btc_mxn",
		Enabled: true,
		Parameters: map[string]interface{}{
			"entry_offset":        float64(0),
			"min_profit":          float64(0),
			"min_signal_interval": float64(0),
			"position_size":       float64(1),
			"use_bitso_fees":      true,
			"buy_liquidity":       "maker",
			"sell_liquidity":      "taker",
		},
	}
	store := indicators.NewInMemoryIndicatorStore()
	prov := indicators.NewMockDataProvider()
	svc := indicators.NewService(nil, store, prov, nil)
	if err := s.Initialize(cfg, svc); err != nil {
		t.Fatal(err)
	}
	// API would say maker=0.01; we override buy leg with measured 0.02 from fill.
	s.SetFeeRatesProvider(&stubBookFees{maker: 0.01, taker: 0.01})
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	sig0, _ := s.OnTick(&indicators.Trade{Price: 100})
	ev, _ := sig0.Metadata["event_id"].(string)
	measured := 0.02
	s.OnOrderFilled(OrderFill{
		EventID: ev, Book: "btc_mxn", Side: "buy", AveragePrice: 100, FilledAmount: 1,
		BuyFeeRate: &measured,
	})
	th, buyR, sellR, _ := s.exitPriceThreshold(context.Background(), 100)
	if buyR != 0.02 {
		t.Fatalf("buy fee should use measured rate, got %v", buyR)
	}
	if sellR != 0.01 {
		t.Fatalf("sell fee from stub taker column, got %v", sellR)
	}
	want := bitso.MinExitPriceAfterRoundTrip(100, 0.02, 0.01)
	if th != want {
		t.Fatalf("threshold: want %v got %v", want, th)
	}
}
