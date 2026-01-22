package cache

import (
	"context"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
)

// Cache defines the interface for caching market data
type Cache interface {
	// Trade operations
	SetTrade(ctx context.Context, book string, trade *models.TradeEvent) error
	GetTrade(ctx context.Context, book string, tradeID uint64) (*models.TradeEvent, error)
	GetRecentTrades(ctx context.Context, book string, limit int) ([]*models.TradeEvent, error)
	GetTradesByTimeRange(ctx context.Context, book string, start, end time.Time) ([]*models.TradeEvent, error)

	// Order book operations
	SetOrderBook(ctx context.Context, book string, orderBook *bitso.OrderBook) error
	GetOrderBook(ctx context.Context, book string) (*bitso.OrderBook, error)
	UpdateOrderBook(ctx context.Context, book string, diff *bitso.WebSocketDiffOrder) error

	// Ticker operations
	SetTicker(ctx context.Context, book string, ticker *bitso.Ticker) error
	GetTicker(ctx context.Context, book string) (*bitso.Ticker, error)

	// Statistics operations
	SetTradeStats(ctx context.Context, book string, stats *TradeStats) error
	GetTradeStats(ctx context.Context, book string) (*TradeStats, error)

	// Cache management
	Clear(ctx context.Context, pattern string) error
	Exists(ctx context.Context, key string) (bool, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Close() error
}

// TradeStats represents aggregated trade statistics
type TradeStats struct {
	Book               string    `json:"book"`
	TotalTrades        int64     `json:"total_trades"`
	TotalVolume        float64   `json:"total_volume"`
	TotalValue         float64   `json:"total_value"`
	LastPrice          float64   `json:"last_price"`
	HighPrice          float64   `json:"high_price"`
	LowPrice           float64   `json:"low_price"`
	VWAP               float64   `json:"vwap"`
	PriceChange        float64   `json:"price_change"`
	PriceChangePercent float64   `json:"price_change_percent"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// OrderBookSnapshot represents a cached order book snapshot
type OrderBookSnapshot struct {
	Book      string        `json:"book"`
	Bids      []bitso.Order `json:"bids"`
	Asks      []bitso.Order `json:"asks"`
	Timestamp time.Time     `json:"timestamp"`
}

// CacheConfig holds configuration for the cache layer
type CacheConfig struct {
	// Redis configuration
	RedisHost     string `json:"redis_host"`
	RedisPort     string `json:"redis_port"`
	RedisPassword string `json:"redis_password"`
	RedisDB       int    `json:"redis_db"`

	// Cache TTLs
	TradeTTL     time.Duration `json:"trade_ttl"`
	OrderBookTTL time.Duration `json:"orderbook_ttl"`
	TickerTTL    time.Duration `json:"ticker_ttl"`
	StatsTTL     time.Duration `json:"stats_ttl"`

	// Cache sizes
	MaxTradesPerBook   int `json:"max_trades_per_book"`
	MaxOrderBookLevels int `json:"max_orderbook_levels"`

	// Performance settings
	BatchSize    int           `json:"batch_size"`
	BatchTimeout time.Duration `json:"batch_timeout"`
	PoolSize     int           `json:"pool_size"`
	PoolTimeout  time.Duration `json:"pool_timeout"`
}

// DefaultCacheConfig returns default cache configuration
func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		RedisHost:     "localhost",
		RedisPort:     "6379",
		RedisPassword: "",
		RedisDB:       0,

		TradeTTL:     5 * time.Minute,
		OrderBookTTL: 2 * time.Second,
		TickerTTL:    1 * time.Second,
		StatsTTL:     1 * time.Minute,

		MaxTradesPerBook:   1000,
		MaxOrderBookLevels: 50,

		BatchSize:    100,
		BatchTimeout: 100 * time.Millisecond,
		PoolSize:     10,
		PoolTimeout:  30 * time.Second,
	}
}
