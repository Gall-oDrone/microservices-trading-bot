package database

import (
	"context"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
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
	GetOrdersBySide(side string) (map[string]string, error)
	GetOrdersByStatus(status string) (map[string]string, error)
	SetOrderWithTTL(oid string, timeout time.Duration) error

	// User trade operations
	SaveTrade(trade *bitso.UserTrade) error
	GetUserTrade(tradeId string) (bitso.UserTrade, error)

	// Balance operations
	SaveUserBalance(balance *bitso.Balance) error
	GetUserBalance(currency string) (bitso.Balance, error)

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
