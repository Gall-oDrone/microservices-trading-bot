package database

import (
	"context"

	"bitso_trading_bot/internal/queue"
	"bitso_trading_bot/pkg/bitso"
)

// Client defines the interface for database operations
type Client interface {
	// Ticker operations
	SaveTicker(ticker bitso.Ticker) error
	GetTicker() (bitso.Ticker, error)

	// User order operations
	SaveUserOrder(order bitso.UserOrder) error
	GetUserOrderById(orderId string) (bitso.UserOrder, error)
	GetAllUserOrders() ([]bitso.UserOrder, error)
	DeleteAllUserOrders() error

	// Trade operations
	SaveTrade(trade *bitso.UserTrade) error
	GetUserTrade(tradeId string) (bitso.UserTrade, error)

	// Batch trade operations
	SaveBatchTrade(batch queue.BidTradeTrendConsumer) error
	GetLastNthBatches(n int) ([]queue.BidTradeTrendConsumer, error)

	// WebSocket Trade Operations
	InsertTradeRecord(trade *bitso.WebSocketTrade) error
	GetLatestTradeRecord() (*bitso.WebSocketTrade, error)
	GetTradeRecordsByTimestampRange(start, end uint64) ([]*bitso.WebSocketTrade, error)
	DeleteAllWSTradeRecords() error

	// Key management operations
	GetKeysMatchingPattern(pattern string) ([]string, error)
	DeleteKafkaBatchKeysByPattern(pattern string) error

	// Connection management
	Close() error
	Ping(ctx context.Context) error
}
