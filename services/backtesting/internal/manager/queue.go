package manager

import (
	"fmt"
	"sync"
)

// BacktestQueue manages a queue of pending backtests
type BacktestQueue struct {
	queue    []string // Backtest IDs
	capacity int
	mu       sync.RWMutex
}

// NewBacktestQueue creates a new backtest queue
func NewBacktestQueue(capacity int) *BacktestQueue {
	return &BacktestQueue{
		queue:    make([]string, 0, capacity),
		capacity: capacity,
	}
}

// Enqueue adds a backtest to the queue
func (q *BacktestQueue) Enqueue(backtestID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.queue) >= q.capacity {
		return fmt.Errorf("queue is full (capacity: %d)", q.capacity)
	}

	q.queue = append(q.queue, backtestID)
	return nil
}

// Dequeue removes and returns the next backtest from the queue
func (q *BacktestQueue) Dequeue() (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.queue) == 0 {
		return "", fmt.Errorf("queue is empty")
	}

	backtestID := q.queue[0]
	q.queue = q.queue[1:]

	return backtestID, nil
}

// Remove removes a specific backtest from the queue
func (q *BacktestQueue) Remove(backtestID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, id := range q.queue {
		if id == backtestID {
			q.queue = append(q.queue[:i], q.queue[i+1:]...)
			return true
		}
	}

	return false
}

// Size returns the current queue size
func (q *BacktestQueue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.queue)
}

// IsFull returns true if the queue is full
func (q *BacktestQueue) IsFull() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.queue) >= q.capacity
}

// IsEmpty returns true if the queue is empty
func (q *BacktestQueue) IsEmpty() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.queue) == 0
}

// Clear clears all items from the queue
func (q *BacktestQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.queue = make([]string, 0, q.capacity)
}

// Peek returns the next backtest without removing it
func (q *BacktestQueue) Peek() (string, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if len(q.queue) == 0 {
		return "", fmt.Errorf("queue is empty")
	}

	return q.queue[0], nil
}

// GetAll returns all backtest IDs in the queue
func (q *BacktestQueue) GetAll() []string {
	q.mu.RLock()
	defer q.mu.RUnlock()

	ids := make([]string, len(q.queue))
	copy(ids, q.queue)
	return ids
}
