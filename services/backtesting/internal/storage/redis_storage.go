package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/models"
	"github.com/redis/go-redis/v9"
)

// RedisStorage implements ResultStorage using Redis
type RedisStorage struct {
	client *redis.Client
	logger logger.Logger
	ttl    time.Duration
}

// NewRedisStorage creates a new Redis storage instance
func NewRedisStorage(client *redis.Client, log logger.Logger, ttl time.Duration) *RedisStorage {
	return &RedisStorage{
		client: client,
		logger: log,
		ttl:    ttl,
	}
}

// Save saves a backtest result
func (s *RedisStorage) Save(ctx context.Context, result *models.BacktestResult) error {
	// Serialize result
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}
	
	// Generate key
	key := s.generateKey(result.BacktestID)
	
	// Save to Redis with TTL
	if err := s.client.Set(ctx, key, data, s.ttl).Err(); err != nil {
		return fmt.Errorf("redis set error: %w", err)
	}
	
	// Add to index sets for filtering
	if err := s.addToIndexes(ctx, result); err != nil {
		s.logger.Warn("Failed to update indexes", map[string]interface{}{"error": err})
	}
	
	s.logger.Debug("Saved backtest result", map[string]interface{}{
		"backtest_id": result.BacktestID,
		"key":         key,
	})
	
	return nil
}

// Get retrieves a backtest result by ID
func (s *RedisStorage) Get(ctx context.Context, backtestID string) (*models.BacktestResult, error) {
	key := s.generateKey(backtestID)
	
	data, err := s.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("backtest not found: %s", backtestID)
		}
		return nil, fmt.Errorf("redis get error: %w", err)
	}
	
	// Deserialize
	var result models.BacktestResult
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}
	
	return &result, nil
}

// List lists backtest results with optional filters
func (s *RedisStorage) List(ctx context.Context, filters *ListFilters) ([]*models.BacktestResult, error) {
	if filters == nil {
		filters = NewListFilters()
	}
	filters.Validate()
	
	// Get all backtest keys
	pattern := "backtest:result:*"
	keys, err := s.scanKeys(ctx, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to scan keys: %w", err)
	}
	
	// Fetch all results
	results := make([]*models.BacktestResult, 0)
	for _, key := range keys {
		result, err := s.Get(ctx, strings.TrimPrefix(key, "backtest:result:"))
		if err != nil {
			s.logger.Warn("Failed to get result", map[string]interface{}{
				"key":   key,
				"error": err,
			})
			continue
		}
		
		// Apply filters
		if s.matchesFilters(result, filters) {
			results = append(results, result)
		}
	}
	
	// Sort results
	s.sortResults(results, filters.SortBy, filters.SortOrder)
	
	// Apply pagination
	start := filters.Offset
	if start > len(results) {
		start = len(results)
	}
	
	end := start + filters.Limit
	if end > len(results) {
		end = len(results)
	}
	
	return results[start:end], nil
}

// Delete deletes a backtest result
func (s *RedisStorage) Delete(ctx context.Context, backtestID string) error {
	key := s.generateKey(backtestID)
	
	if err := s.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("redis delete error: %w", err)
	}
	
	// Remove from indexes
	if err := s.removeFromIndexes(ctx, backtestID); err != nil {
		s.logger.Warn("Failed to remove from indexes", map[string]interface{}{"error": err})
	}
	
	s.logger.Info("Deleted backtest result", map[string]interface{}{
		"backtest_id": backtestID,
	})
	
	return nil
}

// UpdateStatus updates the status and progress of a backtest
func (s *RedisStorage) UpdateStatus(ctx context.Context, backtestID string, status string, progress float64) error {
	// Get existing result
	result, err := s.Get(ctx, backtestID)
	if err != nil {
		return fmt.Errorf("failed to get result: %w", err)
	}
	
	// Update fields
	result.Status = status
	result.Progress = progress
	
	// Save updated result
	return s.Save(ctx, result)
}

// Close closes the storage connection
func (s *RedisStorage) Close() error {
	return s.client.Close()
}

// Helper methods

// generateKey generates a Redis key for a backtest result
func (s *RedisStorage) generateKey(backtestID string) string {
	return fmt.Sprintf("backtest:result:%s", backtestID)
}

// addToIndexes adds the result to index sets for faster filtering
func (s *RedisStorage) addToIndexes(ctx context.Context, result *models.BacktestResult) error {
	// Add to status index
	statusKey := fmt.Sprintf("backtest:index:status:%s", result.Status)
	if err := s.client.SAdd(ctx, statusKey, result.BacktestID).Err(); err != nil {
		return err
	}
	
	// Add to all results index
	allKey := "backtest:index:all"
	if err := s.client.ZAdd(ctx, allKey, redis.Z{
		Score:  float64(result.StartedAt.Unix()),
		Member: result.BacktestID,
	}).Err(); err != nil {
		return err
	}
	
	return nil
}

// removeFromIndexes removes the result from index sets
func (s *RedisStorage) removeFromIndexes(ctx context.Context, backtestID string) error {
	// Remove from all status indexes
	statuses := []string{"pending", "queued", "running", "completed", "failed", "cancelled"}
	for _, status := range statuses {
		statusKey := fmt.Sprintf("backtest:index:status:%s", status)
		s.client.SRem(ctx, statusKey, backtestID)
	}
	
	// Remove from all results index
	allKey := "backtest:index:all"
	s.client.ZRem(ctx, allKey, backtestID)
	
	return nil
}

// scanKeys scans for keys matching a pattern
func (s *RedisStorage) scanKeys(ctx context.Context, pattern string) ([]string, error) {
	keys := make([]string, 0)
	
	iter := s.client.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	
	if err := iter.Err(); err != nil {
		return nil, err
	}
	
	return keys, nil
}

// matchesFilters checks if a result matches the given filters
func (s *RedisStorage) matchesFilters(result *models.BacktestResult, filters *ListFilters) bool {
	// Status filter
	if filters.Status != "" && result.Status != filters.Status {
		return false
	}
	
	// Date range filters
	if filters.StartDate != nil && result.StartedAt.Before(*filters.StartDate) {
		return false
	}
	if filters.EndDate != nil && result.StartedAt.After(*filters.EndDate) {
		return false
	}
	
	return true
}

// sortResults sorts results by the specified field
func (s *RedisStorage) sortResults(results []*models.BacktestResult, sortBy, sortOrder string) {
	// Simple bubble sort (can be optimized with sort.Slice)
	n := len(results)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			shouldSwap := false
			
			// Determine if swap is needed based on sort field
			switch sortBy {
			case "created_at":
				if sortOrder == "asc" {
					shouldSwap = results[j].StartedAt.After(results[j+1].StartedAt)
				} else {
					shouldSwap = results[j].StartedAt.Before(results[j+1].StartedAt)
				}
			default:
				// Default sort by created_at desc
				shouldSwap = results[j].StartedAt.Before(results[j+1].StartedAt)
			}
			
			if shouldSwap {
				results[j], results[j+1] = results[j+1], results[j]
			}
		}
	}
}

