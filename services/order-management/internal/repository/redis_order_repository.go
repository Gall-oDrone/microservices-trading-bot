package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
)

const keyPrefix = "om:"

// RedisOrderRepository implements OrderRepository using Redis.
type RedisOrderRepository struct {
	client  *redis.Client
	logger  *logger.Logger
	metrics *metrics.MetricsCollector
}

// NewRedisOrderRepository creates a new Redis order repository.
func NewRedisOrderRepository(client *redis.Client, logger *logger.Logger, metrics *metrics.MetricsCollector) *RedisOrderRepository {
	return &RedisOrderRepository{
		client:  client,
		logger:  logger,
		metrics: metrics,
	}
}

func orderKey(id string) string   { return keyPrefix + "order:" + id }
func orderIDsKey() string         { return keyPrefix + "order:ids" }
func signalIndexKey(s string) string { return keyPrefix + "order:signal:" + s }
func bookIndexKey(book string) string { return keyPrefix + "order:book:" + book }
func statusIndexKey(s models.OrderStatus) string { return keyPrefix + "order:status:" + string(s) }
func strategyIndexKey(s string) string { return keyPrefix + "order:strategy:" + s }
func bitsoIndexKey(bitsoOrderID string) string { return keyPrefix + "bitso:order:" + bitsoOrderID }

// Create creates a new order.
func (r *RedisOrderRepository) Create(ctx context.Context, order *models.Order) error {
	if order == nil {
		return fmt.Errorf("order cannot be nil")
	}
	if err := order.Validate(); err != nil {
		return fmt.Errorf("invalid order: %w", err)
	}

	data, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("marshal order: %w", err)
	}

	pipe := r.client.Pipeline()
	pipe.Set(ctx, orderKey(order.ID), data, 0)
	pipe.SAdd(ctx, orderIDsKey(), order.ID)
	if order.SignalID != "" {
		pipe.Set(ctx, signalIndexKey(order.SignalID), order.ID, 0)
	}
	pipe.SAdd(ctx, bookIndexKey(order.Book), order.ID)
	pipe.SAdd(ctx, statusIndexKey(order.Status), order.ID)
	if order.Strategy != "" {
		pipe.SAdd(ctx, strategyIndexKey(order.Strategy), order.ID)
	}
	if bid, ok := order.Metadata["bitso_order_id"].(string); ok && bid != "" {
		pipe.Set(ctx, bitsoIndexKey(bid), order.ID, 0)
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis create order: %w", err)
	}

	r.logger.Debug("Order created", map[string]interface{}{"order_id": order.ID, "book": order.Book, "status": order.Status})
	return nil
}

// Update updates an existing order.
func (r *RedisOrderRepository) Update(ctx context.Context, order *models.Order) error {
	if order == nil {
		return fmt.Errorf("order cannot be nil")
	}
	if err := order.Validate(); err != nil {
		return fmt.Errorf("invalid order: %w", err)
	}

	oldOrder, err := r.Get(ctx, order.ID)
	if err != nil {
		return err
	}

	data, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("marshal order: %w", err)
	}

	pipe := r.client.Pipeline()
	pipe.Set(ctx, orderKey(order.ID), data, 0)
	// Update indexes: remove old, add new
	r.pipelineRemoveOrderIndexes(pipe, ctx, oldOrder)
	r.pipelineAddOrderIndexes(pipe, ctx, order)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis update order: %w", err)
	}

	r.logger.Debug("Order updated", map[string]interface{}{"order_id": order.ID, "status": order.Status})
	return nil
}

func (r *RedisOrderRepository) pipelineAddOrderIndexes(pipe redis.Pipeliner, ctx context.Context, order *models.Order) {
	if order.SignalID != "" {
		pipe.Set(ctx, signalIndexKey(order.SignalID), order.ID, 0)
	}
	pipe.SAdd(ctx, bookIndexKey(order.Book), order.ID)
	pipe.SAdd(ctx, statusIndexKey(order.Status), order.ID)
	if order.Strategy != "" {
		pipe.SAdd(ctx, strategyIndexKey(order.Strategy), order.ID)
	}
	if bid, ok := order.Metadata["bitso_order_id"].(string); ok && bid != "" {
		pipe.Set(ctx, bitsoIndexKey(bid), order.ID, 0)
	}
}

func (r *RedisOrderRepository) pipelineRemoveOrderIndexes(pipe redis.Pipeliner, ctx context.Context, order *models.Order) {
	if order.SignalID != "" {
		pipe.Del(ctx, signalIndexKey(order.SignalID))
	}
	pipe.SRem(ctx, bookIndexKey(order.Book), order.ID)
	pipe.SRem(ctx, statusIndexKey(order.Status), order.ID)
	if order.Strategy != "" {
		pipe.SRem(ctx, strategyIndexKey(order.Strategy), order.ID)
	}
	if bid, ok := order.Metadata["bitso_order_id"].(string); ok && bid != "" {
		pipe.Del(ctx, bitsoIndexKey(bid))
	}
}

// Get retrieves an order by ID.
func (r *RedisOrderRepository) Get(ctx context.Context, orderID string) (*models.Order, error) {
	data, err := r.client.Get(ctx, orderKey(orderID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("order not found: %s", orderID)
		}
		return nil, fmt.Errorf("redis get: %w", err)
	}
	var order models.Order
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, fmt.Errorf("unmarshal order: %w", err)
	}
	// Ensure Metadata is non-nil after unmarshal
	if order.Metadata == nil {
		order.Metadata = make(map[string]interface{})
	}
	return order.Clone(), nil
}

// List retrieves orders with optional filters (loads all then applies filters in memory).
func (r *RedisOrderRepository) List(ctx context.Context, filters *models.OrderFilters) ([]*models.Order, error) {
	if filters == nil {
		filters = models.NewOrderFilters()
	}
	if err := filters.Validate(); err != nil {
		return nil, fmt.Errorf("invalid filters: %w", err)
	}

	ids, err := r.client.SMembers(ctx, orderIDsKey()).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers: %w", err)
	}
	orders := make([]*models.Order, 0, len(ids))
	for _, id := range ids {
		order, err := r.Get(ctx, id)
		if err != nil {
			continue // skip missing
		}
		orders = append(orders, order)
	}
	return filters.Apply(orders), nil
}

// Delete deletes an order.
func (r *RedisOrderRepository) Delete(ctx context.Context, orderID string) error {
	order, err := r.Get(ctx, orderID)
	if err != nil {
		return err
	}
	pipe := r.client.Pipeline()
	pipe.Del(ctx, orderKey(orderID))
	pipe.SRem(ctx, orderIDsKey(), orderID)
	r.pipelineRemoveOrderIndexes(pipe, ctx, order)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis delete order: %w", err)
	}
	r.logger.Debug("Order deleted", map[string]interface{}{"order_id": orderID})
	return nil
}

// GetBySignalID retrieves an order by signal ID.
func (r *RedisOrderRepository) GetBySignalID(ctx context.Context, signalID string) (*models.Order, error) {
	orderID, err := r.client.Get(ctx, signalIndexKey(signalID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("order not found for signal: %s", signalID)
		}
		return nil, fmt.Errorf("redis get signal: %w", err)
	}
	return r.Get(ctx, orderID)
}

// GetByBitsoOrderID retrieves an order by Bitso exchange order ID.
func (r *RedisOrderRepository) GetByBitsoOrderID(ctx context.Context, bitsoOrderID string) (*models.Order, error) {
	orderID, err := r.client.Get(ctx, bitsoIndexKey(bitsoOrderID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("order not found for bitso_order_id: %s", bitsoOrderID)
		}
		return nil, fmt.Errorf("redis get bitso: %w", err)
	}
	return r.Get(ctx, orderID)
}

// GetActiveOrders retrieves all active orders.
func (r *RedisOrderRepository) GetActiveOrders(ctx context.Context) ([]*models.Order, error) {
	statuses := []models.OrderStatus{
		models.OrderStatusPending, models.OrderStatusValidated, models.OrderStatusSubmitted,
		models.OrderStatusAccepted, models.OrderStatusPartiallyFilled,
	}
	var allIDs []string
	for _, st := range statuses {
		ids, err := r.client.SMembers(ctx, statusIndexKey(st)).Result()
		if err != nil {
			continue
		}
		allIDs = append(allIDs, ids...)
	}
	orders := make([]*models.Order, 0, len(allIDs))
	seen := make(map[string]bool)
	for _, id := range allIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		order, err := r.Get(ctx, id)
		if err != nil {
			continue
		}
		if order.IsActive() {
			orders = append(orders, order)
		}
	}
	return orders, nil
}

// GetOrdersByBook retrieves orders for a specific book.
func (r *RedisOrderRepository) GetOrdersByBook(ctx context.Context, book string) ([]*models.Order, error) {
	ids, err := r.client.SMembers(ctx, bookIndexKey(book)).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers book: %w", err)
	}
	return r.getOrdersByIDs(ctx, ids)
}

// GetOrdersByStatus retrieves orders with a specific status.
func (r *RedisOrderRepository) GetOrdersByStatus(ctx context.Context, status models.OrderStatus) ([]*models.Order, error) {
	ids, err := r.client.SMembers(ctx, statusIndexKey(status)).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers status: %w", err)
	}
	return r.getOrdersByIDs(ctx, ids)
}

// GetOrdersByStrategy retrieves orders for a specific strategy.
func (r *RedisOrderRepository) GetOrdersByStrategy(ctx context.Context, strategy string) ([]*models.Order, error) {
	ids, err := r.client.SMembers(ctx, strategyIndexKey(strategy)).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers strategy: %w", err)
	}
	return r.getOrdersByIDs(ctx, ids)
}

func (r *RedisOrderRepository) getOrdersByIDs(ctx context.Context, ids []string) ([]*models.Order, error) {
	orders := make([]*models.Order, 0, len(ids))
	for _, id := range ids {
		order, err := r.Get(ctx, id)
		if err != nil {
			continue
		}
		orders = append(orders, order)
	}
	return orders, nil
}

// Count returns the total number of orders matching the filters.
func (r *RedisOrderRepository) Count(ctx context.Context, filters *models.OrderFilters) (int, error) {
	orders, err := r.List(ctx, filters)
	if err != nil {
		return 0, err
	}
	return len(orders), nil
}

// Exists checks if an order exists.
func (r *RedisOrderRepository) Exists(ctx context.Context, orderID string) (bool, error) {
	n, err := r.client.Exists(ctx, orderKey(orderID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Close closes the repository and releases resources.
func (r *RedisOrderRepository) Close() error {
	// Client is typically shared; caller closes it. No-op here.
	r.logger.Info("Order repository closed", nil)
	return nil
}

// Ping verifies Redis connectivity (for health checks).
func (r *RedisOrderRepository) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return r.client.Ping(ctx).Err()
}
