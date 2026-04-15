package sync

import (
	"context"
	"errors"
	"sync"
	"time"

	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/shared/pkg/bitso"
)

// BitsoSyncMetrics is an optional interface for recording Bitso sync metrics (Phase 2).
type BitsoSyncMetrics interface {
	RecordBitsoSyncAttempt()
	RecordBitsoSyncError()
	SetBitsoSyncLastSuccessTimestamp(ts float64)
	// SetActiveOrders sets orders_active{book} from exchange truth (see refreshOpenOrdersGauge).
	SetActiveOrders(book string, count float64)
}

// BitsoSyncJob polls Bitso for active orders and updates order-management (status, fills)
type BitsoSyncJob struct {
	bitsoClient  *bitso.Client
	orderManager OrderManagerSync
	log          *logger.Logger
	interval     time.Duration
	metrics      BitsoSyncMetrics // optional: for Prometheus

	// lastBooksWithBitsoActive tracks books we previously published with count>0 so we can set 0 when empty.
	activeBooksMu            sync.Mutex
	lastBooksWithBitsoActive map[string]struct{}

	// staleOrderRetries tracks how many times an order has failed lookup; after maxStaleRetries, mark as stale.
	staleOrderMu      sync.Mutex
	staleOrderRetries map[string]int
}

const maxStaleRetries = 10

// OrderManagerSync is the subset of order-manager needed for sync
type OrderManagerSync interface {
	ListActiveBitsoOrderIDs(ctx context.Context) ([]string, error)
	SyncOrderFromBitso(ctx context.Context, bitsoOrderID string, filledAmount, avgPrice float64, status models.OrderStatus) error
	SyncOrderFromBitsoTrades(ctx context.Context, bitsoOrderID string, trades []bitso.UserOrderTrade) error
	// MarkOrderStale marks an order as cancelled when it's missing from Bitso after retries.
	MarkOrderStale(ctx context.Context, bitsoOrderID string) error
}

// NewBitsoSyncJob creates a sync job that polls Bitso every interval. metrics is optional (Phase 2).
func NewBitsoSyncJob(bitsoClient *bitso.Client, orderManager OrderManagerSync, log *logger.Logger, interval time.Duration, metrics BitsoSyncMetrics) *BitsoSyncJob {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &BitsoSyncJob{
		bitsoClient:              bitsoClient,
		orderManager:             orderManager,
		log:                      log,
		interval:                 interval,
		metrics:                  metrics,
		lastBooksWithBitsoActive: make(map[string]struct{}),
		staleOrderRetries:        make(map[string]int),
	}
}

// Run runs the sync loop until ctx is cancelled
func (j *BitsoSyncJob) Run(ctx context.Context) {
	j.log.Info("Bitso sync job started", map[string]interface{}{"interval": j.interval})
	j.syncOnce(ctx)
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			j.log.Info("Bitso sync job stopping", nil)
			return
		case <-ticker.C:
			j.syncOnce(ctx)
		}
	}
}

func (j *BitsoSyncJob) syncOnce(ctx context.Context) {
	if j.metrics != nil {
		j.metrics.RecordBitsoSyncAttempt()
	}
	// Always refresh orders_active from Bitso /open_orders so Grafana matches the Stage dashboard (not only OM repo state).
	defer j.refreshOpenOrdersGauge()

	oids, err := j.orderManager.ListActiveBitsoOrderIDs(ctx)
	if err != nil {
		j.log.Warn("ListActiveBitsoOrderIDs failed", map[string]interface{}{"error": err.Error()})
		if j.metrics != nil {
			j.metrics.RecordBitsoSyncError()
		}
		return
	}
	if len(oids) == 0 {
		return
	}

	// Try batch lookup first; if it fails (e.g., 404 due to stale orders), fall back to individual lookups
	orders, batchErr := j.bitsoClient.LookupOrders(oids)
	seen := make(map[string]struct{}, len(orders))
	
	if batchErr == nil {
		// Batch succeeded, process results
		for i := range orders {
			uo := &orders[i]
			seen[uo.OID] = struct{}{}
			j.applyBitsoUserOrder(ctx, uo)
			j.clearStaleRetry(uo.OID)
		}
	} else {
		j.log.Info("Batch LookupOrders failed, falling back to individual lookups", map[string]interface{}{
			"error":      batchErr.Error(),
			"num_orders": len(oids),
		})
	}

	// For orders not in batch response (or if batch failed), try individual OrderTrades lookups
	for _, oid := range oids {
		if _, ok := seen[oid]; ok {
			continue
		}
		
		trades, err := j.bitsoClient.OrderTrades(oid, nil)
		if err != nil {
			// Check if error is Bitso code 378 "Order has not matched yet" - this means
			// the order is still valid and pending on the book, NOT stale.
			var bitsoErr *bitso.Error
			if errors.As(err, &bitsoErr) && bitsoErr.Code() == 378 {
				j.log.Debug("Order has not matched yet (code 378), keeping active", map[string]interface{}{
					"bitso_order_id": oid,
				})
				j.clearStaleRetry(oid)
				continue
			}
			
			// Track this failure for stale order cleanup (true lookup failures only)
			retries := j.incrementStaleRetry(oid)
			j.log.Debug("OrderTrades lookup failed", map[string]interface{}{
				"bitso_order_id": oid,
				"error":          err.Error(),
				"retry_count":    retries,
			})
			if retries >= maxStaleRetries {
				j.log.Info("Marking order as stale after max retries", map[string]interface{}{
					"bitso_order_id": oid,
					"retries":        retries,
				})
				if err := j.orderManager.MarkOrderStale(ctx, oid); err != nil {
					j.log.Warn("MarkOrderStale failed", map[string]interface{}{
						"bitso_order_id": oid,
						"error":          err.Error(),
					})
				}
				j.clearStaleRetry(oid)
			}
			continue
		}
		
		// Got trades, sync the order
		j.clearStaleRetry(oid)
		if err := j.orderManager.SyncOrderFromBitsoTrades(ctx, oid, trades); err != nil {
			j.log.Debug("SyncOrderFromBitsoTrades failed", map[string]interface{}{
				"bitso_order_id": oid,
				"error":          err.Error(),
			})
		}
	}
	
	if j.metrics != nil {
		j.metrics.SetBitsoSyncLastSuccessTimestamp(float64(time.Now().Unix()))
	}
}

func (j *BitsoSyncJob) incrementStaleRetry(oid string) int {
	j.staleOrderMu.Lock()
	defer j.staleOrderMu.Unlock()
	j.staleOrderRetries[oid]++
	return j.staleOrderRetries[oid]
}

func (j *BitsoSyncJob) clearStaleRetry(oid string) {
	j.staleOrderMu.Lock()
	defer j.staleOrderMu.Unlock()
	delete(j.staleOrderRetries, oid)
}

// refreshOpenOrdersGauge sets Prometheus orders_active{book} from GET /open_orders (Bitso source of truth).
func (j *BitsoSyncJob) refreshOpenOrdersGauge() {
	if j.metrics == nil {
		return
	}
	orders, err := j.bitsoClient.MyOpenOrders(nil)
	if err != nil {
		j.log.Warn("MyOpenOrders failed (orders_active gauge unchanged)", map[string]interface{}{"error": err.Error()})
		return
	}
	bookCounts := make(map[string]int)
	for i := range orders {
		book := orders[i].Book.String()
		bookCounts[book]++
	}
	j.activeBooksMu.Lock()
	for book := range j.lastBooksWithBitsoActive {
		if _, ok := bookCounts[book]; !ok {
			bookCounts[book] = 0
		}
	}
	j.lastBooksWithBitsoActive = make(map[string]struct{})
	for book, n := range bookCounts {
		j.metrics.SetActiveOrders(book, float64(n))
		if n > 0 {
			j.lastBooksWithBitsoActive[book] = struct{}{}
		}
	}
	j.activeBooksMu.Unlock()
}

func (j *BitsoSyncJob) applyBitsoUserOrder(ctx context.Context, uo *bitso.UserOrder) {
	bitsoOID := uo.OID
	original := (&uo.OriginalAmount).Float64()
	unfilled := (&uo.UnfilledAmount).Float64()
	filledAmount := original - unfilled
	price := (&uo.Price).Float64()
	avgPrice := price
	if filledAmount > 0 && original > 0 {
		avgPrice = price
	}
	status := bitsoOrderStatusToModel(uo.Status)
	if err := j.orderManager.SyncOrderFromBitso(ctx, bitsoOID, filledAmount, avgPrice, status); err != nil {
		j.log.Debug("SyncOrderFromBitso failed", map[string]interface{}{
			"bitso_order_id": bitsoOID,
			"error":          err.Error(),
		})
	}
}

func bitsoOrderStatusToModel(s bitso.OrderStatus) models.OrderStatus {
	switch s {
	case bitso.OrderStatusOpen, bitso.OrderStatusQueued:
		return models.OrderStatusAccepted
	case bitso.OrderStatusPartialFill:
		return models.OrderStatusPartiallyFilled
	case bitso.OrderStatusCompleted:
		return models.OrderStatusFilled
	case bitso.OrderStatusCancelled:
		return models.OrderStatusCancelled
	default:
		return models.OrderStatusAccepted
	}
}
