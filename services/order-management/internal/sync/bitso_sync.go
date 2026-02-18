package sync

import (
	"context"
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
}

// BitsoSyncJob polls Bitso for active orders and updates order-management (status, fills)
type BitsoSyncJob struct {
	bitsoClient  *bitso.Client
	orderManager OrderManagerSync
	log          *logger.Logger
	interval     time.Duration
	metrics      BitsoSyncMetrics // optional: for Prometheus
}

// OrderManagerSync is the subset of order-manager needed for sync
type OrderManagerSync interface {
	ListActiveBitsoOrderIDs(ctx context.Context) ([]string, error)
	SyncOrderFromBitso(ctx context.Context, bitsoOrderID string, filledAmount, avgPrice float64, status models.OrderStatus) error
}

// NewBitsoSyncJob creates a sync job that polls Bitso every interval. metrics is optional (Phase 2).
func NewBitsoSyncJob(bitsoClient *bitso.Client, orderManager OrderManagerSync, log *logger.Logger, interval time.Duration, metrics BitsoSyncMetrics) *BitsoSyncJob {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &BitsoSyncJob{
		bitsoClient:  bitsoClient,
		orderManager: orderManager,
		log:          log,
		interval:     interval,
		metrics:      metrics,
	}
}

// Run runs the sync loop until ctx is cancelled
func (j *BitsoSyncJob) Run(ctx context.Context) {
	j.log.Info("Bitso sync job started", map[string]interface{}{"interval": j.interval})
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
	// Bitso LookupOrders accepts comma-separated; API is /orders/oid1,oid2,...
	orders, err := j.bitsoClient.LookupOrders(oids)
	if err != nil {
		j.log.Warn("Bitso LookupOrders failed", map[string]interface{}{"error": err.Error()})
		if j.metrics != nil {
			j.metrics.RecordBitsoSyncError()
		}
		return
	}
	for _, uo := range orders {
		bitsoOID := uo.OID
		original := (&uo.OriginalAmount).Float64()
		unfilled := (&uo.UnfilledAmount).Float64()
		filledAmount := original - unfilled
		price := (&uo.Price).Float64()
		avgPrice := price
		if filledAmount > 0 && original > 0 {
			// approximate avg from price (Bitso doesn't give per-fill in UserOrder; use limit price)
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
	if j.metrics != nil {
		j.metrics.SetBitsoSyncLastSuccessTimestamp(float64(time.Now().Unix()))
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
