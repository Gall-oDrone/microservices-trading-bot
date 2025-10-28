package storage

import (
	"context"
	"time"

	"bitso-trading-platform/backtesting/internal/models"
)

// ResultStorage defines the interface for storing and retrieving backtest results
type ResultStorage interface {
	// Save saves a backtest result
	Save(ctx context.Context, result *models.BacktestResult) error

	// Get retrieves a backtest result by ID
	Get(ctx context.Context, backtestID string) (*models.BacktestResult, error)

	// List lists backtest results with optional filters
	List(ctx context.Context, filters *ListFilters) ([]*models.BacktestResult, error)

	// Delete deletes a backtest result
	Delete(ctx context.Context, backtestID string) error

	// UpdateStatus updates the status and progress of a backtest
	UpdateStatus(ctx context.Context, backtestID string, status string, progress float64) error

	// Close closes the storage and releases resources
	Close() error
}

// ListFilters defines filters for listing backtest results
type ListFilters struct {
	Status    string     `json:"status,omitempty"`     // Filter by status
	Strategy  string     `json:"strategy,omitempty"`   // Filter by strategy
	Book      string     `json:"book,omitempty"`       // Filter by book
	StartDate *time.Time `json:"start_date,omitempty"` // Filter by creation date (after)
	EndDate   *time.Time `json:"end_date,omitempty"`   // Filter by creation date (before)
	Limit     int        `json:"limit"`                // Maximum results (default: 20)
	Offset    int        `json:"offset"`               // Pagination offset (default: 0)
	SortBy    string     `json:"sort_by"`              // Sort field (default: created_at)
	SortOrder string     `json:"sort_order"`           // asc or desc (default: desc)
}

// NewListFilters creates a new list filters with defaults
func NewListFilters() *ListFilters {
	return &ListFilters{
		Limit:     20,
		Offset:    0,
		SortBy:    "created_at",
		SortOrder: "desc",
	}
}

// WithStatus sets the status filter
func (f *ListFilters) WithStatus(status string) *ListFilters {
	f.Status = status
	return f
}

// WithStrategy sets the strategy filter
func (f *ListFilters) WithStrategy(strategy string) *ListFilters {
	f.Strategy = strategy
	return f
}

// WithBook sets the book filter
func (f *ListFilters) WithBook(book string) *ListFilters {
	f.Book = book
	return f
}

// WithLimit sets the result limit
func (f *ListFilters) WithLimit(limit int) *ListFilters {
	f.Limit = limit
	return f
}

// WithOffset sets the pagination offset
func (f *ListFilters) WithOffset(offset int) *ListFilters {
	f.Offset = offset
	return f
}

// Validate validates the filters
func (f *ListFilters) Validate() error {
	if f.Limit < 0 {
		f.Limit = 20
	}
	if f.Limit > 1000 {
		f.Limit = 1000 // Max 1000 results
	}

	if f.Offset < 0 {
		f.Offset = 0
	}

	if f.SortOrder != "asc" && f.SortOrder != "desc" {
		f.SortOrder = "desc"
	}

	return nil
}
