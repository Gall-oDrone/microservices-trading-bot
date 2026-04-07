package indicators

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	indicatorKeyPrefix   = "ind"
	bollingerKeyPrefix   = "bb"
	defaultIndicatorTTL  = 5 * time.Minute
)

// RedisIndicatorStore implements IndicatorStore using Redis
type RedisIndicatorStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisIndicatorStore creates a new Redis-based indicator store
func NewRedisIndicatorStore(client *redis.Client) *RedisIndicatorStore {
	return &RedisIndicatorStore{
		client: client,
		ttl:    defaultIndicatorTTL,
	}
}

// NewRedisIndicatorStoreWithTTL creates a store with custom TTL
func NewRedisIndicatorStoreWithTTL(client *redis.Client, ttl time.Duration) *RedisIndicatorStore {
	return &RedisIndicatorStore{
		client: client,
		ttl:    ttl,
	}
}

// indicatorKey generates the Redis key for an indicator
// Format: ind:{book}:{indicator}:{period}
func indicatorKey(book, indicator string, period int) string {
	return fmt.Sprintf("%s:%s:%s:%d", indicatorKeyPrefix, book, indicator, period)
}

// bollingerKey generates the Redis key for Bollinger Bands
func bollingerKey(book string, period int) string {
	return fmt.Sprintf("%s:%s:%d", bollingerKeyPrefix, book, period)
}

// Set stores an indicator value in Redis
func (s *RedisIndicatorStore) Set(ctx context.Context, book, indicator string, period int, value *IndicatorValue) error {
	key := indicatorKey(book, indicator, period)

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal indicator value: %w", err)
	}

	return s.client.Set(ctx, key, data, s.ttl).Err()
}

// Get retrieves an indicator value from Redis
func (s *RedisIndicatorStore) Get(ctx context.Context, book, indicator string, period int) (*IndicatorValue, error) {
	key := indicatorKey(book, indicator, period)

	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("get indicator value: %w", err)
	}

	var value IndicatorValue
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("unmarshal indicator value: %w", err)
	}

	return &value, nil
}

// GetAll retrieves all indicator values for a book
func (s *RedisIndicatorStore) GetAll(ctx context.Context, book string) (map[string]*IndicatorValue, error) {
	pattern := fmt.Sprintf("%s:%s:*", indicatorKeyPrefix, book)
	
	keys, err := s.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("get keys: %w", err)
	}

	result := make(map[string]*IndicatorValue)

	for _, key := range keys {
		data, err := s.client.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}

		var value IndicatorValue
		if err := json.Unmarshal(data, &value); err != nil {
			continue
		}

		result[key] = &value
	}

	return result, nil
}

// SetBollinger stores Bollinger Bands values in Redis
func (s *RedisIndicatorStore) SetBollinger(ctx context.Context, book string, period int, bb *BollingerBands) error {
	key := bollingerKey(book, period)

	data, err := json.Marshal(bb)
	if err != nil {
		return fmt.Errorf("marshal bollinger bands: %w", err)
	}

	return s.client.Set(ctx, key, data, s.ttl).Err()
}

// GetBollinger retrieves Bollinger Bands from Redis
func (s *RedisIndicatorStore) GetBollinger(ctx context.Context, book string, period int) (*BollingerBands, error) {
	key := bollingerKey(book, period)

	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("get bollinger bands: %w", err)
	}

	var bb BollingerBands
	if err := json.Unmarshal(data, &bb); err != nil {
		return nil, fmt.Errorf("unmarshal bollinger bands: %w", err)
	}

	return &bb, nil
}

// SetMultiple stores multiple indicator values atomically
func (s *RedisIndicatorStore) SetMultiple(ctx context.Context, book string, values map[string]*IndicatorValue) error {
	pipe := s.client.Pipeline()

	for indicator, value := range values {
		key := indicatorKey(book, indicator, value.Period)
		data, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal indicator %s: %w", indicator, err)
		}
		pipe.Set(ctx, key, data, s.ttl)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// Delete removes an indicator value from Redis
func (s *RedisIndicatorStore) Delete(ctx context.Context, book, indicator string, period int) error {
	key := indicatorKey(book, indicator, period)
	return s.client.Del(ctx, key).Err()
}

// DeleteAll removes all indicators for a book
func (s *RedisIndicatorStore) DeleteAll(ctx context.Context, book string) error {
	pattern := fmt.Sprintf("%s:%s:*", indicatorKeyPrefix, book)
	
	keys, err := s.client.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("get keys: %w", err)
	}

	if len(keys) == 0 {
		return nil
	}

	return s.client.Del(ctx, keys...).Err()
}

// InMemoryIndicatorStore implements IndicatorStore using in-memory storage (for testing)
type InMemoryIndicatorStore struct {
	indicators map[string]*IndicatorValue
	bollingers map[string]*BollingerBands
}

// NewInMemoryIndicatorStore creates a new in-memory indicator store
func NewInMemoryIndicatorStore() *InMemoryIndicatorStore {
	return &InMemoryIndicatorStore{
		indicators: make(map[string]*IndicatorValue),
		bollingers: make(map[string]*BollingerBands),
	}
}

func (s *InMemoryIndicatorStore) Set(ctx context.Context, book, indicator string, period int, value *IndicatorValue) error {
	key := indicatorKey(book, indicator, period)
	s.indicators[key] = value
	return nil
}

func (s *InMemoryIndicatorStore) Get(ctx context.Context, book, indicator string, period int) (*IndicatorValue, error) {
	key := indicatorKey(book, indicator, period)
	return s.indicators[key], nil
}

func (s *InMemoryIndicatorStore) GetAll(ctx context.Context, book string) (map[string]*IndicatorValue, error) {
	result := make(map[string]*IndicatorValue)
	prefix := fmt.Sprintf("%s:%s:", indicatorKeyPrefix, book)
	for key, value := range s.indicators {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			result[key] = value
		}
	}
	return result, nil
}

func (s *InMemoryIndicatorStore) SetBollinger(ctx context.Context, book string, period int, bb *BollingerBands) error {
	key := bollingerKey(book, period)
	s.bollingers[key] = bb
	return nil
}

func (s *InMemoryIndicatorStore) GetBollinger(ctx context.Context, book string, period int) (*BollingerBands, error) {
	key := bollingerKey(book, period)
	return s.bollingers[key], nil
}
