package persistence

import (
	"context"
	"fmt"

	"bitso-trading-platform/strategy-executor/internal/strategies"
	"github.com/redis/go-redis/v9"
)

const defaultKeyPrefix = "strategy-executor:limit_profit:"

// RedisLimitProfitStore stores limit_profit JSON snapshots in Redis (no TTL — survives restarts).
type RedisLimitProfitStore struct {
	rdb    *redis.Client
	prefix string
}

// NewRedisLimitProfitStore builds a strategies.LimitProfitRawStateStore backed by Redis.
func NewRedisLimitProfitStore(rdb *redis.Client) strategies.LimitProfitRawStateStore {
	if rdb == nil {
		return nil
	}
	return &RedisLimitProfitStore{rdb: rdb, prefix: defaultKeyPrefix}
}

func (s *RedisLimitProfitStore) key(name string) string {
	return s.prefix + name
}

// Save implements strategies.LimitProfitRawStateStore.
func (s *RedisLimitProfitStore) Save(ctx context.Context, strategyName string, payload []byte) error {
	if strategyName == "" {
		return fmt.Errorf("strategy name required")
	}
	return s.rdb.Set(ctx, s.key(strategyName), payload, 0).Err()
}

// Load implements strategies.LimitProfitRawStateStore.
func (s *RedisLimitProfitStore) Load(ctx context.Context, strategyName string) ([]byte, error) {
	if strategyName == "" {
		return nil, fmt.Errorf("strategy name required")
	}
	b, err := s.rdb.Get(ctx, s.key(strategyName)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	return b, err
}

// Delete implements strategies.LimitProfitRawStateStore.
func (s *RedisLimitProfitStore) Delete(ctx context.Context, strategyName string) error {
	if strategyName == "" {
		return nil
	}
	return s.rdb.Del(ctx, s.key(strategyName)).Err()
}
