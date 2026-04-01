package models

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// Position represents a trading position for a specific book
type Position struct {
	// Identification
	ID   string `json:"id"`
	Book string `json:"book"`

	// Position details
	Side string  `json:"side"` // "long", "short"
	Size float64 `json:"size"` // Position size in base currency

	// Pricing
	EntryPrice   float64 `json:"entry_price"`   // Average entry price
	CurrentPrice float64 `json:"current_price"` // Current market price

	// Profit & Loss
	UnrealizedPnL float64 `json:"unrealized_pnl"` // Unrealized profit/loss
	RealizedPnL   float64 `json:"realized_pnl"`   // Realized profit/loss

	// Order tracking
	OpenOrdersCount int      `json:"open_orders_count"` // Number of active orders
	OrderIDs        []string `json:"order_ids"`         // List of related order IDs

	// Timestamps
	OpenedAt  time.Time  `json:"opened_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`

	// Additional metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// NewPosition creates a new position
func NewPosition(book string, side string) *Position {
	now := time.Now()
	return &Position{
		ID:              generatePositionID(book),
		Book:            book,
		Side:            side,
		Size:            0,
		EntryPrice:      0,
		CurrentPrice:    0,
		UnrealizedPnL:   0,
		RealizedPnL:     0,
		OpenOrdersCount: 0,
		OrderIDs:        make([]string, 0),
		OpenedAt:        now,
		UpdatedAt:       now,
		Metadata:        make(map[string]interface{}),
	}
}

// IsOpen returns true if the position is open
func (p *Position) IsOpen() bool {
	return p.Size != 0 || p.OpenOrdersCount > 0
}

// IsClosed returns true if the position is closed
func (p *Position) IsClosed() bool {
	return p.Size == 0 && p.OpenOrdersCount == 0 && p.ClosedAt != nil
}

// UpdatePrice updates the current price and recalculates unrealized P&L
func (p *Position) UpdatePrice(price float64) {
	p.CurrentPrice = price
	p.UnrealizedPnL = p.CalculateUnrealizedPnL()
	p.UpdatedAt = time.Now()
}

// CalculateUnrealizedPnL calculates the unrealized profit/loss
func (p *Position) CalculateUnrealizedPnL() float64 {
	if p.Size == 0 || p.CurrentPrice == 0 || p.EntryPrice == 0 {
		return 0
	}

	priceDiff := p.CurrentPrice - p.EntryPrice
	if p.Side == "short" {
		priceDiff = -priceDiff
	}

	return priceDiff * p.Size
}

// CalculateTotalPnL returns the total profit/loss (realized + unrealized)
func (p *Position) CalculateTotalPnL() float64 {
	return p.RealizedPnL + p.UnrealizedPnL
}

// AddOrder adds an order to the position
func (p *Position) AddOrder(order *Order) {
	// Add order ID if not already present
	for _, id := range p.OrderIDs {
		if id == order.ID {
			return // Already tracked
		}
	}

	p.OrderIDs = append(p.OrderIDs, order.ID)

	// Update open orders count if order is active
	if order.IsActive() {
		p.OpenOrdersCount++
	}

	p.UpdatedAt = time.Now()
}

// RemoveOrder removes an order from the position
func (p *Position) RemoveOrder(orderID string) {
	// Remove order ID
	newOrderIDs := make([]string, 0, len(p.OrderIDs))
	for _, id := range p.OrderIDs {
		if id != orderID {
			newOrderIDs = append(newOrderIDs, id)
		}
	}
	p.OrderIDs = newOrderIDs

	// Decrement open orders count
	if p.OpenOrdersCount > 0 {
		p.OpenOrdersCount--
	}

	p.UpdatedAt = time.Now()
}

// ApplyFill applies a fill delta (major amount) at fillPrice (quote per major).
// Returns realized P&L in quote currency (e.g. MXN) contributed by this fill only.
func (p *Position) ApplyFill(order *Order, fillAmount, fillPrice float64) (realizedDelta float64) {
	if fillAmount <= 0 {
		return 0
	}

	if order.Side == "buy" {
		rem := fillAmount
		for rem > 0 {
			if p.Side == "short" && p.Size > 0 {
				cover := rem
				if cover > p.Size {
					cover = p.Size
				}
				pnl := (p.EntryPrice - fillPrice) * cover
				p.RealizedPnL += pnl
				realizedDelta += pnl
				p.Size -= cover
				rem -= cover
				if p.Size == 0 {
					p.Side = ""
					p.EntryPrice = 0
				}
				continue
			}
			if p.Side == "" {
				p.Side = "long"
			}
			if p.Side != "long" {
				break
			}
			prev := p.Size
			p.Size += rem
			if prev == 0 {
				p.EntryPrice = fillPrice
			} else {
				p.EntryPrice = (prev*p.EntryPrice + rem*fillPrice) / p.Size
			}
			rem = 0
		}
	} else if order.Side == "sell" {
		rem := fillAmount
		for rem > 0 {
			if p.Side == "long" && p.Size > 0 {
				close := math.Min(rem, p.Size)
				pnl := (fillPrice - p.EntryPrice) * close
				p.RealizedPnL += pnl
				realizedDelta += pnl
				p.Size -= close
				rem -= close
				if p.Size == 0 {
					p.Side = ""
					p.EntryPrice = 0
				}
				continue
			}
			if p.Side == "" {
				p.Side = "short"
			}
			if p.Side == "short" {
				prev := p.Size
				p.Size += rem
				if prev == 0 {
					p.EntryPrice = fillPrice
				} else {
					p.EntryPrice = (prev*p.EntryPrice + rem*fillPrice) / p.Size
				}
				rem = 0
				continue
			}
			p.Side = "short"
			p.Size = rem
			p.EntryPrice = fillPrice
			rem = 0
		}
	}

	if p.CurrentPrice > 0 {
		p.UnrealizedPnL = p.CalculateUnrealizedPnL()
	}
	p.UpdatedAt = time.Now()
	if p.Size == 0 && p.OpenOrdersCount == 0 {
		p.Close()
	}
	return realizedDelta
}

// UpdateFromFill updates the position when an order is filled (delegates to ApplyFill).
func (p *Position) UpdateFromFill(order *Order, fillAmount, fillPrice float64) {
	p.ApplyFill(order, fillAmount, fillPrice)
}

// Close closes the position
func (p *Position) Close() {
	now := time.Now()
	p.ClosedAt = &now
	p.Size = 0
	p.UnrealizedPnL = 0
	p.UpdatedAt = now
}

// Validate validates the position data
func (p *Position) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("position ID is required")
	}

	if p.Book == "" {
		return fmt.Errorf("book is required")
	}

	if p.Side != "" && p.Side != "long" && p.Side != "short" {
		return fmt.Errorf("invalid side: %s (must be 'long' or 'short')", p.Side)
	}

	if p.Size < 0 {
		return fmt.Errorf("size cannot be negative: %f", p.Size)
	}

	if p.EntryPrice < 0 {
		return fmt.Errorf("entry price cannot be negative: %f", p.EntryPrice)
	}

	if p.CurrentPrice < 0 {
		return fmt.Errorf("current price cannot be negative: %f", p.CurrentPrice)
	}

	if p.OpenOrdersCount < 0 {
		return fmt.Errorf("open orders count cannot be negative: %d", p.OpenOrdersCount)
	}

	return nil
}

// ToJSON serializes the position to JSON
func (p *Position) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}

// PositionFromJSON deserializes a position from JSON
func PositionFromJSON(data []byte) (*Position, error) {
	var position Position
	if err := json.Unmarshal(data, &position); err != nil {
		return nil, fmt.Errorf("failed to unmarshal position: %w", err)
	}
	return &position, nil
}

// Clone creates a deep copy of the position
func (p *Position) Clone() *Position {
	clone := *p

	// Deep copy closed timestamp
	if p.ClosedAt != nil {
		closed := *p.ClosedAt
		clone.ClosedAt = &closed
	}

	// Deep copy order IDs
	if p.OrderIDs != nil {
		clone.OrderIDs = make([]string, len(p.OrderIDs))
		copy(clone.OrderIDs, p.OrderIDs)
	}

	// Deep copy metadata
	if p.Metadata != nil {
		clone.Metadata = make(map[string]interface{})
		for k, v := range p.Metadata {
			clone.Metadata[k] = v
		}
	}

	return &clone
}

// String returns a string representation of the position
func (p *Position) String() string {
	return fmt.Sprintf("Position{ID:%s, Book:%s, Side:%s, Size:%f, EntryPrice:%f, UnrealizedPnL:%f, RealizedPnL:%f}",
		p.ID, p.Book, p.Side, p.Size, p.EntryPrice, p.UnrealizedPnL, p.RealizedPnL)
}

// PositionSummary provides a summary of all positions
type PositionSummary struct {
	TotalPositions     int                  `json:"total_positions"`
	OpenPositions      int                  `json:"open_positions"`
	ClosedPositions    int                  `json:"closed_positions"`
	TotalUnrealizedPnL float64              `json:"total_unrealized_pnl"`
	TotalRealizedPnL   float64              `json:"total_realized_pnl"`
	TotalPnL           float64              `json:"total_pnl"`
	PositionsByBook    map[string]*Position `json:"positions_by_book"`
}

// NewPositionSummary creates a new position summary from a list of positions
func NewPositionSummary(positions []*Position) *PositionSummary {
	summary := &PositionSummary{
		TotalPositions:     len(positions),
		OpenPositions:      0,
		ClosedPositions:    0,
		TotalUnrealizedPnL: 0,
		TotalRealizedPnL:   0,
		TotalPnL:           0,
		PositionsByBook:    make(map[string]*Position),
	}

	for _, pos := range positions {
		if pos.IsOpen() {
			summary.OpenPositions++
		} else {
			summary.ClosedPositions++
		}

		summary.TotalUnrealizedPnL += pos.UnrealizedPnL
		summary.TotalRealizedPnL += pos.RealizedPnL
		summary.PositionsByBook[pos.Book] = pos
	}

	summary.TotalPnL = summary.TotalUnrealizedPnL + summary.TotalRealizedPnL

	return summary
}

// Helper functions

// generatePositionID generates a unique position ID for a book
func generatePositionID(book string) string {
	return fmt.Sprintf("pos-%s-%d", book, time.Now().UnixNano())
}
