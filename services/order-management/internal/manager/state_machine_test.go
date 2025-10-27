package manager

import (
	"testing"

	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/models"
)

func TestCanTransition(t *testing.T) {
	sm := NewStateMachine(logger.DefaultLogger())

	tests := []struct {
		name string
		from models.OrderStatus
		to   models.OrderStatus
		want bool
	}{
		// Valid transitions
		{"pending to validated", models.OrderStatusPending, models.OrderStatusValidated, true},
		{"pending to rejected", models.OrderStatusPending, models.OrderStatusRejected, true},
		{"validated to submitted", models.OrderStatusValidated, models.OrderStatusSubmitted, true},
		{"validated to rejected", models.OrderStatusValidated, models.OrderStatusRejected, true},
		{"submitted to accepted", models.OrderStatusSubmitted, models.OrderStatusAccepted, true},
		{"submitted to rejected", models.OrderStatusSubmitted, models.OrderStatusRejected, true},
		{"accepted to filled", models.OrderStatusAccepted, models.OrderStatusFilled, true},
		{"accepted to partially_filled", models.OrderStatusAccepted, models.OrderStatusPartiallyFilled, true},
		{"accepted to cancelled", models.OrderStatusAccepted, models.OrderStatusCancelled, true},
		{"partially_filled to filled", models.OrderStatusPartiallyFilled, models.OrderStatusFilled, true},
		{"partially_filled to cancelled", models.OrderStatusPartiallyFilled, models.OrderStatusCancelled, true},

		// Invalid transitions
		{"pending to filled", models.OrderStatusPending, models.OrderStatusFilled, false},
		{"pending to accepted", models.OrderStatusPending, models.OrderStatusAccepted, false},
		{"filled to cancelled", models.OrderStatusFilled, models.OrderStatusCancelled, false},
		{"cancelled to filled", models.OrderStatusCancelled, models.OrderStatusFilled, false},
		{"rejected to pending", models.OrderStatusRejected, models.OrderStatusPending, false},
		{"same state", models.OrderStatusPending, models.OrderStatusPending, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sm.CanTransition(tt.from, tt.to)
			if got != tt.want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestValidateTransition(t *testing.T) {
	sm := NewStateMachine(logger.DefaultLogger())

	tests := []struct {
		name    string
		from    models.OrderStatus
		to      models.OrderStatus
		wantErr bool
	}{
		{"valid transition", models.OrderStatusPending, models.OrderStatusValidated, false},
		{"invalid transition", models.OrderStatusPending, models.OrderStatusFilled, true},
		{"same state", models.OrderStatusPending, models.OrderStatusPending, true},
		{"from final state", models.OrderStatusFilled, models.OrderStatusCancelled, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sm.ValidateTransition(tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTransition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTransition(t *testing.T) {
	sm := NewStateMachine(logger.DefaultLogger())

	// Create an order
	order := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)

	// Test valid transition
	err := sm.Transition(order, models.OrderStatusValidated)
	if err != nil {
		t.Errorf("Transition() failed: %v", err)
	}

	if order.Status != models.OrderStatusValidated {
		t.Errorf("Expected status %s, got %s", models.OrderStatusValidated, order.Status)
	}

	// Test invalid transition
	err = sm.Transition(order, models.OrderStatusFilled)
	if err == nil {
		t.Error("Expected error for invalid transition, got nil")
	}

	// Status should not change
	if order.Status != models.OrderStatusValidated {
		t.Errorf("Status should remain %s after failed transition, got %s", models.OrderStatusValidated, order.Status)
	}
}

func TestGetAllowedTransitions(t *testing.T) {
	sm := NewStateMachine(logger.DefaultLogger())

	tests := []struct {
		name          string
		status        models.OrderStatus
		expectedCount int
	}{
		{"pending", models.OrderStatusPending, 2},                  // validated, rejected
		{"validated", models.OrderStatusValidated, 2},              // submitted, rejected
		{"submitted", models.OrderStatusSubmitted, 2},              // accepted, rejected
		{"accepted", models.OrderStatusAccepted, 3},                // partially_filled, filled, cancelled
		{"partially_filled", models.OrderStatusPartiallyFilled, 2}, // filled, cancelled
		{"filled", models.OrderStatusFilled, 0},                    // final state
		{"cancelled", models.OrderStatusCancelled, 0},              // final state
		{"rejected", models.OrderStatusRejected, 0},                // final state
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed := sm.GetAllowedTransitions(tt.status)
			if len(allowed) != tt.expectedCount {
				t.Errorf("Expected %d allowed transitions from %s, got %d", tt.expectedCount, tt.status, len(allowed))
			}
		})
	}
}

func TestIsFinalState(t *testing.T) {
	sm := NewStateMachine(logger.DefaultLogger())

	tests := []struct {
		status models.OrderStatus
		want   bool
	}{
		{models.OrderStatusPending, false},
		{models.OrderStatusValidated, false},
		{models.OrderStatusSubmitted, false},
		{models.OrderStatusAccepted, false},
		{models.OrderStatusPartiallyFilled, false},
		{models.OrderStatusFilled, true},
		{models.OrderStatusCancelled, true},
		{models.OrderStatusRejected, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := sm.IsFinalState(tt.status)
			if got != tt.want {
				t.Errorf("IsFinalState(%s) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestGetStateDescription(t *testing.T) {
	sm := NewStateMachine(logger.DefaultLogger())

	tests := []struct {
		status models.OrderStatus
	}{
		{models.OrderStatusPending},
		{models.OrderStatusValidated},
		{models.OrderStatusSubmitted},
		{models.OrderStatusAccepted},
		{models.OrderStatusPartiallyFilled},
		{models.OrderStatusFilled},
		{models.OrderStatusCancelled},
		{models.OrderStatusRejected},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			desc := sm.GetStateDescription(tt.status)
			if desc == "" || desc == "Unknown status" {
				t.Errorf("GetStateDescription(%s) returned empty or unknown", tt.status)
			}
		})
	}
}

func TestCanCancel(t *testing.T) {
	sm := NewStateMachine(logger.DefaultLogger())

	tests := []struct {
		status models.OrderStatus
		want   bool
	}{
		{models.OrderStatusPending, false},
		{models.OrderStatusValidated, false},
		{models.OrderStatusSubmitted, false},
		{models.OrderStatusAccepted, true},
		{models.OrderStatusPartiallyFilled, true},
		{models.OrderStatusFilled, false},
		{models.OrderStatusCancelled, false},
		{models.OrderStatusRejected, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := sm.CanCancel(tt.status)
			if got != tt.want {
				t.Errorf("CanCancel(%s) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestCanReject(t *testing.T) {
	sm := NewStateMachine(logger.DefaultLogger())

	tests := []struct {
		status models.OrderStatus
		want   bool
	}{
		{models.OrderStatusPending, true},
		{models.OrderStatusValidated, true},
		{models.OrderStatusSubmitted, true},
		{models.OrderStatusAccepted, false},
		{models.OrderStatusPartiallyFilled, false},
		{models.OrderStatusFilled, false},
		{models.OrderStatusCancelled, false},
		{models.OrderStatusRejected, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := sm.CanReject(tt.status)
			if got != tt.want {
				t.Errorf("CanReject(%s) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestCompleteOrderFlow(t *testing.T) {
	sm := NewStateMachine(logger.DefaultLogger())
	order := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
	_ = order // Suppress unused warning initially

	// Expected flow: pending -> validated -> submitted -> accepted -> filled
	transitions := []models.OrderStatus{
		models.OrderStatusValidated,
		models.OrderStatusSubmitted,
		models.OrderStatusAccepted,
		models.OrderStatusPartiallyFilled,
		models.OrderStatusFilled,
	}

	for _, nextStatus := range transitions {
		err := sm.Transition(order, nextStatus)
		if err != nil {
			t.Errorf("Transition to %s failed: %v", nextStatus, err)
		}

		if order.Status != nextStatus {
			t.Errorf("Expected status %s, got %s", nextStatus, order.Status)
		}
	}

	// Try to transition from final state (should fail)
	err := sm.Transition(order, models.OrderStatusCancelled)
	if err == nil {
		t.Error("Expected error when transitioning from final state, got nil")
	}
}

func TestRejectionFlow(t *testing.T) {
	sm := NewStateMachine(logger.DefaultLogger())

	// Test rejection at different stages
	stages := []models.OrderStatus{
		models.OrderStatusPending,
		models.OrderStatusValidated,
		models.OrderStatusSubmitted,
	}

	for _, stage := range stages {
		testOrder := models.NewOrder("signal", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
		testOrder.UpdateStatus(stage)

		err := sm.Transition(testOrder, models.OrderStatusRejected)
		if err != nil {
			t.Errorf("Failed to reject from %s: %v", stage, err)
		}

		if testOrder.Status != models.OrderStatusRejected {
			t.Errorf("Expected status rejected, got %s", testOrder.Status)
		}
	}
}

func TestCancellationFlow(t *testing.T) {
	sm := NewStateMachine(logger.DefaultLogger())

	// Test cancellation from accepted
	order := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
	order.UpdateStatus(models.OrderStatusAccepted)

	err := sm.Transition(order, models.OrderStatusCancelled)
	if err != nil {
		t.Errorf("Failed to cancel from accepted: %v", err)
	}

	// Test cancellation from partially filled
	order2 := models.NewOrder("signal-2", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
	order2.UpdateStatus(models.OrderStatusPartiallyFilled)

	err = sm.Transition(order2, models.OrderStatusCancelled)
	if err != nil {
		t.Errorf("Failed to cancel from partially filled: %v", err)
	}
}
