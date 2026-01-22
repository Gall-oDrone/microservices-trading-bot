package manager

import (
	"fmt"

	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/models"
)

// StateMachine manages order state transitions
type StateMachine struct {
	logger             *logger.Logger
	allowedTransitions map[models.OrderStatus][]models.OrderStatus
}

// NewStateMachine creates a new state machine
func NewStateMachine(logger *logger.Logger) *StateMachine {
	sm := &StateMachine{
		logger:             logger,
		allowedTransitions: make(map[models.OrderStatus][]models.OrderStatus),
	}

	sm.initializeTransitions()
	return sm
}

// initializeTransitions sets up allowed state transitions
func (sm *StateMachine) initializeTransitions() {
	sm.allowedTransitions = map[models.OrderStatus][]models.OrderStatus{
		models.OrderStatusPending: {
			models.OrderStatusValidated,
			models.OrderStatusRejected,
		},
		models.OrderStatusValidated: {
			models.OrderStatusSubmitted,
			models.OrderStatusRejected,
		},
		models.OrderStatusSubmitted: {
			models.OrderStatusAccepted,
			models.OrderStatusRejected,
		},
		models.OrderStatusAccepted: {
			models.OrderStatusPartiallyFilled,
			models.OrderStatusFilled,
			models.OrderStatusCancelled,
		},
		models.OrderStatusPartiallyFilled: {
			models.OrderStatusFilled,
			models.OrderStatusCancelled,
		},
		// Final states have no allowed transitions
		models.OrderStatusFilled:    {},
		models.OrderStatusCancelled: {},
		models.OrderStatusRejected:  {},
	}
}

// CanTransition checks if a transition from one status to another is allowed
func (sm *StateMachine) CanTransition(from, to models.OrderStatus) bool {
	allowedStates, exists := sm.allowedTransitions[from]
	if !exists {
		return false
	}

	for _, allowed := range allowedStates {
		if allowed == to {
			return true
		}
	}

	return false
}

// ValidateTransition validates if a transition is allowed
func (sm *StateMachine) ValidateTransition(from, to models.OrderStatus) error {
	if from == to {
		return fmt.Errorf("cannot transition to same state: %s", from)
	}

	if !sm.CanTransition(from, to) {
		return fmt.Errorf("invalid transition from %s to %s", from, to)
	}

	return nil
}

// Transition executes a state transition
func (sm *StateMachine) Transition(order *models.Order, to models.OrderStatus) error {
	from := order.Status

	// Validate transition
	if err := sm.ValidateTransition(from, to); err != nil {
		sm.logger.Warn("Invalid state transition attempted", map[string]interface{}{
			"order_id": order.ID,
			"from":     from,
			"to":       to,
			"error":    err.Error(),
		})
		return err
	}

	// Execute transition
	order.UpdateStatus(to)

	sm.logger.Debug("State transition successful", map[string]interface{}{
		"order_id": order.ID,
		"from":     from,
		"to":       to,
	})

	return nil
}

// GetAllowedTransitions returns all allowed transitions from a given status
func (sm *StateMachine) GetAllowedTransitions(status models.OrderStatus) []models.OrderStatus {
	allowed, exists := sm.allowedTransitions[status]
	if !exists {
		return []models.OrderStatus{}
	}

	// Return a copy to prevent external modification
	result := make([]models.OrderStatus, len(allowed))
	copy(result, allowed)
	return result
}

// IsFinalState returns true if the status is a final state
func (sm *StateMachine) IsFinalState(status models.OrderStatus) bool {
	finalStates := map[models.OrderStatus]bool{
		models.OrderStatusFilled:    true,
		models.OrderStatusCancelled: true,
		models.OrderStatusRejected:  true,
	}
	return finalStates[status]
}

// GetStateDescription returns a human-readable description of a state
func (sm *StateMachine) GetStateDescription(status models.OrderStatus) string {
	descriptions := map[models.OrderStatus]string{
		models.OrderStatusPending:         "Order created, awaiting validation",
		models.OrderStatusValidated:       "Order validated, ready for submission",
		models.OrderStatusSubmitted:       "Order submitted to trading engine",
		models.OrderStatusAccepted:        "Order accepted by exchange",
		models.OrderStatusPartiallyFilled: "Order partially filled",
		models.OrderStatusFilled:          "Order completely filled",
		models.OrderStatusCancelled:       "Order cancelled",
		models.OrderStatusRejected:        "Order rejected",
	}

	if desc, exists := descriptions[status]; exists {
		return desc
	}

	return "Unknown status"
}

// GetNextStates returns possible next states from current status
func (sm *StateMachine) GetNextStates(status models.OrderStatus) []models.OrderStatus {
	return sm.GetAllowedTransitions(status)
}

// CanCancel returns true if an order can be cancelled from its current status
func (sm *StateMachine) CanCancel(status models.OrderStatus) bool {
	// Can cancel from accepted or partially filled states
	cancelableStates := map[models.OrderStatus]bool{
		models.OrderStatusAccepted:        true,
		models.OrderStatusPartiallyFilled: true,
	}
	return cancelableStates[status]
}

// CanReject returns true if an order can be rejected from its current status
func (sm *StateMachine) CanReject(status models.OrderStatus) bool {
	// Can reject from pending, validated, or submitted states
	rejectableStates := map[models.OrderStatus]bool{
		models.OrderStatusPending:   true,
		models.OrderStatusValidated: true,
		models.OrderStatusSubmitted: true,
	}
	return rejectableStates[status]
}

// GetTransitionPath returns the expected path from one state to another
func (sm *StateMachine) GetTransitionPath(from, to models.OrderStatus) ([]models.OrderStatus, error) {
	// For simple cases, return direct path
	if sm.CanTransition(from, to) {
		return []models.OrderStatus{from, to}, nil
	}

	// For complex cases, would need graph traversal
	// For now, return error
	return nil, fmt.Errorf("no direct path from %s to %s", from, to)
}
