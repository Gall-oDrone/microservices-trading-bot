package repository

import (
	"context"

	"bitso-trading-platform/order-management/internal/models"
)

// OrderRepository defines the interface for order persistence operations
type OrderRepository interface {
	// Create creates a new order
	Create(ctx context.Context, order *models.Order) error

	// Update updates an existing order
	Update(ctx context.Context, order *models.Order) error

	// Get retrieves an order by ID
	Get(ctx context.Context, orderID string) (*models.Order, error)

	// List retrieves orders with optional filters
	List(ctx context.Context, filters *models.OrderFilters) ([]*models.Order, error)

	// Delete deletes an order
	Delete(ctx context.Context, orderID string) error

	// GetBySignalID retrieves an order by signal ID
	GetBySignalID(ctx context.Context, signalID string) (*models.Order, error)

	// GetByBitsoOrderID retrieves an order by Bitso exchange order ID (stored in Metadata["bitso_order_id"])
	GetByBitsoOrderID(ctx context.Context, bitsoOrderID string) (*models.Order, error)

	// GetActiveOrders retrieves all active orders
	GetActiveOrders(ctx context.Context) ([]*models.Order, error)

	// GetOrdersByBook retrieves orders for a specific book
	GetOrdersByBook(ctx context.Context, book string) ([]*models.Order, error)

	// GetOrdersByStatus retrieves orders with a specific status
	GetOrdersByStatus(ctx context.Context, status models.OrderStatus) ([]*models.Order, error)

	// GetOrdersByStrategy retrieves orders for a specific strategy
	GetOrdersByStrategy(ctx context.Context, strategy string) ([]*models.Order, error)

	// Count returns the total number of orders matching the filters
	Count(ctx context.Context, filters *models.OrderFilters) (int, error)

	// Exists checks if an order exists
	Exists(ctx context.Context, orderID string) (bool, error)

	// Close closes the repository and releases resources
	Close() error
}

// PositionRepository defines the interface for position persistence operations
type PositionRepository interface {
	// Create creates a new position
	Create(ctx context.Context, position *models.Position) error

	// Update updates an existing position
	Update(ctx context.Context, position *models.Position) error

	// Get retrieves a position by book
	Get(ctx context.Context, book string) (*models.Position, error)

	// GetAll retrieves all positions
	GetAll(ctx context.Context) ([]*models.Position, error)

	// List retrieves positions with optional filters
	List(ctx context.Context, filters *models.PositionFilters) ([]*models.Position, error)

	// Delete deletes a position
	Delete(ctx context.Context, book string) error

	// GetOpenPositions retrieves all open positions
	GetOpenPositions(ctx context.Context) ([]*models.Position, error)

	// GetSummary retrieves a summary of all positions
	GetSummary(ctx context.Context) (*models.PositionSummary, error)

	// Exists checks if a position exists
	Exists(ctx context.Context, book string) (bool, error)

	// Close closes the repository and releases resources
	Close() error
}
