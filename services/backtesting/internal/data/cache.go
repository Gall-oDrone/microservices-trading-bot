package data

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/models"

	"github.com/redis/go-redis/v9"
)

// Cache provides caching for historical market data
type Cache struct {
	redis  *redis.Client
	ttl    time.Duration
	logger logger.Logger
}

// NewCache creates a new cache instance
func NewCache(redisClient *redis.Client, ttl time.Duration, log logger.Logger) *Cache {
	return &Cache{
		redis:  redisClient,
		ttl:    ttl,
		logger: log,
	}
}

// Get retrieves cached events
func (c *Cache) Get(ctx context.Context, key string) ([]models.MarketEvent, error) {
	data, err := c.redis.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("cache miss")
		}
		return nil, fmt.Errorf("redis get error: %w", err)
	}

	// Deserialize
	var events []models.MarketEvent
	if err := json.Unmarshal([]byte(data), &events); err != nil {
		return nil, fmt.Errorf("failed to unmarshal events: %w", err)
	}

	c.logger.Debug("Cache hit", map[string]interface{}{
		"key":   key,
		"count": len(events),
	})

	return events, nil
}

// Set stores events in cache
func (c *Cache) Set(ctx context.Context, key string, events []models.MarketEvent) error {
	// Serialize
	data, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("failed to marshal events: %w", err)
	}

	// Store with TTL
	if err := c.redis.Set(ctx, key, data, c.ttl).Err(); err != nil {
		return fmt.Errorf("redis set error: %w", err)
	}

	c.logger.Debug("Cached data", map[string]interface{}{
		"key":   key,
		"count": len(events),
		"ttl":   c.ttl,
	})

	return nil
}

// Delete removes cached data
func (c *Cache) Delete(ctx context.Context, key string) error {
	if err := c.redis.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("redis delete error: %w", err)
	}

	c.logger.Debug("Deleted cache key", map[string]interface{}{
		"key": key,
	})

	return nil
}

// Clear clears all cached data (use with caution)
func (c *Cache) Clear(ctx context.Context) error {
	// Delete all keys matching the cache prefix
	pattern := "backtest:data:*"

	iter := c.redis.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		if err := c.redis.Del(ctx, iter.Val()).Err(); err != nil {
			c.logger.Warn("Failed to delete cache key", map[string]interface{}{
				"key":   iter.Val(),
				"error": err,
			})
		}
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("redis scan error: %w", err)
	}

	c.logger.Info("Cache cleared", nil)
	return nil
}

// generateKey generates a cache key for the given parameters
func (c *Cache) generateKey(book string, start, end time.Time, eventType string) string {
	return fmt.Sprintf("backtest:data:%s:%s:%s:%s",
		book,
		eventType,
		start.Format("20060102"),
		end.Format("20060102"))
}

// GetStats returns cache statistics
func (c *Cache) GetStats(ctx context.Context) (map[string]interface{}, error) {
	// Get number of cached keys
	pattern := "backtest:data:*"

	var keyCount int64
	iter := c.redis.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		keyCount++
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("redis scan error: %w", err)
	}

	// Get memory usage (if available)
	memoryStats, _ := c.redis.Info(ctx, "memory").Result()

	return map[string]interface{}{
		"key_count": keyCount,
		"ttl":       c.ttl.String(),
		"memory":    memoryStats,
	}, nil
}

// Ping checks if the cache is available
func (c *Cache) Ping(ctx context.Context) error {
	return c.redis.Ping(ctx).Err()
}

// Close closes the cache connection
func (c *Cache) Close() error {
	return c.redis.Close()
}
