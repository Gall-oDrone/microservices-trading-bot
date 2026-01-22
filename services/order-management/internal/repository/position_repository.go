package repository

import (
	"context"
	"fmt"
	"sync"

	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
)

// InMemoryPositionRepository implements PositionRepository using in-memory storage
// NOTE: This is for development/testing. In production, use Redis implementation.
type InMemoryPositionRepository struct {
	logger  *logger.Logger
	metrics *metrics.MetricsCollector

	// In-memory storage
	positions map[string]*models.Position // book -> Position

	mu sync.RWMutex
}

// NewInMemoryPositionRepository creates a new in-memory position repository
func NewInMemoryPositionRepository(logger *logger.Logger, metrics *metrics.MetricsCollector) *InMemoryPositionRepository {
	return &InMemoryPositionRepository{
		logger:    logger,
		metrics:   metrics,
		positions: make(map[string]*models.Position),
	}
}

// Create creates a new position
func (r *InMemoryPositionRepository) Create(ctx context.Context, position *models.Position) error {
	if position == nil {
		return fmt.Errorf("position cannot be nil")
	}

	if err := position.Validate(); err != nil {
		return fmt.Errorf("invalid position: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if position already exists
	if _, exists := r.positions[position.Book]; exists {
		return fmt.Errorf("position already exists for book: %s", position.Book)
	}

	// Store position
	r.positions[position.Book] = position.Clone()

	r.logger.Debug("Position created", map[string]interface{}{
		"book": position.Book,
		"side": position.Side,
		"size": position.Size,
	})

	return nil
}

// Update updates an existing position
func (r *InMemoryPositionRepository) Update(ctx context.Context, position *models.Position) error {
	if position == nil {
		return fmt.Errorf("position cannot be nil")
	}

	if err := position.Validate(); err != nil {
		return fmt.Errorf("invalid position: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if position exists
	if _, exists := r.positions[position.Book]; !exists {
		return fmt.Errorf("position not found for book: %s", position.Book)
	}

	// Update position
	r.positions[position.Book] = position.Clone()

	r.logger.Debug("Position updated", map[string]interface{}{
		"book": position.Book,
		"size": position.Size,
	})

	return nil
}

// Get retrieves a position by book
func (r *InMemoryPositionRepository) Get(ctx context.Context, book string) (*models.Position, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	position, exists := r.positions[book]
	if !exists {
		return nil, fmt.Errorf("position not found for book: %s", book)
	}

	return position.Clone(), nil
}

// GetAll retrieves all positions
func (r *InMemoryPositionRepository) GetAll(ctx context.Context) ([]*models.Position, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	positions := make([]*models.Position, 0, len(r.positions))
	for _, position := range r.positions {
		positions = append(positions, position.Clone())
	}

	return positions, nil
}

// List retrieves positions with optional filters
func (r *InMemoryPositionRepository) List(ctx context.Context, filters *models.PositionFilters) ([]*models.Position, error) {
	if filters == nil {
		filters = models.NewPositionFilters()
	}

	if err := filters.Validate(); err != nil {
		return nil, fmt.Errorf("invalid filters: %w", err)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Get all positions
	positions := make([]*models.Position, 0, len(r.positions))
	for _, position := range r.positions {
		positions = append(positions, position.Clone())
	}

	// Apply filters
	filtered := filters.Apply(positions)

	return filtered, nil
}

// Delete deletes a position
func (r *InMemoryPositionRepository) Delete(ctx context.Context, book string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.positions[book]; !exists {
		return fmt.Errorf("position not found for book: %s", book)
	}

	// Delete position
	delete(r.positions, book)

	r.logger.Debug("Position deleted", map[string]interface{}{
		"book": book,
	})

	return nil
}

// GetOpenPositions retrieves all open positions
func (r *InMemoryPositionRepository) GetOpenPositions(ctx context.Context) ([]*models.Position, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	openPositions := make([]*models.Position, 0)

	for _, position := range r.positions {
		if position.IsOpen() {
			openPositions = append(openPositions, position.Clone())
		}
	}

	return openPositions, nil
}

// GetSummary retrieves a summary of all positions
func (r *InMemoryPositionRepository) GetSummary(ctx context.Context) (*models.PositionSummary, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	positions := make([]*models.Position, 0, len(r.positions))
	for _, position := range r.positions {
		positions = append(positions, position.Clone())
	}

	summary := models.NewPositionSummary(positions)

	return summary, nil
}

// Exists checks if a position exists
func (r *InMemoryPositionRepository) Exists(ctx context.Context, book string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.positions[book]
	return exists, nil
}

// Close closes the repository and releases resources
func (r *InMemoryPositionRepository) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear all data
	r.positions = make(map[string]*models.Position)

	r.logger.Info("Position repository closed", nil)
	return nil
}
