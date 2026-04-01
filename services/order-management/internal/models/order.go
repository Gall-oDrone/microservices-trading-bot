package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// OrderStatus represents the status of an order
type OrderStatus string

const (
	// OrderStatusPending - Initial state after receiving signal
	OrderStatusPending OrderStatus = "pending"
	// OrderStatusValidated - Passed pre-trade validation
	OrderStatusValidated OrderStatus = "validated"
	// OrderStatusSubmitted - Sent to trading-engine
	OrderStatusSubmitted OrderStatus = "submitted"
	// OrderStatusAccepted - Acknowledged by exchange
	OrderStatusAccepted OrderStatus = "accepted"
	// OrderStatusPartiallyFilled - Order partially executed
	OrderStatusPartiallyFilled OrderStatus = "partially_filled"
	// OrderStatusFilled - Order completely executed
	OrderStatusFilled OrderStatus = "filled"
	// OrderStatusCancelled - Order cancelled by user/system
	OrderStatusCancelled OrderStatus = "cancelled"
	// OrderStatusRejected - Order rejected by validation/exchange
	OrderStatusRejected OrderStatus = "rejected"
)

// Order represents a trading order in the system
type Order struct {
	// Identification
	ID            string `json:"id"`
	ClientOrderID string `json:"client_order_id"`
	SignalID      string `json:"signal_id"`

	// Order details
	Book   string      `json:"book"`
	Side   string      `json:"side"` // "buy", "sell"
	Type   string      `json:"type"` // "market", "limit"
	Status OrderStatus `json:"status"`

	// Pricing
	Price           float64 `json:"price"`
	Amount          float64 `json:"amount"`
	FilledAmount    float64 `json:"filled_amount"`
	RemainingAmount float64 `json:"remaining_amount"`
	AveragePrice    float64 `json:"average_price"`

	// Metadata
	Strategy string `json:"strategy"`

	// Timestamps
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
	FilledAt    *time.Time `json:"filled_at,omitempty"`
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`

	// Rejection
	RejectionReason string `json:"rejection_reason,omitempty"`

	// Additional metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// NewOrder creates a new order with default values
func NewOrder(signalID, book, side, orderType, strategy string, price, amount float64) *Order {
	now := time.Now()
	return &Order{
		ID:              generateOrderID(),
		ClientOrderID:   generateOrderID(),
		SignalID:        signalID,
		Book:            book,
		Side:            side,
		Type:            orderType,
		Status:          OrderStatusPending,
		Price:           price,
		Amount:          amount,
		FilledAmount:    0,
		RemainingAmount: amount,
		AveragePrice:    0,
		Strategy:        strategy,
		CreatedAt:       now,
		UpdatedAt:       now,
		Metadata:        make(map[string]interface{}),
	}
}

// IsActive returns true if the order is in an active state
func (o *Order) IsActive() bool {
	return o.Status == OrderStatusPending ||
		o.Status == OrderStatusValidated ||
		o.Status == OrderStatusSubmitted ||
		o.Status == OrderStatusAccepted ||
		o.Status == OrderStatusPartiallyFilled
}

// IsClosed returns true if the order is in a closed state
func (o *Order) IsClosed() bool {
	return o.Status == OrderStatusFilled ||
		o.Status == OrderStatusCancelled ||
		o.Status == OrderStatusRejected
}

// IsFilled returns true if the order is completely filled
func (o *Order) IsFilled() bool {
	return o.Status == OrderStatusFilled
}

// IsPartiallyFilled returns true if the order is partially filled
func (o *Order) IsPartiallyFilled() bool {
	return o.Status == OrderStatusPartiallyFilled
}

// UpdateStatus updates the order status and timestamp
func (o *Order) UpdateStatus(status OrderStatus) {
	o.Status = status
	o.UpdatedAt = time.Now()

	// Update specific timestamps based on status
	now := time.Now()
	switch status {
	case OrderStatusSubmitted:
		if o.SubmittedAt == nil {
			o.SubmittedAt = &now
		}
	case OrderStatusFilled:
		if o.FilledAt == nil {
			o.FilledAt = &now
		}
	case OrderStatusCancelled:
		if o.CancelledAt == nil {
			o.CancelledAt = &now
		}
	}
}

// RecordFill records a fill for the order
func (o *Order) RecordFill(filledAmount, fillPrice float64) {
	// Update filled amount
	previousFilledAmount := o.FilledAmount
	o.FilledAmount += filledAmount
	o.RemainingAmount = o.Amount - o.FilledAmount

	// Update average price
	totalValue := (previousFilledAmount * o.AveragePrice) + (filledAmount * fillPrice)
	o.AveragePrice = totalValue / o.FilledAmount

	// Update status based on fill
	if o.RemainingAmount <= 0.0000001 { // Account for floating point precision
		o.UpdateStatus(OrderStatusFilled)
	} else if o.FilledAmount > 0 {
		o.UpdateStatus(OrderStatusPartiallyFilled)
	}

	o.UpdatedAt = time.Now()
}

// AccumulateFill updates filled quantity and average price from a fill delta without changing status.
// Used when syncing from the exchange so Bitso-reported status is applied only via the state machine.
func (o *Order) AccumulateFill(fillDelta, fillPrice float64) {
	if fillDelta <= 0 {
		return
	}
	previousFilledAmount := o.FilledAmount
	o.FilledAmount += fillDelta
	o.RemainingAmount = o.Amount - o.FilledAmount
	if o.RemainingAmount < 0 && o.RemainingAmount > -1e-6 {
		o.RemainingAmount = 0
	}
	totalValue := (previousFilledAmount * o.AveragePrice) + (fillDelta * fillPrice)
	if o.FilledAmount > 0 {
		o.AveragePrice = totalValue / o.FilledAmount
	}
	o.UpdatedAt = time.Now()
}

// GetFillPercentage returns the fill percentage (0-100)
func (o *Order) GetFillPercentage() float64 {
	if o.Amount == 0 {
		return 0
	}
	return (o.FilledAmount / o.Amount) * 100
}

// Reject marks the order as rejected with a reason
func (o *Order) Reject(reason string) {
	o.Status = OrderStatusRejected
	o.RejectionReason = reason
	o.UpdatedAt = time.Now()
}

// Cancel marks the order as cancelled
func (o *Order) Cancel() {
	now := time.Now()
	o.Status = OrderStatusCancelled
	o.CancelledAt = &now
	o.UpdatedAt = now
}

// Validate validates the order data
func (o *Order) Validate() error {
	if o.ID == "" {
		return fmt.Errorf("order ID is required")
	}

	if o.Book == "" {
		return fmt.Errorf("book is required")
	}

	if o.Side != "buy" && o.Side != "sell" {
		return fmt.Errorf("invalid side: %s (must be 'buy' or 'sell')", o.Side)
	}

	if o.Type != "market" && o.Type != "limit" {
		return fmt.Errorf("invalid type: %s (must be 'market' or 'limit')", o.Type)
	}

	if o.Amount <= 0 {
		return fmt.Errorf("amount must be positive: %f", o.Amount)
	}

	if o.Type == "limit" && o.Price <= 0 {
		return fmt.Errorf("price must be positive for limit orders: %f", o.Price)
	}

	if o.FilledAmount < 0 {
		return fmt.Errorf("filled amount cannot be negative: %f", o.FilledAmount)
	}

	if o.FilledAmount > o.Amount {
		return fmt.Errorf("filled amount cannot exceed total amount: %f > %f", o.FilledAmount, o.Amount)
	}

	return nil
}

// ToJSON serializes the order to JSON
func (o *Order) ToJSON() ([]byte, error) {
	return json.Marshal(o)
}

// OrderFromJSON deserializes an order from JSON
func OrderFromJSON(data []byte) (*Order, error) {
	var order Order
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, fmt.Errorf("failed to unmarshal order: %w", err)
	}
	return &order, nil
}

// Clone creates a deep copy of the order
func (o *Order) Clone() *Order {
	clone := *o

	// Deep copy timestamps
	if o.SubmittedAt != nil {
		submitted := *o.SubmittedAt
		clone.SubmittedAt = &submitted
	}
	if o.FilledAt != nil {
		filled := *o.FilledAt
		clone.FilledAt = &filled
	}
	if o.CancelledAt != nil {
		cancelled := *o.CancelledAt
		clone.CancelledAt = &cancelled
	}

	// Deep copy metadata
	if o.Metadata != nil {
		clone.Metadata = make(map[string]interface{})
		for k, v := range o.Metadata {
			clone.Metadata[k] = v
		}
	}

	return &clone
}

// String returns a string representation of the order
func (o *Order) String() string {
	return fmt.Sprintf("Order{ID:%s, Book:%s, Side:%s, Type:%s, Status:%s, Amount:%f, Filled:%f}",
		o.ID, o.Book, o.Side, o.Type, o.Status, o.Amount, o.FilledAmount)
}

// OrderEvent represents an event in the order lifecycle
type OrderEvent struct {
	EventID   string                 `json:"event_id"`
	EventType string                 `json:"event_type"` // "created", "updated", "filled", "cancelled", "rejected"
	OrderID   string                 `json:"order_id"`
	Order     *Order                 `json:"order"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// NewOrderEvent creates a new order event
func NewOrderEvent(eventType string, order *Order) *OrderEvent {
	return &OrderEvent{
		EventID:   generateEventID(),
		EventType: eventType,
		OrderID:   order.ID,
		Order:     order.Clone(),
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
}

// Helper functions

// generateOrderID generates a unique order ID
func generateOrderID() string {
	// Simple implementation - in production, use UUID
	return fmt.Sprintf("ord-%d", time.Now().UnixNano())
}

// generateEventID generates a unique event ID
func generateEventID() string {
	// Simple implementation - in production, use UUID
	return fmt.Sprintf("evt-%d", time.Now().UnixNano())
}
