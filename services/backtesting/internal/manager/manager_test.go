package manager

import (
	"testing"
	"time"

	"bitso-trading-platform/backtesting/internal/models"
)

func TestBacktestQueue(t *testing.T) {
	queue := NewBacktestQueue(10)

	// Test empty queue
	if !queue.IsEmpty() {
		t.Error("Expected queue to be empty")
	}

	if queue.Size() != 0 {
		t.Errorf("Expected size 0, got %d", queue.Size())
	}

	// Test enqueue
	err := queue.Enqueue("bt-1")
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	if queue.Size() != 1 {
		t.Errorf("Expected size 1, got %d", queue.Size())
	}

	if queue.IsEmpty() {
		t.Error("Expected queue to not be empty")
	}

	// Test dequeue
	id, err := queue.Dequeue()
	if err != nil {
		t.Fatalf("Dequeue() error = %v", err)
	}

	if id != "bt-1" {
		t.Errorf("Expected 'bt-1', got '%s'", id)
	}

	if !queue.IsEmpty() {
		t.Error("Expected queue to be empty after dequeue")
	}
}

func TestBacktestQueueCapacity(t *testing.T) {
	queue := NewBacktestQueue(2)

	// Fill queue
	queue.Enqueue("bt-1")
	queue.Enqueue("bt-2")

	if !queue.IsFull() {
		t.Error("Expected queue to be full")
	}

	// Try to add when full
	err := queue.Enqueue("bt-3")
	if err == nil {
		t.Error("Expected error when enqueueing to full queue")
	}
}

func TestBacktestQueueRemove(t *testing.T) {
	queue := NewBacktestQueue(10)

	queue.Enqueue("bt-1")
	queue.Enqueue("bt-2")
	queue.Enqueue("bt-3")

	// Remove middle item
	removed := queue.Remove("bt-2")
	if !removed {
		t.Error("Expected bt-2 to be removed")
	}

	if queue.Size() != 2 {
		t.Errorf("Expected size 2, got %d", queue.Size())
	}

	// Try to remove non-existent item
	removed = queue.Remove("bt-99")
	if removed {
		t.Error("Expected false for non-existent item")
	}
}

func TestBacktestQueuePeek(t *testing.T) {
	queue := NewBacktestQueue(10)

	// Peek empty queue
	_, err := queue.Peek()
	if err == nil {
		t.Error("Expected error when peeking empty queue")
	}

	// Add items
	queue.Enqueue("bt-1")
	queue.Enqueue("bt-2")

	// Peek
	id, err := queue.Peek()
	if err != nil {
		t.Fatalf("Peek() error = %v", err)
	}

	if id != "bt-1" {
		t.Errorf("Expected 'bt-1', got '%s'", id)
	}

	// Size should not change
	if queue.Size() != 2 {
		t.Error("Peek should not change queue size")
	}
}

func TestBacktestQueueClear(t *testing.T) {
	queue := NewBacktestQueue(10)

	queue.Enqueue("bt-1")
	queue.Enqueue("bt-2")
	queue.Enqueue("bt-3")

	queue.Clear()

	if !queue.IsEmpty() {
		t.Error("Expected queue to be empty after clear")
	}

	if queue.Size() != 0 {
		t.Errorf("Expected size 0, got %d", queue.Size())
	}
}

func TestCreateBacktest(t *testing.T) {
	// This would need a full manager setup
	// For now, test basic config creation
	config := models.NewBacktestConfig("Test", "btc_mxn",
		time.Now().AddDate(0, -1, 0), time.Now())

	if err := config.Validate(); err != nil {
		t.Errorf("Config validation error = %v", err)
	}
}
