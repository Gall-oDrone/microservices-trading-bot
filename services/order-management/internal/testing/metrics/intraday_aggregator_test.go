package metricstest

import (
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"bitso-trading-platform/order-management/internal/metrics"
	sharedMetrics "bitso-trading-platform/shared/pkg/metrics"
)

// mockIntradayWriter records calls for testing and implements metrics.IntradayMetricsWriter.
type mockIntradayWriter struct {
	mu sync.Mutex

	DailyRealizedPnL   map[string]float64
	DailyUnrealizedPnL map[string]float64
	DrawdownPercent    map[string]float64
	DrawdownAbsolute   map[string]float64
	PeakEquity         map[string]float64
	CurrentEquity      map[string]float64
	TradesToday        map[string]float64 // key book|strategy
	WinsToday          map[string]float64
	LossesToday        map[string]float64
}

func newMockWriter() *mockIntradayWriter {
	return &mockIntradayWriter{
		DailyRealizedPnL:   make(map[string]float64),
		DailyUnrealizedPnL: make(map[string]float64),
		DrawdownPercent:    make(map[string]float64),
		DrawdownAbsolute:   make(map[string]float64),
		PeakEquity:         make(map[string]float64),
		CurrentEquity:      make(map[string]float64),
		TradesToday:        make(map[string]float64),
		WinsToday:          make(map[string]float64),
		LossesToday:        make(map[string]float64),
	}
}

func (m *mockIntradayWriter) SetDailyRealizedPnL(currency string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DailyRealizedPnL[currency] = value
}
func (m *mockIntradayWriter) SetDailyUnrealizedPnL(currency string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DailyUnrealizedPnL[currency] = value
}
func (m *mockIntradayWriter) SetDrawdownPercent(currency string, percent float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DrawdownPercent[currency] = percent
}
func (m *mockIntradayWriter) SetDrawdownAbsolute(currency string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DrawdownAbsolute[currency] = value
}
func (m *mockIntradayWriter) SetPeakEquity(currency string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PeakEquity[currency] = value
}
func (m *mockIntradayWriter) SetCurrentEquity(currency string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CurrentEquity[currency] = value
}
func (m *mockIntradayWriter) SetTradesToday(book, strategy string, count float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TradesToday[book+"|"+strategy] = count
}
func (m *mockIntradayWriter) SetWinsToday(book, strategy string, count float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WinsToday[book+"|"+strategy] = count
}
func (m *mockIntradayWriter) SetLossesToday(book, strategy string, count float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LossesToday[book+"|"+strategy] = count
}

// fixedSessionProvider returns a fixed date for deterministic tests (implements sharedMetrics.SessionProvider).
type fixedSessionProvider struct {
	date time.Time
}

func (f fixedSessionProvider) SessionDate() time.Time {
	return f.date
}

func TestIntradayAggregator_RecordTradeClosed(t *testing.T) {
	writer := newMockWriter()
	session := fixedSessionProvider{date: time.Date(2025, 2, 5, 12, 0, 0, 0, time.UTC)}
	agg := metrics.NewIntradayAggregator(writer, session)

	outcome := sharedMetrics.TradeOutcome{
		Book:        "btc_mxn",
		Strategy:    "basic",
		Currency:    "MXN",
		RealizedPnL: sharedMetrics.NewMonetaryAmount(decimal.NewFromFloat(100.50), "MXN"),
		IsWin:       true,
	}
	agg.RecordTradeClosed(outcome)

	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.DailyRealizedPnL["MXN"] != 100.5 {
		t.Errorf("DailyRealizedPnL MXN: want 100.5, got %v", writer.DailyRealizedPnL["MXN"])
	}
	if writer.TradesToday["btc_mxn|basic"] != 1 {
		t.Errorf("TradesToday: want 1, got %v", writer.TradesToday["btc_mxn|basic"])
	}
	if writer.WinsToday["btc_mxn|basic"] != 1 {
		t.Errorf("WinsToday: want 1, got %v", writer.WinsToday["btc_mxn|basic"])
	}
	if writer.LossesToday["btc_mxn|basic"] != 0 {
		t.Errorf("LossesToday: want 0, got %v", writer.LossesToday["btc_mxn|basic"])
	}
}

func TestIntradayAggregator_RecordTradeClosed_Breakeven(t *testing.T) {
	writer := newMockWriter()
	session := fixedSessionProvider{date: time.Date(2025, 2, 5, 12, 0, 0, 0, time.UTC)}
	agg := metrics.NewIntradayAggregator(writer, session)

	outcome := sharedMetrics.TradeOutcome{
		Book:        "btc_mxn",
		Strategy:    "basic",
		Currency:    "MXN",
		RealizedPnL: sharedMetrics.NewMonetaryAmount(decimal.Zero, "MXN"),
		IsBreakeven: true,
	}
	agg.RecordTradeClosed(outcome)

	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.TradesToday["btc_mxn|basic"] != 1 {
		t.Errorf("TradesToday: want 1, got %v", writer.TradesToday["btc_mxn|basic"])
	}
	if writer.WinsToday["btc_mxn|basic"] != 0 || writer.LossesToday["btc_mxn|basic"] != 0 {
		t.Errorf("breakeven should not win/loss: wins=%v losses=%v", writer.WinsToday["btc_mxn|basic"], writer.LossesToday["btc_mxn|basic"])
	}
}

func TestIntradayAggregator_RecordEquityUpdate_Drawdown(t *testing.T) {
	writer := newMockWriter()
	session := fixedSessionProvider{date: time.Date(2025, 2, 5, 12, 0, 0, 0, time.UTC)}
	agg := metrics.NewIntradayAggregator(writer, session)

	agg.RecordEquityUpdate("MXN", decimal.NewFromFloat(10000))
	agg.RecordEquityUpdate("MXN", decimal.NewFromFloat(9500)) // 5% drawdown

	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.PeakEquity["MXN"] != 10000 {
		t.Errorf("PeakEquity: want 10000, got %v", writer.PeakEquity["MXN"])
	}
	if writer.CurrentEquity["MXN"] != 9500 {
		t.Errorf("CurrentEquity: want 9500, got %v", writer.CurrentEquity["MXN"])
	}
	if writer.DrawdownPercent["MXN"] != 5.0 {
		t.Errorf("DrawdownPercent: want 5.0, got %v", writer.DrawdownPercent["MXN"])
	}
	if writer.DrawdownAbsolute["MXN"] != 500 {
		t.Errorf("DrawdownAbsolute: want 500, got %v", writer.DrawdownAbsolute["MXN"])
	}
}

func TestIntradayAggregator_RecordDailyRealizedPnL_Accumulates(t *testing.T) {
	writer := newMockWriter()
	session := fixedSessionProvider{date: time.Date(2025, 2, 5, 12, 0, 0, 0, time.UTC)}
	agg := metrics.NewIntradayAggregator(writer, session)

	agg.RecordDailyRealizedPnL("MXN", decimal.NewFromFloat(10.10))
	agg.RecordDailyRealizedPnL("MXN", decimal.NewFromFloat(20.20))

	writer.mu.Lock()
	defer writer.mu.Unlock()
	// 10.1 + 20.2 = 30.3 (decimal avoids float drift)
	if writer.DailyRealizedPnL["MXN"] != 30.3 {
		t.Errorf("DailyRealizedPnL accumulated: want 30.3, got %v", writer.DailyRealizedPnL["MXN"])
	}
}
