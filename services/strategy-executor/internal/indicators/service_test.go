package indicators

import (
	"context"
	"testing"
	"time"
)

type stubDataProvider struct {
	bars   []OHLCV
	trades []Trade
}

func (s *stubDataProvider) GetRecentTrades(ctx context.Context, book string, limit int) ([]Trade, error) {
	return s.trades, nil
}

func (s *stubDataProvider) GetRecentBars(ctx context.Context, book, interval string, limit int) ([]OHLCV, error) {
	if len(s.bars) <= limit {
		return s.bars, nil
	}
	return s.bars[len(s.bars)-limit:], nil
}

func (s *stubDataProvider) GetBookTicker(ctx context.Context, book string) (float64, float64, float64, bool) {
	return 0, 0, 0, false
}

func makeBars(n int, base float64) []OHLCV {
	bars := make([]OHLCV, n)
	now := time.Now().UTC().Truncate(time.Minute)
	for i := 0; i < n; i++ {
		price := base + float64(i)*10
		bars[i] = OHLCV{
			Timestamp: now.Add(time.Duration(i-n) * time.Minute),
			Open:      price,
			High:      price + 5,
			Low:       price - 5,
			Close:     price,
			Volume:    0.01,
		}
	}
	return bars
}

func TestComputeAndStoreBarFirst(t *testing.T) {
	cfg := DefaultServiceConfig()
	cfg.BarLimitBuffer = 2
	cfg.MaxStaleness = time.Hour

	store := NewInMemoryIndicatorStore()
	provider := &stubDataProvider{
		bars:   makeBars(25, 1_000_000),
		trades: []Trade{}, // sparse ticks should not block bar-based indicators
	}
	svc := NewService(cfg, store, provider, nil)

	if err := svc.ComputeAndStore(context.Background(), "btc_mxn"); err != nil {
		t.Fatalf("ComputeAndStore: %v", err)
	}

	snap, err := svc.GetSnapshot(context.Background(), "btc_mxn")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if !snap.DataHealthy {
		t.Fatalf("expected data_healthy=true, reason=%q", snap.StaleReason)
	}
	if snap.SMA == nil || snap.EMA == nil || snap.RSI == nil || snap.Bollinger == nil || snap.ATR == nil {
		t.Fatalf("expected all core indicators, got %+v", snap)
	}
	if snap.CurrentPrice <= 0 {
		t.Fatalf("expected current_price from last bar close, got %v", snap.CurrentPrice)
	}
}

func TestComputeAndStoreStaleBarAge(t *testing.T) {
	cfg := DefaultServiceConfig()
	cfg.MaxStaleness = 15 * time.Minute

	bars := makeBars(25, 1_000_000)
	for i := range bars {
		bars[i].Timestamp = bars[i].Timestamp.Add(-time.Hour)
	}

	store := NewInMemoryIndicatorStore()
	provider := &stubDataProvider{bars: bars}
	svc := NewService(cfg, store, provider, nil)

	if err := svc.ComputeAndStore(context.Background(), "btc_mxn"); err != nil {
		t.Fatalf("ComputeAndStore: %v", err)
	}

	snap, err := svc.GetSnapshot(context.Background(), "btc_mxn")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if snap.DataHealthy {
		t.Fatalf("expected data_healthy=false for stale bars, reason=%q", snap.StaleReason)
	}
	if snap.StaleReason == "" {
		t.Fatal("expected stale_reason when bars are stale")
	}
	if snap.LastBarAgeSec <= float64((15 * time.Minute).Seconds()) {
		t.Fatalf("expected last_bar_age_sec > 15m, got %v", snap.LastBarAgeSec)
	}
}

func TestComputeAndStoreInsufficientBars(t *testing.T) {
	cfg := DefaultServiceConfig()
	store := NewInMemoryIndicatorStore()
	provider := &stubDataProvider{bars: makeBars(5, 1_000_000)}
	svc := NewService(cfg, store, provider, nil)

	if err := svc.ComputeAndStore(context.Background(), "btc_mxn"); err != nil {
		t.Fatalf("ComputeAndStore: %v", err)
	}

	snap, err := svc.GetSnapshot(context.Background(), "btc_mxn")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if snap.DataHealthy {
		t.Fatal("expected data_healthy=false when bars below minimum")
	}
}
