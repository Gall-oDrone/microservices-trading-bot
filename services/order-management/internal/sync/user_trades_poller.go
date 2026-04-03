package sync

import (
	"context"
	"sync"
	"time"

	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/shared/pkg/bitso"
)

// UserTradesPollerMetrics extends BitsoSyncMetrics for user-trades polling.
type UserTradesPollerMetrics interface {
	BitsoSyncMetrics
	RecordUserTradesPollAttempt()
	RecordUserTradesPollError()
	SetUserTradesLastPollTimestamp(ts float64)
}

// OrderManagerUserTrades is the subset of order-manager needed for user-trades polling.
type OrderManagerUserTrades interface {
	// GetOrderByBitsoOrderID returns an order by its Bitso OID (nil if not found).
	GetOrderByBitsoOrderID(ctx context.Context, bitsoOrderID string) (*models.Order, error)
	// SyncOrderFromBitsoTrades updates order state from a slice of UserOrderTrade.
	SyncOrderFromBitsoTrades(ctx context.Context, bitsoOrderID string, trades []bitso.UserOrderTrade) error
}

// UserTradesPoller continuously polls GET /v3/user_trades to discover fills
// for orders that may have completed between sync intervals or whose /orders
// lookup returned 404 (already completed and evicted from Bitso cache).
type UserTradesPoller struct {
	bitsoClient  *bitso.Client
	orderManager OrderManagerUserTrades
	log          *logger.Logger
	interval     time.Duration
	book         string
	metrics      UserTradesPollerMetrics

	mu              sync.Mutex
	lastSeenTradeID uint64 // track most recent trade TID to avoid reprocessing
}

// NewUserTradesPoller creates a poller that polls /user_trades every interval.
// book should be the trading book (e.g. "btc_mxn"); metrics is optional.
func NewUserTradesPoller(
	bitsoClient *bitso.Client,
	orderManager OrderManagerUserTrades,
	log *logger.Logger,
	interval time.Duration,
	book string,
	metrics UserTradesPollerMetrics,
) *UserTradesPoller {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &UserTradesPoller{
		bitsoClient:  bitsoClient,
		orderManager: orderManager,
		log:          log,
		interval:     interval,
		book:         book,
		metrics:      metrics,
	}
}

// Run polls /user_trades until ctx is cancelled.
func (p *UserTradesPoller) Run(ctx context.Context) {
	p.log.Info("User-trades poller started", map[string]interface{}{
		"interval": p.interval,
		"book":     p.book,
	})
	p.pollOnce(ctx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			p.log.Info("User-trades poller stopping", nil)
			return
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

func (p *UserTradesPoller) pollOnce(ctx context.Context) {
	if p.metrics != nil {
		p.metrics.RecordUserTradesPollAttempt()
	}

	// Build params for /user_trades
	params := make(map[string][]string)
	params["book"] = []string{p.book}
	params["sort"] = []string{"desc"}
	params["limit"] = []string{"50"}

	trades, err := p.bitsoClient.MyTrades(params)
	if err != nil {
		p.log.Warn("UserTrades poll failed", map[string]interface{}{"error": err.Error()})
		if p.metrics != nil {
			p.metrics.RecordUserTradesPollError()
		}
		return
	}

	if len(trades) == 0 {
		p.log.Debug("UserTrades poll returned no trades", nil)
		if p.metrics != nil {
			p.metrics.SetUserTradesLastPollTimestamp(float64(time.Now().Unix()))
		}
		return
	}

	// Trades are returned newest first; we process from oldest to newest
	// to maintain correct fill order semantics.
	p.mu.Lock()
	lastSeen := p.lastSeenTradeID
	p.mu.Unlock()

	p.log.Info("UserTrades poll", map[string]interface{}{
		"num_trades":        len(trades),
		"newest_tid":        uint64(trades[0].TID),
		"last_seen_tid":     lastSeen,
	})

	// Find the index of lastSeen (if present) and process everything newer
	startIdx := len(trades)
	for i := len(trades) - 1; i >= 0; i-- {
		tid := uint64(trades[i].TID)
		if tid == lastSeen {
			startIdx = i
			break
		}
	}

	// Process trades from startIdx-1 down to 0 (newest)
	processedAny := false
	newTradesCount := 0
	for i := startIdx - 1; i >= 0; i-- {
		t := &trades[i]
		newTradesCount++
		p.log.Info("Processing new trade", map[string]interface{}{
			"tid":  uint64(t.TID),
			"oid":  t.OID,
			"side": t.Side,
		})
		p.handleTrade(ctx, t)
		processedAny = true
	}

	if newTradesCount > 0 {
		p.log.Info("Processed new trades from user-trades poll", map[string]interface{}{
			"count": newTradesCount,
		})
	}

	// Update lastSeenTradeID to the newest trade
	if processedAny || lastSeen == 0 {
		p.mu.Lock()
		p.lastSeenTradeID = uint64(trades[0].TID)
		p.mu.Unlock()
	}

	if p.metrics != nil {
		p.metrics.SetUserTradesLastPollTimestamp(float64(time.Now().Unix()))
	}
}

func (p *UserTradesPoller) handleTrade(ctx context.Context, t *bitso.UserTrade) {
	oid := t.OID
	if oid == "" {
		return
	}

	// Check if this order belongs to us (exists in OM with bitso_order_id)
	order, err := p.orderManager.GetOrderByBitsoOrderID(ctx, oid)
	if err != nil {
		p.log.Info("handleTrade: order lookup error", map[string]interface{}{
			"tid":            uint64(t.TID),
			"bitso_order_id": oid,
			"error":          err.Error(),
		})
		return
	}
	if order == nil {
		return
	}

	prevStatus := order.Status

	// Skip if order is truly closed (filled, cancelled) but NOT if rejected.
	// Rejected orders may have been placed on Bitso by trading-engine despite risk check failures.
	// If they got filled, we still need to process them for P&L.
	if order.IsClosed() && order.Status != models.OrderStatusRejected {
		p.log.Info("handleTrade: order already closed", map[string]interface{}{
			"tid":            uint64(t.TID),
			"bitso_order_id": oid,
			"order_status":   string(order.Status),
		})
		return
	}

	// If rejected, we'll try to recover and process the fill
	if order.Status == models.OrderStatusRejected {
		p.log.Info("handleTrade: processing fill for rejected order (may have been placed on Bitso)", map[string]interface{}{
			"tid":            uint64(t.TID),
			"bitso_order_id": oid,
			"order_id":       order.ID,
		})
	}

	// Fetch all trades for this order and sync
	p.log.Info("handleTrade: fetching order trades", map[string]interface{}{
		"tid":            uint64(t.TID),
		"bitso_order_id": oid,
		"order_id":       order.ID,
	})

	orderTrades, err := p.bitsoClient.OrderTrades(oid, nil)
	if err != nil {
		p.log.Warn("OrderTrades lookup failed for discovered fill", map[string]interface{}{
			"bitso_order_id": oid,
			"error":          err.Error(),
		})
		return
	}

	if len(orderTrades) == 0 {
		p.log.Warn("No trades returned from OrderTrades", map[string]interface{}{
			"bitso_order_id": oid,
		})
		return
	}

	p.log.Info("handleTrade: calling SyncOrderFromBitsoTrades", map[string]interface{}{
		"bitso_order_id": oid,
		"num_trades":     len(orderTrades),
	})

	if err := p.orderManager.SyncOrderFromBitsoTrades(ctx, oid, orderTrades); err != nil {
		p.log.Warn("SyncOrderFromBitsoTrades failed from user-trades poll", map[string]interface{}{
			"bitso_order_id": oid,
			"error":          err.Error(),
		})
		return
	}

	// Re-fetch order to check if status changed
	updatedOrder, err := p.orderManager.GetOrderByBitsoOrderID(ctx, oid)
	if err != nil {
		p.log.Debug("Failed to re-fetch order after sync", map[string]interface{}{
			"bitso_order_id": oid,
			"error":          err.Error(),
		})
		return
	}

	// Only log if status actually changed to a closed state
	if updatedOrder != nil && updatedOrder.IsClosed() && !order.IsClosed() {
		p.log.Info("Fill synced via user-trades poll", map[string]interface{}{
			"bitso_order_id": oid,
			"trade_id":       uint64(t.TID),
			"order_id":       order.ID,
			"prev_status":    string(prevStatus),
			"new_status":     string(updatedOrder.Status),
			"num_trades":     len(orderTrades),
		})
	}
}
