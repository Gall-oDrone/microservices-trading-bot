package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"bitso_trading_bot/internal/queue"
	"bitso_trading_bot/pkg/bitso"

	"github.com/redis/go-redis/v9"
)

// RedisClient implements the Client interface using Redis
type RedisClient struct {
	client *redis.Client
	ctx    context.Context
}

// Initialize creates a new Redis client with default configuration
func Initialize() (*RedisClient, error) {
	cfg := NewConfig()
	return NewClient(cfg)
}

// InitializeWithConfig creates a new Redis client with custom configuration
func InitializeWithConfig(host string, port int, password string, db int, poolSize int) (*RedisClient, error) {
	cfg := &Config{
		Host:     host,
		Port:     port,
		Password: password,
		DB:       db,
		PoolSize: poolSize,
		Timeout:  5 * time.Second,
	}
	return NewClient(cfg)
}

// NewClient creates a new Redis client with the given configuration
func NewClient(cfg *Config) (*RedisClient, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr:     cfg.GetDSN(),
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisClient{
		client: client,
		ctx:    ctx,
	}, nil
}

// Close closes the Redis connection
func (c *RedisClient) Close() error {
	return c.client.Close()
}

// Ping checks if the Redis connection is alive
func (c *RedisClient) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// SaveTicker saves a ticker to Redis
func (c *RedisClient) SaveTicker(ticker bitso.Ticker) error {
	data, err := json.Marshal(ticker)
	if err != nil {
		return fmt.Errorf("failed to marshal ticker: %w", err)
	}

	// Debug: Print the JSON data being saved
	fmt.Printf("Saving ticker JSON data: %s\n", string(data))

	key := fmt.Sprintf("ticker:%s", ticker.Book)
	if err := c.client.Set(c.ctx, key, data, 24*time.Hour).Err(); err != nil {
		return fmt.Errorf("failed to save ticker: %w", err)
	}

	return nil
}

// GetTicker retrieves the latest ticker from Redis
func (c *RedisClient) GetTicker() (bitso.Ticker, error) {
	var ticker bitso.Ticker
	key := "ticker:btc_usd"

	data, err := c.client.Get(c.ctx, key).Bytes()
	if err != nil {
		return ticker, fmt.Errorf("failed to get ticker: %w", err)
	}

	// Debug: Print the raw JSON data
	fmt.Printf("Retrieved ticker JSON data: %s\n", string(data))

	if err := json.Unmarshal(data, &ticker); err != nil {
		return ticker, fmt.Errorf("failed to unmarshal ticker: %w", err)
	}

	return ticker, nil
}

// SaveUserOrder saves a user order to Redis
func (c *RedisClient) SaveUserOrder(order bitso.UserOrder) error {
	data, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("failed to marshal order: %w", err)
	}

	key := fmt.Sprintf("order:%s", order.OID)
	if err := c.client.Set(c.ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("failed to save order: %w", err)
	}

	return nil
}

// GetUserOrderById retrieves a user order by ID
func (c *RedisClient) GetUserOrderById(orderId string) (bitso.UserOrder, error) {
	var order bitso.UserOrder
	key := fmt.Sprintf("order:%s", orderId)

	data, err := c.client.Get(c.ctx, key).Bytes()
	if err != nil {
		return order, fmt.Errorf("failed to get order: %w", err)
	}

	if err := json.Unmarshal(data, &order); err != nil {
		return order, fmt.Errorf("failed to unmarshal order: %w", err)
	}

	return order, nil
}

// GetAllUserOrders retrieves all user orders
func (c *RedisClient) GetAllUserOrders() ([]bitso.UserOrder, error) {
	keys, err := c.client.Keys(c.ctx, "order:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get order keys: %w", err)
	}

	orders := make([]bitso.UserOrder, 0, len(keys))
	for _, key := range keys {
		data, err := c.client.Get(c.ctx, key).Bytes()
		if err != nil {
			return nil, fmt.Errorf("failed to get order data: %w", err)
		}

		var order bitso.UserOrder
		if err := json.Unmarshal(data, &order); err != nil {
			return nil, fmt.Errorf("failed to unmarshal order: %w", err)
		}

		orders = append(orders, order)
	}

	return orders, nil
}

// DeleteAllUserOrders deletes all user orders
func (c *RedisClient) DeleteAllUserOrders() error {
	keys, err := c.client.Keys(c.ctx, "order:*").Result()
	if err != nil {
		return fmt.Errorf("failed to get order keys: %w", err)
	}

	if len(keys) > 0 {
		if err := c.client.Del(c.ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to delete orders: %w", err)
		}
	}

	return nil
}

// SaveTrade saves a trade to Redis
func (c *RedisClient) SaveTrade(trade *bitso.UserTrade) error {
	data, err := json.Marshal(trade)
	if err != nil {
		return fmt.Errorf("failed to marshal trade: %w", err)
	}

	// Debug: Print the trade data being saved
	fmt.Printf("Saving trade with side: %v (%s)\n", trade.Side, trade.Side.String())

	key := fmt.Sprintf("trade:%s", trade.OID)
	if err := c.client.Set(c.ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("failed to save trade: %w", err)
	}

	return nil
}

// GetUserTrade retrieves a user trade by ID
func (c *RedisClient) GetUserTrade(tradeId string) (bitso.UserTrade, error) {
	var trade bitso.UserTrade
	key := fmt.Sprintf("trade:%s", tradeId)

	data, err := c.client.Get(c.ctx, key).Bytes()
	if err != nil {
		return trade, fmt.Errorf("failed to get trade: %w", err)
	}

	// Debug: Print the raw JSON data
	fmt.Printf("Retrieved trade JSON data: %s\n", string(data))

	if err := json.Unmarshal(data, &trade); err != nil {
		return trade, fmt.Errorf("failed to unmarshal trade: %w", err)
	}

	// Debug: Print the retrieved trade side
	fmt.Printf("Retrieved trade with side: %v (%s)\n", trade.Side, trade.Side.String())

	return trade, nil
}

// SaveBatchTrade saves a batch trade to Redis
func (c *RedisClient) SaveBatchTrade(batch queue.BidTradeTrendConsumer) error {
	data, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("failed to marshal batch: %w", err)
	}

	key := fmt.Sprintf("batch:%d", batch.BatchId)
	if err := c.client.Set(c.ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("failed to save batch: %w", err)
	}

	return nil
}

// GetLastNthBatches retrieves the last N batch trades
func (c *RedisClient) GetLastNthBatches(n int) ([]queue.BidTradeTrendConsumer, error) {
	keys, err := c.client.Keys(c.ctx, "batch:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get batch keys: %w", err)
	}

	if len(keys) == 0 {
		return nil, nil
	}

	// Get the last N keys
	start := 0
	if len(keys) > n {
		start = len(keys) - n
	}
	keys = keys[start:]

	batches := make([]queue.BidTradeTrendConsumer, 0, len(keys))
	for _, key := range keys {
		data, err := c.client.Get(c.ctx, key).Bytes()
		if err != nil {
			return nil, fmt.Errorf("failed to get batch data: %w", err)
		}

		var batch queue.BidTradeTrendConsumer
		if err := json.Unmarshal(data, &batch); err != nil {
			return nil, fmt.Errorf("failed to unmarshal batch: %w", err)
		}

		batches = append(batches, batch)
	}

	return batches, nil
}

// InsertTradeRecord saves a websocket trade record
func (c *RedisClient) InsertTradeRecord(trade *bitso.WebSocketTrade) error {
	data, err := json.Marshal(trade)
	if err != nil {
		return fmt.Errorf("failed to marshal trade record: %w", err)
	}

	key := fmt.Sprintf("ws_trade:%d", trade.Sent)
	if err := c.client.Set(c.ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("failed to save trade record: %w", err)
	}

	return nil
}

// GetLatestTradeRecord retrieves the latest websocket trade record
func (c *RedisClient) GetLatestTradeRecord() (bitso.WebSocketTrade, error) {
	var trade bitso.WebSocketTrade
	keys, err := c.client.Keys(c.ctx, "ws_trade:*").Result()
	if err != nil {
		return trade, fmt.Errorf("failed to get trade record keys: %w", err)
	}

	if len(keys) == 0 {
		return trade, nil
	}

	// Get the latest key
	latestKey := keys[len(keys)-1]
	data, err := c.client.Get(c.ctx, latestKey).Bytes()
	if err != nil {
		return trade, fmt.Errorf("failed to get trade record data: %w", err)
	}

	if err := json.Unmarshal(data, &trade); err != nil {
		return trade, fmt.Errorf("failed to unmarshal trade record: %w", err)
	}

	return trade, nil
}

// GetTradeRecordsByTimestampRange retrieves trade records within a timestamp range
func (c *RedisClient) GetTradeRecordsByTimestampRange(start, end uint64) ([]bitso.WebSocketTrade, error) {
	keys, err := c.client.Keys(c.ctx, "ws_trade:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get trade record keys: %w", err)
	}

	trades := make([]bitso.WebSocketTrade, 0)
	for _, key := range keys {
		data, err := c.client.Get(c.ctx, key).Bytes()
		if err != nil {
			return nil, fmt.Errorf("failed to get trade record data: %w", err)
		}

		var trade bitso.WebSocketTrade
		if err := json.Unmarshal(data, &trade); err != nil {
			return nil, fmt.Errorf("failed to unmarshal trade record: %w", err)
		}

		if trade.Sent >= start && trade.Sent <= end {
			trades = append(trades, trade)
		}
	}

	return trades, nil
}

// DeleteAllWSTradeRecords deletes all websocket trade records
func (c *RedisClient) DeleteAllWSTradeRecords() error {
	keys, err := c.client.Keys(c.ctx, "ws_trade:*").Result()
	if err != nil {
		return fmt.Errorf("failed to get trade record keys: %w", err)
	}

	if len(keys) > 0 {
		if err := c.client.Del(c.ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to delete trade records: %w", err)
		}
	}

	return nil
}

// GetKeysMatchingPattern retrieves keys matching a pattern
func (c *RedisClient) GetKeysMatchingPattern(pattern string) ([]string, error) {
	keys, err := c.client.Keys(c.ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get keys: %w", err)
	}

	return keys, nil
}

// DeleteKafkaBatchKeysByPattern deletes keys matching a pattern
func (c *RedisClient) DeleteKafkaBatchKeysByPattern(pattern string) error {
	keys, err := c.client.Keys(c.ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("failed to get keys: %w", err)
	}

	if len(keys) > 0 {
		if err := c.client.Del(c.ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to delete keys: %w", err)
		}
	}

	return nil
}
