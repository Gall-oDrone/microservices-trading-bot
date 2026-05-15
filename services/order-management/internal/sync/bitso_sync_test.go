package sync

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/shared/pkg/bitso"
)

// fakeBitsoClient implements BitsoSyncClient for tests.
type fakeBitsoClient struct {
	orderTrades        map[string][]bitso.UserOrderTrade
	orderTradesErr     map[string]error
	orderTradesCallsBy map[string]int
}

func newFakeBitsoClient() *fakeBitsoClient {
	return &fakeBitsoClient{
		orderTrades:        map[string][]bitso.UserOrderTrade{},
		orderTradesErr:     map[string]error{},
		orderTradesCallsBy: map[string]int{},
	}
}

func (f *fakeBitsoClient) LookupOrders(_ []string) ([]bitso.UserOrder, error) {
	return nil, nil
}

func (f *fakeBitsoClient) OrderTrades(oid string, _ url.Values) ([]bitso.UserOrderTrade, error) {
	f.orderTradesCallsBy[oid]++
	if err, ok := f.orderTradesErr[oid]; ok {
		return nil, err
	}
	return f.orderTrades[oid], nil
}

func (f *fakeBitsoClient) MyOpenOrders(_ url.Values) ([]bitso.UserOrder, error) {
	return nil, nil
}

// recordingOrderManager captures calls to OrderManagerSync so we can assert
// which path was taken.
type recordingOrderManager struct {
	syncCalls       []syncCall
	syncTradesCalls []syncTradesCall
	staleCalls      []string
}

type syncCall struct {
	OID          string
	FilledAmount float64
	AvgPrice     float64
	Status       models.OrderStatus
}

type syncTradesCall struct {
	OID    string
	Trades []bitso.UserOrderTrade
}

func (r *recordingOrderManager) ListActiveBitsoOrderIDs(_ context.Context) ([]string, error) {
	return nil, nil
}

func (r *recordingOrderManager) SyncOrderFromBitso(_ context.Context, oid string, filled, avg float64, status models.OrderStatus) error {
	r.syncCalls = append(r.syncCalls, syncCall{OID: oid, FilledAmount: filled, AvgPrice: avg, Status: status})
	return nil
}

func (r *recordingOrderManager) SyncOrderFromBitsoTrades(_ context.Context, oid string, trades []bitso.UserOrderTrade) error {
	r.syncTradesCalls = append(r.syncTradesCalls, syncTradesCall{OID: oid, Trades: trades})
	return nil
}

func (r *recordingOrderManager) MarkOrderStale(_ context.Context, oid string) error {
	r.staleCalls = append(r.staleCalls, oid)
	return nil
}

// userOrderForTest builds a *bitso.UserOrder with the given filled amount and limit price.
func userOrderForTest(oid string, original, unfilled, limitPrice float64, status bitso.OrderStatus) *bitso.UserOrder {
	return &bitso.UserOrder{
		OID:            oid,
		OriginalAmount: bitso.ToMonetary(original),
		UnfilledAmount: bitso.ToMonetary(unfilled),
		Price:          bitso.ToMonetary(limitPrice),
		Status:         status,
	}
}

func newJobForTest(client BitsoSyncClient, om OrderManagerSync) *BitsoSyncJob {
	return NewBitsoSyncJob(client, om, logger.DefaultLogger(), 0, nil)
}

// Regression test for the bug where applyBitsoUserOrder used uo.Price (the submitted
// limit price) as avg_price instead of computing the real VWAP from /order_trades.
//
// Reproduces:
//   - BUY limit @ 1,372,600, filled at 1,372,560 on Bitso (real fill).
//   - SELL limit @ 1,370,080, filled at 1,371,650 on Bitso (real fill).
// Expectation after fix: SyncOrderFromBitsoTrades is called with the real trade
// records, and the legacy SyncOrderFromBitso(filledAmount, limitPrice) path is NOT
// used for filled orders.
func TestApplyBitsoUserOrder_FilledOrderUsesActualTradePriceNotLimitPrice(t *testing.T) {
	const (
		buyOID      = "7k6uUi5qfD6m7hXM"
		sellOID     = "yHPaiePH4orsHvI6"
		buyLimit    = 1_372_600.0
		buyActual   = 1_372_560.0
		sellLimit   = 1_370_080.0
		sellActual  = 1_371_650.0
		positionSz  = 0.001
	)

	bc := newFakeBitsoClient()
	bc.orderTrades[buyOID] = []bitso.UserOrderTrade{{
		OID:   buyOID,
		Major: bitso.ToMonetary(positionSz),
		Price: bitso.ToMonetary(buyActual),
		Side:  bitso.OrderSideBuy,
	}}
	bc.orderTrades[sellOID] = []bitso.UserOrderTrade{{
		OID:   sellOID,
		Major: bitso.ToMonetary(positionSz),
		Price: bitso.ToMonetary(sellActual),
		Side:  bitso.OrderSideSell,
	}}

	om := &recordingOrderManager{}
	job := newJobForTest(bc, om)

	// Filled BUY: original=0.001, unfilled=0 → filled=0.001
	job.applyBitsoUserOrder(context.Background(), userOrderForTest(buyOID, positionSz, 0, buyLimit, bitso.OrderStatusCompleted))
	// Filled SELL
	job.applyBitsoUserOrder(context.Background(), userOrderForTest(sellOID, positionSz, 0, sellLimit, bitso.OrderStatusCompleted))

	if len(om.syncTradesCalls) != 2 {
		t.Fatalf("expected SyncOrderFromBitsoTrades to be called twice, got %d (calls=%+v, syncCalls=%+v)",
			len(om.syncTradesCalls), om.syncTradesCalls, om.syncCalls)
	}
	if len(om.syncCalls) != 0 {
		t.Fatalf("expected SyncOrderFromBitso (limit-price path) NOT to be called for filled orders, got %+v", om.syncCalls)
	}

	// Trade records must carry the real fill price, not the submitted limit.
	got := om.syncTradesCalls
	if got[0].OID != buyOID {
		t.Fatalf("buy: oid=%q, want %q", got[0].OID, buyOID)
	}
	if px := (&got[0].Trades[0].Price).Float64(); px != buyActual {
		t.Fatalf("buy: trade price=%v, want actual fill %v (limit was %v)", px, buyActual, buyLimit)
	}
	if got[1].OID != sellOID {
		t.Fatalf("sell: oid=%q, want %q", got[1].OID, sellOID)
	}
	if px := (&got[1].Trades[0].Price).Float64(); px != sellActual {
		t.Fatalf("sell: trade price=%v, want actual fill %v (limit was %v)", px, sellActual, sellLimit)
	}

	// And /order_trades was actually consulted.
	if bc.orderTradesCallsBy[buyOID] == 0 || bc.orderTradesCallsBy[sellOID] == 0 {
		t.Fatalf("expected OrderTrades to be called for both filled orders, got %+v", bc.orderTradesCallsBy)
	}
}

// When /order_trades is not yet available (Bitso 378 race right after fill),
// we must NOT fall back to writing the limit price as avg_price. Instead we
// defer the sync so the next tick / user-trades poller can reconcile with the
// real fill price.
func TestApplyBitsoUserOrder_FilledOrderDefersWhenTradesUnavailable(t *testing.T) {
	const oid = "no-trades-yet"

	bc := newFakeBitsoClient()
	bc.orderTradesErr[oid] = errors.New("Bitso 378: order has not matched yet")
	om := &recordingOrderManager{}
	job := newJobForTest(bc, om)

	job.applyBitsoUserOrder(context.Background(), userOrderForTest(oid, 0.001, 0, 1_500_000, bitso.OrderStatusCompleted))

	if len(om.syncTradesCalls) != 0 {
		t.Fatalf("expected no SyncOrderFromBitsoTrades when trades not available, got %+v", om.syncTradesCalls)
	}
	if len(om.syncCalls) != 0 {
		t.Fatalf("expected NO SyncOrderFromBitso(limit-price) call when trades unavailable, got %+v", om.syncCalls)
	}
}

// Status-only updates (no fills yet) should still flow through the legacy
// SyncOrderFromBitso path so we can record state transitions like cancelled.
func TestApplyBitsoUserOrder_NoFillsForwardsStatus(t *testing.T) {
	const oid = "open-no-fills"
	bc := newFakeBitsoClient()
	om := &recordingOrderManager{}
	job := newJobForTest(bc, om)

	job.applyBitsoUserOrder(context.Background(), userOrderForTest(oid, 0.001, 0.001, 1_500_000, bitso.OrderStatusOpen))

	if len(om.syncCalls) != 1 {
		t.Fatalf("expected one SyncOrderFromBitso for status-only update, got %+v", om.syncCalls)
	}
	if om.syncCalls[0].FilledAmount != 0 {
		t.Fatalf("expected filled=0, got %v", om.syncCalls[0].FilledAmount)
	}
	if len(om.syncTradesCalls) != 0 {
		t.Fatalf("expected no SyncOrderFromBitsoTrades for unfilled order, got %+v", om.syncTradesCalls)
	}
	if bc.orderTradesCallsBy[oid] != 0 {
		t.Fatalf("OrderTrades should not be called for unfilled order, got %d calls", bc.orderTradesCallsBy[oid])
	}
}
