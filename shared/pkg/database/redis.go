package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"

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
func (c *RedisClient) GetLatestTradeRecord() (*bitso.WebSocketTrade, error) {
	var trade bitso.WebSocketTrade
	keys, err := c.client.Keys(c.ctx, "ws_trade:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get trade record keys: %w", err)
	}

	if len(keys) == 0 {
		return nil, nil
	}

	// Get the latest key
	latestKey := keys[len(keys)-1]
	data, err := c.client.Get(c.ctx, latestKey).Bytes()
	if err != nil {
		return nil, fmt.Errorf("failed to get trade record data: %w", err)
	}

	if err := json.Unmarshal(data, &trade); err != nil {
		return nil, fmt.Errorf("failed to unmarshal trade record: %w", err)
	}

	return &trade, nil
}

// GetTradeRecordsByTimestampRange retrieves trade records within a timestamp range
func (c *RedisClient) GetTradeRecordsByTimestampRange(start, end uint64) ([]*bitso.WebSocketTrade, error) {
	keys, err := c.client.Keys(c.ctx, "ws_trade:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get trade record keys: %w", err)
	}

	trades := make([]*bitso.WebSocketTrade, 0)
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
			trades = append(trades, &trade)
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

// SaveUserBalance saves a user's balance for a specific currency to Redis
func (c *RedisClient) SaveUserBalance(balance *bitso.Balance) error {
	data, err := json.Marshal(balance)
	if err != nil {
		return fmt.Errorf("failed to marshal balance: %w", err)
	}

	key := fmt.Sprintf("balance:%s", balance.Currency.String())
	if err := c.client.Set(c.ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("failed to save balance: %w", err)
	}

	return nil
}

// GetUserBalance retrieves a user's balance for a specific currency from Redis
func (c *RedisClient) GetUserBalance(currency string) (bitso.Balance, error) {
	var balance bitso.Balance
	key := fmt.Sprintf("balance:%s", currency)

	data, err := c.client.Get(c.ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return balance, fmt.Errorf("balance not found for currency: %s", currency)
		}
		return balance, fmt.Errorf("failed to get balance: %w", err)
	}

	if err := json.Unmarshal(data, &balance); err != nil {
		return balance, fmt.Errorf("failed to unmarshal balance: %w", err)
	}

	return balance, nil
}

// GetOrdersBySide returns a map of order IDs by their side
func (c *RedisClient) GetOrdersBySide(side string) (map[string]string, error) {
	pattern := fmt.Sprintf("order:*:%s", side)
	keys, err := c.GetKeysMatchingPattern(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to get orders by side: %w", err)
	}

	orders := make(map[string]string)
	for _, key := range keys {
		// Extract order ID from key
		parts := strings.Split(key, ":")
		if len(parts) >= 2 {
			orderID := parts[1]
			orders[orderID] = side
		}
	}

	return orders, nil
}

// GetOrdersByStatus returns a map of order IDs by their status
func (c *RedisClient) GetOrdersByStatus(status string) (map[string]string, error) {
	// Get all orders first
	orders, err := c.GetAllUserOrders()
	if err != nil {
		return nil, fmt.Errorf("failed to get orders: %w", err)
	}

	// Filter orders by status
	ordersByStatus := make(map[string]string)
	for _, order := range orders {
		if order.Status.String() == status {
			ordersByStatus[order.OID] = status
		}
	}

	return ordersByStatus, nil
}

// SetOrderWithTTL sets an order with a time-to-live
func (c *RedisClient) SetOrderWithTTL(oid string, timeout time.Duration) error {
	key := fmt.Sprintf("order_ttl:%s", oid)
	return c.client.Set(c.ctx, key, "active", timeout).Err()
}
