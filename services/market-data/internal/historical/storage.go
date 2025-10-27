package historical

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"

	"github.com/redis/go-redis/v9"
)

// Storage defines the interface for historical data storage
type Storage interface {
	// Trade operations
	StoreTrade(ctx context.Context, trade *models.TradeEvent) error
	GetTradesByTimeRange(ctx context.Context, book string, start, end time.Time) ([]*models.TradeEvent, error)
	GetTradesByBook(ctx context.Context, book string, limit int) ([]*models.TradeEvent, error)
	GetTradeByID(ctx context.Context, book string, tradeID uint64) (*models.TradeEvent, error)

	// Order book operations
	StoreOrderBook(ctx context.Context, book string, orderBook *bitso.OrderBook) error
	GetOrderBookHistory(ctx context.Context, book string, start, end time.Time) ([]*OrderBookSnapshot, error)

	// Ticker operations
	StoreTicker(ctx context.Context, book string, ticker *bitso.Ticker) error
	GetTickerHistory(ctx context.Context, book string, start, end time.Time) ([]*bitso.Ticker, error)

	// Statistics operations
	GetTradeStatistics(ctx context.Context, book string, start, end time.Time) (*TradeStatistics, error)
	GetVolumeStatistics(ctx context.Context, book string, start, end time.Time) (*VolumeStatistics, error)

	// Data management
	CleanupOldData(ctx context.Context, olderThan time.Time) error
	GetStorageStats(ctx context.Context) (*StorageStats, error)
	Close() error
}

// OrderBookSnapshot represents a historical order book snapshot
type OrderBookSnapshot struct {
	Book      string                 `json:"book"`
	Bids      []bitso.OrderBookLevel `json:"bids"`
	Asks      []bitso.OrderBookLevel `json:"asks"`
	Timestamp time.Time              `json:"timestamp"`
	Source    string                 `json:"source"`
}

// TradeStatistics represents aggregated trade statistics
type TradeStatistics struct {
	Book               string    `json:"book"`
	StartTime          time.Time `json:"start_time"`
	EndTime            time.Time `json:"end_time"`
	TotalTrades        int64     `json:"total_trades"`
	TotalVolume        float64   `json:"total_volume"`
	TotalValue         float64   `json:"total_value"`
	AveragePrice       float64   `json:"average_price"`
	VWAP               float64   `json:"vwap"`
	HighPrice          float64   `json:"high_price"`
	LowPrice           float64   `json:"low_price"`
	PriceChange        float64   `json:"price_change"`
	PriceChangePercent float64   `json:"price_change_percent"`
	Volatility         float64   `json:"volatility"`
}

// VolumeStatistics represents volume-based statistics
type VolumeStatistics struct {
	Book          string        `json:"book"`
	StartTime     time.Time     `json:"start_time"`
	EndTime       time.Time     `json:"end_time"`
	TotalVolume   float64       `json:"total_volume"`
	AverageVolume float64       `json:"average_volume"`
	MaxVolume     float64       `json:"max_volume"`
	MinVolume     float64       `json:"min_volume"`
	VolumeProfile []VolumeLevel `json:"volume_profile"`
}

// VolumeLevel represents a volume level in the volume profile
type VolumeLevel struct {
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"`
}

// StorageStats represents storage statistics
type StorageStats struct {
	TotalTrades     int64     `json:"total_trades"`
	TotalOrderBooks int64     `json:"total_orderbooks"`
	TotalTickers    int64     `json:"total_tickers"`
	StorageSize     int64     `json:"storage_size_bytes"`
	LastCleanup     time.Time `json:"last_cleanup"`
	OldestData      time.Time `json:"oldest_data"`
	NewestData      time.Time `json:"newest_data"`
}

// StorageConfig holds configuration for historical data storage
type StorageConfig struct {
	// Storage backend configuration
	BackendType string `json:"backend_type"` // "redis", "postgres", "influxdb"

	// Data retention
	RetentionDays int `json:"retention_days"`

	// Batch processing
	BatchSize    int           `json:"batch_size"`
	BatchTimeout time.Duration `json:"batch_timeout"`

	// Compression
	EnableCompression bool `json:"enable_compression"`

	// Indexing
	EnableIndexing bool `json:"enable_indexing"`

	// Cleanup
	CleanupInterval time.Duration `json:"cleanup_interval"`
}

// DefaultStorageConfig returns default storage configuration
func DefaultStorageConfig() *StorageConfig {
	return &StorageConfig{
		BackendType:       "redis",
		RetentionDays:     30,
		BatchSize:         100,
		BatchTimeout:      1 * time.Second,
		EnableCompression: true,
		EnableIndexing:    true,
		CleanupInterval:   1 * time.Hour,
	}
}

// RedisStorage implements the Storage interface using Redis
type RedisStorage struct {
	client *redis.Client
	config *StorageConfig
	logger *log.Logger
}

// NewRedisStorage creates a new Redis storage instance
func NewRedisStorage(config *StorageConfig, logger *log.Logger) (*RedisStorage, error) {
	if config == nil {
		config = DefaultStorageConfig()
	}

	if logger == nil {
		logger = log.New(log.Writer(), "[HISTORICAL-STORAGE] ", log.LstdFlags|log.Lshortfile)
	}

	// Create Redis client (this would be injected in a real implementation)
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use different DB for historical data
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisStorage{
		client: client,
		config: config,
		logger: logger,
	}, nil
}

// StoreTrade stores a trade in historical storage
func (r *RedisStorage) StoreTrade(ctx context.Context, trade *models.TradeEvent) error {
	if trade == nil {
		return fmt.Errorf("trade cannot be nil")
	}

	// Create storage key with timestamp for time-based partitioning
	key := r.tradeKey(trade.Book, trade.Timestamp, trade.ID)

	// Serialize trade
	data, err := json.Marshal(trade)
	if err != nil {
		return fmt.Errorf("failed to marshal trade: %w", err)
	}

	// Store trade with retention period
	ttl := time.Duration(r.config.RetentionDays) * 24 * time.Hour
	if err := r.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to store trade: %w", err)
	}

	// Add to time-based index
	if r.config.EnableIndexing {
		if err := r.addToTimeIndex(ctx, trade); err != nil {
			r.logger.Printf("Warning: failed to add trade to time index: %v", err)
		}
	}

	return nil
}

// GetTradesByTimeRange retrieves trades within a time range
func (r *RedisStorage) GetTradesByTimeRange(ctx context.Context, book string, start, end time.Time) ([]*models.TradeEvent, error) {
	// Get trade keys from time index
	keys, err := r.getKeysFromTimeRange(ctx, book, start, end, "trade")
	if err != nil {
		return nil, fmt.Errorf("failed to get trade keys: %w", err)
	}

	if len(keys) == 0 {
		return []*models.TradeEvent{}, nil
	}

	// Get trade data
	trades := make([]*models.TradeEvent, 0, len(keys))
	for _, key := range keys {
		data, err := r.client.Get(ctx, key).Result()
		if err != nil {
			if err == redis.Nil {
				continue // Trade was deleted
			}
			return nil, fmt.Errorf("failed to get trade: %w", err)
		}

		var trade models.TradeEvent
		if err := json.Unmarshal([]byte(data), &trade); err != nil {
			r.logger.Printf("Warning: failed to unmarshal trade: %v", err)
			continue
		}

		trades = append(trades, &trade)
	}

	return trades, nil
}

// GetTradesByBook retrieves trades for a specific book
func (r *RedisStorage) GetTradesByBook(ctx context.Context, book string, limit int) ([]*models.TradeEvent, error) {
	// Get recent trades for the book
	pattern := fmt.Sprintf("trade:%s:*", book)
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get trade keys: %w", err)
	}

	if len(keys) == 0 {
		return []*models.TradeEvent{}, nil
	}

	// Limit results
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}

	// Get trade data
	trades := make([]*models.TradeEvent, 0, len(keys))
	for _, key := range keys {
		data, err := r.client.Get(ctx, key).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			return nil, fmt.Errorf("failed to get trade: %w", err)
		}

		var trade models.TradeEvent
		if err := json.Unmarshal([]byte(data), &trade); err != nil {
			r.logger.Printf("Warning: failed to unmarshal trade: %v", err)
			continue
		}

		trades = append(trades, &trade)
	}

	return trades, nil
}

// GetTradeByID retrieves a specific trade by ID
func (r *RedisStorage) GetTradeByID(ctx context.Context, book string, tradeID uint64) (*models.TradeEvent, error) {
	// Search for trade by ID (this is simplified - in production you'd have a proper index)
	pattern := fmt.Sprintf("trade:%s:*:%d", book, tradeID)
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to search for trade: %w", err)
	}

	if len(keys) == 0 {
		return nil, nil // Trade not found
	}

	// Get the first match
	data, err := r.client.Get(ctx, keys[0]).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get trade: %w", err)
	}

	var trade models.TradeEvent
	if err := json.Unmarshal([]byte(data), &trade); err != nil {
		return nil, fmt.Errorf("failed to unmarshal trade: %w", err)
	}

	return &trade, nil
}

// StoreOrderBook stores an order book snapshot
func (r *RedisStorage) StoreOrderBook(ctx context.Context, book string, orderBook *bitso.OrderBook) error {
	if orderBook == nil {
		return fmt.Errorf("order book cannot be nil")
	}

	// Create snapshot
	snapshot := &OrderBookSnapshot{
		Book:      book,
		Bids:      orderBook.Bids,
		Asks:      orderBook.Asks,
		Timestamp: time.Now(),
		Source:    "bitso_websocket",
	}

	// Create storage key
	key := r.orderBookKey(book, snapshot.Timestamp)

	// Serialize snapshot
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal order book snapshot: %w", err)
	}

	// Store snapshot with retention period
	ttl := time.Duration(r.config.RetentionDays) * 24 * time.Hour
	if err := r.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to store order book snapshot: %w", err)
	}

	return nil
}

// GetOrderBookHistory retrieves order book history
func (r *RedisStorage) GetOrderBookHistory(ctx context.Context, book string, start, end time.Time) ([]*OrderBookSnapshot, error) {
	// Get order book keys from time range
	keys, err := r.getKeysFromTimeRange(ctx, book, start, end, "orderbook")
	if err != nil {
		return nil, fmt.Errorf("failed to get order book keys: %w", err)
	}

	if len(keys) == 0 {
		return []*OrderBookSnapshot{}, nil
	}

	// Get order book data
	snapshots := make([]*OrderBookSnapshot, 0, len(keys))
	for _, key := range keys {
		data, err := r.client.Get(ctx, key).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			return nil, fmt.Errorf("failed to get order book snapshot: %w", err)
		}

		var snapshot OrderBookSnapshot
		if err := json.Unmarshal([]byte(data), &snapshot); err != nil {
			r.logger.Printf("Warning: failed to unmarshal order book snapshot: %v", err)
			continue
		}

		snapshots = append(snapshots, &snapshot)
	}

	return snapshots, nil
}

// StoreTicker stores a ticker snapshot
func (r *RedisStorage) StoreTicker(ctx context.Context, book string, ticker *bitso.Ticker) error {
	if ticker == nil {
		return fmt.Errorf("ticker cannot be nil")
	}

	// Create storage key
	key := r.tickerKey(book, time.Now())

	// Serialize ticker
	data, err := json.Marshal(ticker)
	if err != nil {
		return fmt.Errorf("failed to marshal ticker: %w", err)
	}

	// Store ticker with retention period
	ttl := time.Duration(r.config.RetentionDays) * 24 * time.Hour
	if err := r.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to store ticker: %w", err)
	}

	return nil
}

// GetTickerHistory retrieves ticker history
func (r *RedisStorage) GetTickerHistory(ctx context.Context, book string, start, end time.Time) ([]*bitso.Ticker, error) {
	// Get ticker keys from time range
	keys, err := r.getKeysFromTimeRange(ctx, book, start, end, "ticker")
	if err != nil {
		return nil, fmt.Errorf("failed to get ticker keys: %w", err)
	}

	if len(keys) == 0 {
		return []*bitso.Ticker{}, nil
	}

	// Get ticker data
	tickers := make([]*bitso.Ticker, 0, len(keys))
	for _, key := range keys {
		data, err := r.client.Get(ctx, key).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			return nil, fmt.Errorf("failed to get ticker: %w", err)
		}

		var ticker bitso.Ticker
		if err := json.Unmarshal([]byte(data), &ticker); err != nil {
			r.logger.Printf("Warning: failed to unmarshal ticker: %v", err)
			continue
		}

		tickers = append(tickers, &ticker)
	}

	return tickers, nil
}

// GetTradeStatistics calculates trade statistics for a time range
func (r *RedisStorage) GetTradeStatistics(ctx context.Context, book string, start, end time.Time) (*TradeStatistics, error) {
	trades, err := r.GetTradesByTimeRange(ctx, book, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to get trades: %w", err)
	}

	if len(trades) == 0 {
		return &TradeStatistics{
			Book:      book,
			StartTime: start,
			EndTime:   end,
		}, nil
	}

	// Calculate statistics
	stats := &TradeStatistics{
		Book:      book,
		StartTime: start,
		EndTime:   end,
		LowPrice:  trades[0].Price,
		HighPrice: trades[0].Price,
	}

	for _, trade := range trades {
		stats.TotalTrades++
		stats.TotalVolume += trade.Amount
		stats.TotalValue += trade.Value

		if trade.Price > stats.HighPrice {
			stats.HighPrice = trade.Price
		}
		if trade.Price < stats.LowPrice {
			stats.LowPrice = trade.Price
		}
	}

	// Calculate derived statistics
	if stats.TotalTrades > 0 {
		stats.AveragePrice = stats.TotalValue / stats.TotalVolume
		stats.VWAP = stats.TotalValue / stats.TotalVolume
	}

	if len(trades) > 1 {
		stats.PriceChange = trades[len(trades)-1].Price - trades[0].Price
		if trades[0].Price > 0 {
			stats.PriceChangePercent = (stats.PriceChange / trades[0].Price) * 100
		}
	}

	return stats, nil
}

// GetVolumeStatistics calculates volume statistics
func (r *RedisStorage) GetVolumeStatistics(ctx context.Context, book string, start, end time.Time) (*VolumeStatistics, error) {
	trades, err := r.GetTradesByTimeRange(ctx, book, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to get trades: %w", err)
	}

	if len(trades) == 0 {
		return &VolumeStatistics{
			Book:      book,
			StartTime: start,
			EndTime:   end,
		}, nil
	}

	stats := &VolumeStatistics{
		Book:      book,
		StartTime: start,
		EndTime:   end,
		MinVolume: trades[0].Amount,
		MaxVolume: trades[0].Amount,
	}

	// Calculate volume statistics
	volumeByPrice := make(map[float64]float64)
	for _, trade := range trades {
		stats.TotalVolume += trade.Amount
		volumeByPrice[trade.Price] += trade.Amount

		if trade.Amount > stats.MaxVolume {
			stats.MaxVolume = trade.Amount
		}
		if trade.Amount < stats.MinVolume {
			stats.MinVolume = trade.Amount
		}
	}

	stats.AverageVolume = stats.TotalVolume / float64(len(trades))

	// Create volume profile
	for price, volume := range volumeByPrice {
		stats.VolumeProfile = append(stats.VolumeProfile, VolumeLevel{
			Price:  price,
			Volume: volume,
		})
	}

	return stats, nil
}

// CleanupOldData removes data older than the specified time
func (r *RedisStorage) CleanupOldData(ctx context.Context, olderThan time.Time) error {
	// Get all keys
	keys, err := r.client.Keys(ctx, "*").Result()
	if err != nil {
		return fmt.Errorf("failed to get keys: %w", err)
	}

	deletedCount := 0
	for _, key := range keys {
		// Check if key is older than threshold
		if r.isKeyOlderThan(key, olderThan) {
			if err := r.client.Del(ctx, key).Err(); err != nil {
				r.logger.Printf("Warning: failed to delete old key %s: %v", key, err)
				continue
			}
			deletedCount++
		}
	}

	r.logger.Printf("Cleaned up %d old entries", deletedCount)
	return nil
}

// GetStorageStats returns storage statistics
func (r *RedisStorage) GetStorageStats(ctx context.Context) (*StorageStats, error) {
	// Get all keys
	keys, err := r.client.Keys(ctx, "*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get keys: %w", err)
	}

	stats := &StorageStats{
		TotalTrades:     0,
		TotalOrderBooks: 0,
		TotalTickers:    0,
	}

	var oldestTime, newestTime time.Time
	first := true

	for _, key := range keys {
		// Count by type
		if strings.HasPrefix(key, "trade:") {
			stats.TotalTrades++
		} else if strings.HasPrefix(key, "orderbook:") {
			stats.TotalOrderBooks++
		} else if strings.HasPrefix(key, "ticker:") {
			stats.TotalTickers++
		}

		// Get timestamp from key
		if timestamp := r.extractTimestampFromKey(key); !timestamp.IsZero() {
			if first || timestamp.Before(oldestTime) {
				oldestTime = timestamp
			}
			if first || timestamp.After(newestTime) {
				newestTime = timestamp
			}
			first = false
		}
	}

	stats.OldestData = oldestTime
	stats.NewestData = newestTime
	stats.StorageSize = int64(len(keys)) // Simplified size calculation

	return stats, nil
}

// Close closes the storage connection
func (r *RedisStorage) Close() error {
	return r.client.Close()
}

// Helper methods
func (r *RedisStorage) tradeKey(book string, timestamp time.Time, tradeID uint64) string {
	return fmt.Sprintf("trade:%s:%d:%d", book, timestamp.Unix(), tradeID)
}

func (r *RedisStorage) orderBookKey(book string, timestamp time.Time) string {
	return fmt.Sprintf("orderbook:%s:%d", book, timestamp.Unix())
}

func (r *RedisStorage) tickerKey(book string, timestamp time.Time) string {
	return fmt.Sprintf("ticker:%s:%d", book, timestamp.Unix())
}

func (r *RedisStorage) addToTimeIndex(ctx context.Context, trade *models.TradeEvent) error {
	// Add to time-based index for efficient range queries
	indexKey := fmt.Sprintf("time_index:trade:%s:%d", trade.Book, trade.Timestamp.Unix())
	return r.client.Set(ctx, indexKey, trade.ID, 0).Err()
}

func (r *RedisStorage) getKeysFromTimeRange(ctx context.Context, book string, start, end time.Time, dataType string) ([]string, error) {
	// This is a simplified implementation
	// In production, you'd use Redis Streams or a proper time-series database
	pattern := fmt.Sprintf("%s:%s:*", dataType, book)
	return r.client.Keys(ctx, pattern).Result()
}

func (r *RedisStorage) isKeyOlderThan(key string, threshold time.Time) bool {
	// Extract timestamp from key and compare
	timestamp := r.extractTimestampFromKey(key)
	return !timestamp.IsZero() && timestamp.Before(threshold)
}

func (r *RedisStorage) extractTimestampFromKey(key string) time.Time {
	// Extract timestamp from key format: "type:book:timestamp:..."
	parts := strings.Split(key, ":")
	if len(parts) >= 3 {
		if timestamp, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
			return time.Unix(timestamp, 0)
		}
	}
	return time.Time{}
}
