package models

import (
	"fmt"
	"time"
)

// OrderFilters holds filters for querying orders
type OrderFilters struct {
	// Filter by specific fields
	Book     string      `json:"book,omitempty"`
	Status   OrderStatus `json:"status,omitempty"`
	Strategy string      `json:"strategy,omitempty"`
	Side     string      `json:"side,omitempty"`

	// Date range filters
	FromDate time.Time `json:"from_date,omitempty"`
	ToDate   time.Time `json:"to_date,omitempty"`

	// Pagination
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`

	// Sorting
	SortBy    string `json:"sort_by,omitempty"`    // "created_at", "updated_at", "amount", "price"
	SortOrder string `json:"sort_order,omitempty"` // "asc", "desc"
}

// NewOrderFilters creates a new OrderFilters with default values
func NewOrderFilters() *OrderFilters {
	return &OrderFilters{
		Limit:     100,
		Offset:    0,
		SortBy:    "created_at",
		SortOrder: "desc",
	}
}

// Apply filters orders based on the criteria
func (f *OrderFilters) Apply(orders []*Order) []*Order {
	if orders == nil {
		return nil
	}

	filtered := make([]*Order, 0)

	for _, order := range orders {
		if f.matches(order) {
			filtered = append(filtered, order)
		}
	}

	// Sort orders
	filtered = f.sort(filtered)

	// Apply pagination
	start := f.Offset
	if start > len(filtered) {
		return []*Order{}
	}

	end := start + f.Limit
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[start:end]
}

// matches checks if an order matches the filters
func (f *OrderFilters) matches(order *Order) bool {
	// Book filter
	if f.Book != "" && order.Book != f.Book {
		return false
	}

	// Status filter
	if f.Status != "" && order.Status != f.Status {
		return false
	}

	// Strategy filter
	if f.Strategy != "" && order.Strategy != f.Strategy {
		return false
	}

	// Side filter
	if f.Side != "" && order.Side != f.Side {
		return false
	}

	// Date range filter
	if !f.FromDate.IsZero() && order.CreatedAt.Before(f.FromDate) {
		return false
	}

	if !f.ToDate.IsZero() && order.CreatedAt.After(f.ToDate) {
		return false
	}

	return true
}

// sort sorts orders based on the sort criteria
func (f *OrderFilters) sort(orders []*Order) []*Order {
	if len(orders) == 0 {
		return orders
	}

	// Simple bubble sort for now - in production use sort.Slice
	for i := 0; i < len(orders)-1; i++ {
		for j := 0; j < len(orders)-i-1; j++ {
			if f.shouldSwap(orders[j], orders[j+1]) {
				orders[j], orders[j+1] = orders[j+1], orders[j]
			}
		}
	}

	return orders
}

// shouldSwap determines if two orders should be swapped based on sort criteria
func (f *OrderFilters) shouldSwap(a, b *Order) bool {
	ascending := f.SortOrder == "asc"

	switch f.SortBy {
	case "updated_at":
		if ascending {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		return a.UpdatedAt.Before(b.UpdatedAt)

	case "amount":
		if ascending {
			return a.Amount > b.Amount
		}
		return a.Amount < b.Amount

	case "price":
		if ascending {
			return a.Price > b.Price
		}
		return a.Price < b.Price

	default: // "created_at"
		if ascending {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.CreatedAt.Before(b.CreatedAt)
	}
}

// Validate validates the filter parameters
func (f *OrderFilters) Validate() error {
	if f.Limit < 0 {
		return fmt.Errorf("limit cannot be negative: %d", f.Limit)
	}

	if f.Limit > 1000 {
		return fmt.Errorf("limit too large (max 1000): %d", f.Limit)
	}

	if f.Offset < 0 {
		return fmt.Errorf("offset cannot be negative: %d", f.Offset)
	}

	if f.Side != "" && f.Side != "buy" && f.Side != "sell" {
		return fmt.Errorf("invalid side: %s", f.Side)
	}

	if f.SortOrder != "" && f.SortOrder != "asc" && f.SortOrder != "desc" {
		return fmt.Errorf("invalid sort order: %s (must be 'asc' or 'desc')", f.SortOrder)
	}

	if !f.FromDate.IsZero() && !f.ToDate.IsZero() && f.FromDate.After(f.ToDate) {
		return fmt.Errorf("from_date cannot be after to_date")
	}

	return nil
}

// ToQueryParams converts filters to query parameters
func (f *OrderFilters) ToQueryParams() map[string]string {
	params := make(map[string]string)

	if f.Book != "" {
		params["book"] = f.Book
	}

	if f.Status != "" {
		params["status"] = string(f.Status)
	}

	if f.Strategy != "" {
		params["strategy"] = f.Strategy
	}

	if f.Side != "" {
		params["side"] = f.Side
	}

	if !f.FromDate.IsZero() {
		params["from"] = f.FromDate.Format(time.RFC3339)
	}

	if !f.ToDate.IsZero() {
		params["to"] = f.ToDate.Format(time.RFC3339)
	}

	if f.Limit > 0 {
		params["limit"] = fmt.Sprintf("%d", f.Limit)
	}

	if f.Offset > 0 {
		params["offset"] = fmt.Sprintf("%d", f.Offset)
	}

	if f.SortBy != "" {
		params["sort_by"] = f.SortBy
	}

	if f.SortOrder != "" {
		params["sort_order"] = f.SortOrder
	}

	return params
}

// Clone creates a copy of the filters
func (f *OrderFilters) Clone() *OrderFilters {
	clone := *f
	return &clone
}

// PositionFilters holds filters for querying positions
type PositionFilters struct {
	// Filter by specific fields
	Book   string `json:"book,omitempty"`
	Status string `json:"status,omitempty"` // "open", "closed"

	// Pagination
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// NewPositionFilters creates a new PositionFilters with default values
func NewPositionFilters() *PositionFilters {
	return &PositionFilters{
		Limit:  100,
		Offset: 0,
	}
}

// Apply filters positions based on the criteria
func (f *PositionFilters) Apply(positions []*Position) []*Position {
	if positions == nil {
		return nil
	}

	filtered := make([]*Position, 0)

	for _, position := range positions {
		if f.matches(position) {
			filtered = append(filtered, position)
		}
	}

	// Apply pagination
	start := f.Offset
	if start > len(filtered) {
		return []*Position{}
	}

	end := start + f.Limit
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[start:end]
}

// matches checks if a position matches the filters
func (f *PositionFilters) matches(position *Position) bool {
	// Book filter
	if f.Book != "" && position.Book != f.Book {
		return false
	}

	// Status filter
	if f.Status == "open" && !position.IsOpen() {
		return false
	}

	if f.Status == "closed" && !position.IsClosed() {
		return false
	}

	return true
}

// Validate validates the filter parameters
func (f *PositionFilters) Validate() error {
	if f.Limit < 0 {
		return fmt.Errorf("limit cannot be negative: %d", f.Limit)
	}

	if f.Limit > 1000 {
		return fmt.Errorf("limit too large (max 1000): %d", f.Limit)
	}

	if f.Offset < 0 {
		return fmt.Errorf("offset cannot be negative: %d", f.Offset)
	}

	if f.Status != "" && f.Status != "open" && f.Status != "closed" {
		return fmt.Errorf("invalid status: %s (must be 'open' or 'closed')", f.Status)
	}

	return nil
}

// Clone creates a copy of the filters
func (f *PositionFilters) Clone() *PositionFilters {
	clone := *f
	return &clone
}
