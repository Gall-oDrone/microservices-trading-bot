package repository

import (
	"context"
	"fmt"
	"sync"

	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
)

// InMemoryOrderRepository implements OrderRepository using in-memory storage
// NOTE: This is for development/testing. In production, use Redis implementation.
type InMemoryOrderRepository struct {
	logger  *logger.Logger
	metrics *metrics.MetricsCollector

	// In-memory storage
	orders        map[string]*models.Order        // orderID -> Order
	signalIndex   map[string]string               // signalID -> orderID
	bookIndex     map[string][]string             // book -> []orderID
	statusIndex   map[models.OrderStatus][]string // status -> []orderID
	strategyIndex map[string][]string             // strategy -> []orderID

	mu sync.RWMutex
}

// NewInMemoryOrderRepository creates a new in-memory order repository
func NewInMemoryOrderRepository(logger *logger.Logger, metrics *metrics.MetricsCollector) *InMemoryOrderRepository {
	return &InMemoryOrderRepository{
		logger:        logger,
		metrics:       metrics,
		orders:        make(map[string]*models.Order),
		signalIndex:   make(map[string]string),
		bookIndex:     make(map[string][]string),
		statusIndex:   make(map[models.OrderStatus][]string),
		strategyIndex: make(map[string][]string),
	}
}

// Create creates a new order
func (r *InMemoryOrderRepository) Create(ctx context.Context, order *models.Order) error {
	if order == nil {
		return fmt.Errorf("order cannot be nil")
	}

	if err := order.Validate(); err != nil {
		return fmt.Errorf("invalid order: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if order already exists
	if _, exists := r.orders[order.ID]; exists {
		return fmt.Errorf("order already exists: %s", order.ID)
	}

	// Store order
	r.orders[order.ID] = order.Clone()

	// Update indexes
	r.addToIndexes(order)

	r.logger.Debug("Order created", map[string]interface{}{
		"order_id": order.ID,
		"book":     order.Book,
		"status":   order.Status,
	})

	return nil
}

// Update updates an existing order
func (r *InMemoryOrderRepository) Update(ctx context.Context, order *models.Order) error {
	if order == nil {
		return fmt.Errorf("order cannot be nil")
	}

	if err := order.Validate(); err != nil {
		return fmt.Errorf("invalid order: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if order exists
	oldOrder, exists := r.orders[order.ID]
	if !exists {
		return fmt.Errorf("order not found: %s", order.ID)
	}

	// Remove old indexes
	r.removeFromIndexes(oldOrder)

	// Update order
	r.orders[order.ID] = order.Clone()

	// Update indexes
	r.addToIndexes(order)

	r.logger.Debug("Order updated", map[string]interface{}{
		"order_id": order.ID,
		"status":   order.Status,
	})

	return nil
}

// Get retrieves an order by ID
func (r *InMemoryOrderRepository) Get(ctx context.Context, orderID string) (*models.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	order, exists := r.orders[orderID]
	if !exists {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}

	return order.Clone(), nil
}

// List retrieves orders with optional filters
func (r *InMemoryOrderRepository) List(ctx context.Context, filters *models.OrderFilters) ([]*models.Order, error) {
	if filters == nil {
		filters = models.NewOrderFilters()
	}

	if err := filters.Validate(); err != nil {
		return nil, fmt.Errorf("invalid filters: %w", err)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Get all orders
	orders := make([]*models.Order, 0, len(r.orders))
	for _, order := range r.orders {
		orders = append(orders, order.Clone())
	}

	// Apply filters
	filtered := filters.Apply(orders)

	return filtered, nil
}

// Delete deletes an order
func (r *InMemoryOrderRepository) Delete(ctx context.Context, orderID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	order, exists := r.orders[orderID]
	if !exists {
		return fmt.Errorf("order not found: %s", orderID)
	}

	// Remove from indexes
	r.removeFromIndexes(order)

	// Delete order
	delete(r.orders, orderID)

	r.logger.Debug("Order deleted", map[string]interface{}{
		"order_id": orderID,
	})

	return nil
}

// GetBySignalID retrieves an order by signal ID
func (r *InMemoryOrderRepository) GetBySignalID(ctx context.Context, signalID string) (*models.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	orderID, exists := r.signalIndex[signalID]
	if !exists {
		return nil, fmt.Errorf("order not found for signal: %s", signalID)
	}

	order, exists := r.orders[orderID]
	if !exists {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}

	return order.Clone(), nil
}

// GetByBitsoOrderID retrieves an order by Bitso exchange order ID (Metadata["bitso_order_id"])
func (r *InMemoryOrderRepository) GetByBitsoOrderID(ctx context.Context, bitsoOrderID string) (*models.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, order := range r.orders {
		if id, ok := order.Metadata["bitso_order_id"].(string); ok && id == bitsoOrderID {
			return order.Clone(), nil
		}
	}
	return nil, fmt.Errorf("order not found for bitso_order_id: %s", bitsoOrderID)
}

// GetActiveOrders retrieves all active orders
func (r *InMemoryOrderRepository) GetActiveOrders(ctx context.Context) ([]*models.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	activeOrders := make([]*models.Order, 0)

	for _, order := range r.orders {
		if order.IsActive() {
			activeOrders = append(activeOrders, order.Clone())
		}
	}

	return activeOrders, nil
}

// GetOrdersByBook retrieves orders for a specific book
func (r *InMemoryOrderRepository) GetOrdersByBook(ctx context.Context, book string) ([]*models.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	orderIDs, exists := r.bookIndex[book]
	if !exists {
		return []*models.Order{}, nil
	}

	orders := make([]*models.Order, 0, len(orderIDs))
	for _, orderID := range orderIDs {
		if order, exists := r.orders[orderID]; exists {
			orders = append(orders, order.Clone())
		}
	}

	return orders, nil
}

// GetOrdersByStatus retrieves orders with a specific status
func (r *InMemoryOrderRepository) GetOrdersByStatus(ctx context.Context, status models.OrderStatus) ([]*models.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	orderIDs, exists := r.statusIndex[status]
	if !exists {
		return []*models.Order{}, nil
	}

	orders := make([]*models.Order, 0, len(orderIDs))
	for _, orderID := range orderIDs {
		if order, exists := r.orders[orderID]; exists {
			orders = append(orders, order.Clone())
		}
	}

	return orders, nil
}

// GetOrdersByStrategy retrieves orders for a specific strategy
func (r *InMemoryOrderRepository) GetOrdersByStrategy(ctx context.Context, strategy string) ([]*models.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	orderIDs, exists := r.strategyIndex[strategy]
	if !exists {
		return []*models.Order{}, nil
	}

	orders := make([]*models.Order, 0, len(orderIDs))
	for _, orderID := range orderIDs {
		if order, exists := r.orders[orderID]; exists {
			orders = append(orders, order.Clone())
		}
	}

	return orders, nil
}

// Count returns the total number of orders matching the filters
func (r *InMemoryOrderRepository) Count(ctx context.Context, filters *models.OrderFilters) (int, error) {
	orders, err := r.List(ctx, filters)
	if err != nil {
		return 0, err
	}
	return len(orders), nil
}

// Exists checks if an order exists
func (r *InMemoryOrderRepository) Exists(ctx context.Context, orderID string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.orders[orderID]
	return exists, nil
}

// Close closes the repository and releases resources
func (r *InMemoryOrderRepository) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear all data
	r.orders = make(map[string]*models.Order)
	r.signalIndex = make(map[string]string)
	r.bookIndex = make(map[string][]string)
	r.statusIndex = make(map[models.OrderStatus][]string)
	r.strategyIndex = make(map[string][]string)

	r.logger.Info("Order repository closed", nil)
	return nil
}

// Helper methods for index management

func (r *InMemoryOrderRepository) addToIndexes(order *models.Order) {
	// Signal index
	if order.SignalID != "" {
		r.signalIndex[order.SignalID] = order.ID
	}

	// Book index
	if order.Book != "" {
		r.bookIndex[order.Book] = append(r.bookIndex[order.Book], order.ID)
	}

	// Status index
	r.statusIndex[order.Status] = append(r.statusIndex[order.Status], order.ID)

	// Strategy index
	if order.Strategy != "" {
		r.strategyIndex[order.Strategy] = append(r.strategyIndex[order.Strategy], order.ID)
	}
}

func (r *InMemoryOrderRepository) removeFromIndexes(order *models.Order) {
	// Signal index
	if order.SignalID != "" {
		delete(r.signalIndex, order.SignalID)
	}

	// Book index
	if order.Book != "" {
		r.bookIndex[order.Book] = removeFromSlice(r.bookIndex[order.Book], order.ID)
	}

	// Status index
	r.statusIndex[order.Status] = removeFromSlice(r.statusIndex[order.Status], order.ID)

	// Strategy index
	if order.Strategy != "" {
		r.strategyIndex[order.Strategy] = removeFromSlice(r.strategyIndex[order.Strategy], order.ID)
	}
}

func removeFromSlice(slice []string, value string) []string {
	result := make([]string, 0, len(slice))
	for _, v := range slice {
		if v != value {
			result = append(result, v)
		}
	}
	return result
}
