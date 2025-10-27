package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"

	"github.com/redis/go-redis/v9"
)

// RedisCache implements the Cache interface using Redis
type RedisCache struct {
	client *redis.Client
	config *CacheConfig
	logger *log.Logger
}

// NewRedisCache creates a new Redis cache instance
func NewRedisCache(config *CacheConfig, logger *log.Logger) (*RedisCache, error) {
	if config == nil {
		config = DefaultCacheConfig()
	}

	if logger == nil {
		logger = log.New(log.Writer(), "[REDIS-CACHE] ", log.LstdFlags|log.Lshortfile)
	}

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%s", config.RedisHost, config.RedisPort),
		Password:     config.RedisPassword,
		DB:           config.RedisDB,
		PoolSize:     config.PoolSize,
		PoolTimeout:  config.PoolTimeout,
		MinIdleConns: 2,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisCache{
		client: client,
		config: config,
		logger: logger,
	}, nil
}

// SetTrade stores a trade in the cache
func (r *RedisCache) SetTrade(ctx context.Context, book string, trade *models.TradeEvent) error {
	if trade == nil {
		return fmt.Errorf("trade cannot be nil")
	}

	key := r.tradeKey(book, trade.ID)

	// Serialize trade
	data, err := json.Marshal(trade)
	if err != nil {
		return fmt.Errorf("failed to marshal trade: %w", err)
	}

	// Store trade with TTL
	if err := r.client.Set(ctx, key, data, r.config.TradeTTL).Err(); err != nil {
		return fmt.Errorf("failed to set trade: %w", err)
	}

	// Add to recent trades list
	if err := r.addToRecentTrades(ctx, book, trade); err != nil {
		r.logger.Printf("Warning: failed to add trade to recent trades list: %v", err)
	}

	// Update trade statistics
	if err := r.updateTradeStats(ctx, book, trade); err != nil {
		r.logger.Printf("Warning: failed to update trade statistics: %v", err)
	}

	return nil
}

// GetTrade retrieves a trade from the cache
func (r *RedisCache) GetTrade(ctx context.Context, book string, tradeID uint64) (*models.TradeEvent, error) {
	key := r.tradeKey(book, tradeID)

	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Trade not found
		}
		return nil, fmt.Errorf("failed to get trade: %w", err)
	}

	var trade models.TradeEvent
	if err := json.Unmarshal([]byte(data), &trade); err != nil {
		return nil, fmt.Errorf("failed to unmarshal trade: %w", err)
	}

	return &trade, nil
}

// GetRecentTrades retrieves recent trades for a book
func (r *RedisCache) GetRecentTrades(ctx context.Context, book string, limit int) ([]*models.TradeEvent, error) {
	key := r.recentTradesKey(book)

	// Get trade IDs from the list
	tradeIDs, err := r.client.LRange(ctx, key, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get recent trade IDs: %w", err)
	}

	if len(tradeIDs) == 0 {
		return []*models.TradeEvent{}, nil
	}

	// Get trade data for each ID
	trades := make([]*models.TradeEvent, 0, len(tradeIDs))
	for _, tradeIDStr := range tradeIDs {
		tradeID, err := strconv.ParseUint(tradeIDStr, 10, 64)
		if err != nil {
			r.logger.Printf("Warning: invalid trade ID in recent trades list: %s", tradeIDStr)
			continue
		}

		trade, err := r.GetTrade(ctx, book, tradeID)
		if err != nil {
			r.logger.Printf("Warning: failed to get trade %d: %v", tradeID, err)
			continue
		}

		if trade != nil {
			trades = append(trades, trade)
		}
	}

	return trades, nil
}

// GetTradesByTimeRange retrieves trades within a time range
func (r *RedisCache) GetTradesByTimeRange(ctx context.Context, book string, start, end time.Time) ([]*models.TradeEvent, error) {
	// This is a simplified implementation
	// In a production system, you might want to use Redis Streams or a time-series database
	recentTrades, err := r.GetRecentTrades(ctx, book, r.config.MaxTradesPerBook)
	if err != nil {
		return nil, err
	}

	var filteredTrades []*models.TradeEvent
	for _, trade := range recentTrades {
		if trade.Timestamp.After(start) && trade.Timestamp.Before(end) {
			filteredTrades = append(filteredTrades, trade)
		}
	}

	return filteredTrades, nil
}

// SetOrderBook stores an order book in the cache
func (r *RedisCache) SetOrderBook(ctx context.Context, book string, orderBook *bitso.OrderBook) error {
	if orderBook == nil {
		return fmt.Errorf("order book cannot be nil")
	}

	key := r.orderBookKey(book)

	// Create snapshot
	snapshot := &OrderBookSnapshot{
		Book:      book,
		Bids:      orderBook.Bids,
		Asks:      orderBook.Asks,
		Timestamp: time.Now(),
	}

	// Serialize order book
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal order book: %w", err)
	}

	// Store order book with TTL
	if err := r.client.Set(ctx, key, data, r.config.OrderBookTTL).Err(); err != nil {
		return fmt.Errorf("failed to set order book: %w", err)
	}

	return nil
}

// GetOrderBook retrieves an order book from the cache
func (r *RedisCache) GetOrderBook(ctx context.Context, book string) (*bitso.OrderBook, error) {
	key := r.orderBookKey(book)

	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Order book not found
		}
		return nil, fmt.Errorf("failed to get order book: %w", err)
	}

	var snapshot OrderBookSnapshot
	if err := json.Unmarshal([]byte(data), &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal order book: %w", err)
	}

	// Convert snapshot to OrderBook
	orderBook := &bitso.OrderBook{
		Book: bitso.ToBook(book),
		Bids: snapshot.Bids,
		Asks: snapshot.Asks,
	}

	return orderBook, nil
}

// UpdateOrderBook updates the order book with a diff
func (r *RedisCache) UpdateOrderBook(ctx context.Context, book string, diff *bitso.WebSocketDiffOrder) error {
	if diff == nil {
		return fmt.Errorf("order book diff cannot be nil")
	}

	// Get current order book
	orderBook, err := r.GetOrderBook(ctx, book)
	if err != nil {
		return fmt.Errorf("failed to get current order book: %w", err)
	}

	// Apply diff (simplified implementation)
	// In a production system, you would implement proper order book diff application
	if orderBook == nil {
		// Create new order book if it doesn't exist
		orderBook = &bitso.OrderBook{
			Book: bitso.ToBook(book),
			Bids: []bitso.OrderBookLevel{},
			Asks: []bitso.OrderBookLevel{},
		}
	}

	// Store updated order book
	return r.SetOrderBook(ctx, book, orderBook)
}

// SetTicker stores a ticker in the cache
func (r *RedisCache) SetTicker(ctx context.Context, book string, ticker *bitso.Ticker) error {
	if ticker == nil {
		return fmt.Errorf("ticker cannot be nil")
	}

	key := r.tickerKey(book)

	// Serialize ticker
	data, err := json.Marshal(ticker)
	if err != nil {
		return fmt.Errorf("failed to marshal ticker: %w", err)
	}

	// Store ticker with TTL
	if err := r.client.Set(ctx, key, data, r.config.TickerTTL).Err(); err != nil {
		return fmt.Errorf("failed to set ticker: %w", err)
	}

	return nil
}

// GetTicker retrieves a ticker from the cache
func (r *RedisCache) GetTicker(ctx context.Context, book string) (*bitso.Ticker, error) {
	key := r.tickerKey(book)

	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Ticker not found
		}
		return nil, fmt.Errorf("failed to get ticker: %w", err)
	}

	var ticker bitso.Ticker
	if err := json.Unmarshal([]byte(data), &ticker); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ticker: %w", err)
	}

	return &ticker, nil
}

// SetTradeStats stores trade statistics in the cache
func (r *RedisCache) SetTradeStats(ctx context.Context, book string, stats *TradeStats) error {
	if stats == nil {
		return fmt.Errorf("trade stats cannot be nil")
	}

	key := r.tradeStatsKey(book)

	// Serialize stats
	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal trade stats: %w", err)
	}

	// Store stats with TTL
	if err := r.client.Set(ctx, key, data, r.config.StatsTTL).Err(); err != nil {
		return fmt.Errorf("failed to set trade stats: %w", err)
	}

	return nil
}

// GetTradeStats retrieves trade statistics from the cache
func (r *RedisCache) GetTradeStats(ctx context.Context, book string) (*TradeStats, error) {
	key := r.tradeStatsKey(book)

	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Stats not found
		}
		return nil, fmt.Errorf("failed to get trade stats: %w", err)
	}

	var stats TradeStats
	if err := json.Unmarshal([]byte(data), &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal trade stats: %w", err)
	}

	return &stats, nil
}

// Clear clears cache entries matching a pattern
func (r *RedisCache) Clear(ctx context.Context, pattern string) error {
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("failed to get keys: %w", err)
	}

	if len(keys) == 0 {
		return nil
	}

	if err := r.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("failed to delete keys: %w", err)
	}

	return nil
}

// Exists checks if a key exists in the cache
func (r *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	count, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check key existence: %w", err)
	}

	return count > 0, nil
}

// Expire sets expiration for a key
func (r *RedisCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if err := r.client.Expire(ctx, key, ttl).Err(); err != nil {
		return fmt.Errorf("failed to set expiration: %w", err)
	}

	return nil
}

// Close closes the Redis connection
func (r *RedisCache) Close() error {
	return r.client.Close()
}

// Helper methods for key generation
func (r *RedisCache) tradeKey(book string, tradeID uint64) string {
	return fmt.Sprintf("trade:%s:%d", book, tradeID)
}

func (r *RedisCache) recentTradesKey(book string) string {
	return fmt.Sprintf("recent_trades:%s", book)
}

func (r *RedisCache) orderBookKey(book string) string {
	return fmt.Sprintf("orderbook:%s", book)
}

func (r *RedisCache) tickerKey(book string) string {
	return fmt.Sprintf("ticker:%s", book)
}

func (r *RedisCache) tradeStatsKey(book string) string {
	return fmt.Sprintf("trade_stats:%s", book)
}

// addToRecentTrades adds a trade to the recent trades list
func (r *RedisCache) addToRecentTrades(ctx context.Context, book string, trade *models.TradeEvent) error {
	key := r.recentTradesKey(book)

	// Add trade ID to the list
	if err := r.client.LPush(ctx, key, trade.ID).Err(); err != nil {
		return fmt.Errorf("failed to add trade to recent trades list: %w", err)
	}

	// Trim list to max size
	if err := r.client.LTrim(ctx, key, 0, int64(r.config.MaxTradesPerBook-1)).Err(); err != nil {
		return fmt.Errorf("failed to trim recent trades list: %w", err)
	}

	// Set expiration
	if err := r.client.Expire(ctx, key, r.config.TradeTTL).Err(); err != nil {
		return fmt.Errorf("failed to set expiration for recent trades list: %w", err)
	}

	return nil
}

// updateTradeStats updates trade statistics
func (r *RedisCache) updateTradeStats(ctx context.Context, book string, trade *models.TradeEvent) error {
	// Get current stats
	stats, err := r.GetTradeStats(ctx, book)
	if err != nil {
		return fmt.Errorf("failed to get current trade stats: %w", err)
	}

	// Initialize stats if they don't exist
	if stats == nil {
		stats = &TradeStats{
			Book:      book,
			LowPrice:  trade.Price,
			HighPrice: trade.Price,
			UpdatedAt: time.Now(),
		}
	}

	// Update stats
	stats.TotalTrades++
	stats.TotalVolume += trade.Amount
	stats.TotalValue += trade.Value
	stats.LastPrice = trade.Price

	if trade.Price > stats.HighPrice {
		stats.HighPrice = trade.Price
	}
	if trade.Price < stats.LowPrice {
		stats.LowPrice = trade.Price
	}

	// Calculate VWAP
	if stats.TotalVolume > 0 {
		stats.VWAP = stats.TotalValue / stats.TotalVolume
	}

	// Calculate price change
	if stats.TotalTrades > 1 {
		stats.PriceChange = trade.Price - stats.LastPrice
		if stats.LastPrice > 0 {
			stats.PriceChangePercent = (stats.PriceChange / stats.LastPrice) * 100
		}
	}

	stats.UpdatedAt = time.Now()

	// Store updated stats
	return r.SetTradeStats(ctx, book, stats)
}
