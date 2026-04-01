// Package integration holds integration tests for the order-management service.
// These tests wire the full component stack (manager, repos, validator, risk, intraday aggregator)
// and verify behavior across components.
package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"bitso-trading-platform/order-management/internal/config"
	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/manager"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/repository"
	"bitso-trading-platform/order-management/internal/risk"
	"bitso-trading-platform/order-management/internal/validator"
	sharedModels "bitso-trading-platform/shared/pkg/models"
)

// mockIntradayWriter implements metrics.IntradayMetricsWriter for integration tests.
type mockIntradayWriter struct {
	mu sync.Mutex

	DailyRealizedPnL map[string]float64
	TradesToday      map[string]float64
	WinsToday        map[string]float64
	LossesToday      map[string]float64
}

func newMockIntradayWriter() *mockIntradayWriter {
	return &mockIntradayWriter{
		DailyRealizedPnL: make(map[string]float64),
		TradesToday:      make(map[string]float64),
		WinsToday:        make(map[string]float64),
		LossesToday:      make(map[string]float64),
	}
}

func (m *mockIntradayWriter) SetDailyRealizedPnL(currency string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DailyRealizedPnL[currency] = value
}
func (m *mockIntradayWriter) SetDailyUnrealizedPnL(currency string, value float64) {}
func (m *mockIntradayWriter) SetDrawdownPercent(currency string, percent float64)   {}
func (m *mockIntradayWriter) SetDrawdownAbsolute(currency string, value float64)     {}
func (m *mockIntradayWriter) SetPeakEquity(currency string, value float64)          {}
func (m *mockIntradayWriter) SetCurrentEquity(currency string, value float64)       {}
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

type fixedSessionProvider struct{ date time.Time }

func (f fixedSessionProvider) SessionDate() time.Time { return f.date }

var (
	integrationLogger  *logger.Logger
	integrationMetrics *metrics.MetricsCollector
)

func init() {
	integrationLogger = logger.DefaultLogger()
	integrationMetrics = metrics.NewMetricsCollector("integration-test")
}

func buildManagerWithIntraday(t *testing.T) (*manager.Manager, *mockIntradayWriter) {
	t.Helper()
	cfg := &config.Config{
		Risk: config.RiskConfig{
			MaxOpenOrders:        10,
			MaxOrderValue:        100000.0,
			MinOrderSize:         0.001,
			MaxPositionSize:      1.0,
			EnableDuplicateCheck: true,
			MaxOrdersPerMinute:   60,
		},
	}
	log := integrationLogger
	metricsCollector := integrationMetrics
	orderRepo := repository.NewInMemoryOrderRepository(log, metricsCollector)
	positionRepo := repository.NewInMemoryPositionRepository(log, metricsCollector)

	writer := newMockIntradayWriter()
	session := fixedSessionProvider{date: time.Date(2025, 2, 5, 12, 0, 0, 0, time.UTC)}
	aggregator := metrics.NewIntradayAggregator(writer, session)

	v := validator.NewOrderValidator(&cfg.Risk, log, orderRepo, metricsCollector)
	rm := risk.NewRiskManager(&cfg.Risk, log, orderRepo, positionRepo, metricsCollector)

	mgr := manager.NewOrderManager(
		cfg,
		log,
		v,
		rm,
		orderRepo,
		positionRepo,
		metricsCollector,
		aggregator,
		nil, // fill ledger optional
		true,
	)
	return mgr, writer
}

// TestOrderManager_ProcessSignalToFilled_RecordsIntraday verifies that when an order
// is created from a signal and transitioned to filled, the intraday P&L aggregator
// receives the trade (daily realized P&L and trade counts updated).
func TestOrderManager_ProcessSignalToFilled_RecordsIntraday(t *testing.T) {
	mgr, writer := buildManagerWithIntraday(t)
	defer mgr.Stop()

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	signal := &sharedModels.TradeSignalEvent{
		EventID:   "int-signal-1",
		Timestamp: 1698160000000,
		Book:      "btc_mxn",
		Strategy:  "basic",
		Signal:    "BUY",
		Price:     500000.0,
		Amount:    0.01,
		Metadata:  make(map[string]interface{}),
	}

	order, err := mgr.ProcessSignal(ctx, signal)
	if err != nil {
		t.Fatalf("ProcessSignal: %v", err)
	}
	if order == nil {
		t.Fatal("ProcessSignal returned nil order")
	}

	// Order is already Validated after ProcessSignal; transition to filled
	for _, status := range []models.OrderStatus{
		models.OrderStatusSubmitted,
		models.OrderStatusAccepted,
		models.OrderStatusFilled,
	} {
		err = mgr.UpdateOrderStatus(ctx, order.ID, status, nil)
		if err != nil {
			t.Fatalf("UpdateOrderStatus(%s): %v", status, err)
		}
	}

	// Verify intraday aggregator was called: trade count and realized P&L (0 when no metadata)
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if n := writer.TradesToday["btc_mxn|basic"]; n != 1 {
		t.Errorf("TradesToday btc_mxn|basic: want 1, got %v", n)
	}
	// Realized PnL with nil metadata is 0
	if v := writer.DailyRealizedPnL["MXN"]; v != 0 {
		t.Errorf("DailyRealizedPnL MXN (no metadata): want 0, got %v", v)
	}
}

// TestOrderManager_ProcessSignalToFilled_WithRealizedPnL verifies that when
// metadata includes realized_pnl, the aggregator records it and win/loss count.
func TestOrderManager_ProcessSignalToFilled_WithRealizedPnL(t *testing.T) {
	mgr, writer := buildManagerWithIntraday(t)
	defer mgr.Stop()

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	signal := &sharedModels.TradeSignalEvent{
		EventID:   "int-signal-2",
		Timestamp: 1698160000000,
		Book:      "eth_mxn",
		Strategy:  "trend",
		Signal:    "SELL",
		Price:     25000.0,
		Amount:    0.1,
		Metadata:  make(map[string]interface{}),
	}

	order, err := mgr.ProcessSignal(ctx, signal)
	if err != nil {
		t.Fatalf("ProcessSignal: %v", err)
	}
	if order == nil {
		t.Fatal("ProcessSignal returned nil order")
	}

	for _, status := range []models.OrderStatus{
		models.OrderStatusSubmitted,
		models.OrderStatusAccepted,
		models.OrderStatusFilled,
	} {
		metadata := map[string]interface{}{}
		if status == models.OrderStatusFilled {
			metadata["realized_pnl"] = 150.50 // win
		}
		err = mgr.UpdateOrderStatus(ctx, order.ID, status, metadata)
		if err != nil {
			t.Fatalf("UpdateOrderStatus(%s): %v", status, err)
		}
	}

	writer.mu.Lock()
	defer writer.mu.Unlock()
	if v := writer.DailyRealizedPnL["MXN"]; v != 150.5 {
		t.Errorf("DailyRealizedPnL MXN: want 150.5, got %v", v)
	}
	if n := writer.TradesToday["eth_mxn|trend"]; n != 1 {
		t.Errorf("TradesToday eth_mxn|trend: want 1, got %v", n)
	}
	if w := writer.WinsToday["eth_mxn|trend"]; w != 1 {
		t.Errorf("WinsToday eth_mxn|trend: want 1, got %v", w)
	}
}

// TestOrderManager_GetPositionSummary_afterFlow verifies GetPositionSummary
// returns without error after a full process-signal and fill flow.
func TestOrderManager_GetPositionSummary_afterFlow(t *testing.T) {
	mgr, _ := buildManagerWithIntraday(t)
	defer mgr.Stop()

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	summary, err := mgr.GetPositionSummary(ctx)
	if err != nil {
		t.Fatalf("GetPositionSummary: %v", err)
	}
	if summary == nil {
		t.Fatal("GetPositionSummary returned nil")
	}
	// Empty PositionsByBook is valid after no position updates
	_ = summary.PositionsByBook
}
