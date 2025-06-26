package database

import (
	"time"

	"bitso_trading_bot/internal/queue"
	"bitso_trading_bot/pkg/bitso"
)

// DatabaseClient defines the interface for database operations
type DatabaseClient interface {
	GetUserBalance(currency string) (bitso.Balance, error)
	SaveUserBalance(balance *bitso.Balance) error
	GetUserOrderById(orderId string) (bitso.UserOrder, error)
	SaveUserOrder(order bitso.UserOrder) error
	GetAllUserOrders() ([]bitso.UserOrder, error)
	DeleteAllUserOrders() error
	SaveTrade(trade *bitso.UserTrade) error
	GetUserTrade(tradeId string) (bitso.UserTrade, error)
	SaveBatchTrade(batch queue.BidTradeTrendConsumer) error
	GetLastNthBatches(n int) ([]queue.BidTradeTrendConsumer, error)
	InsertTradeRecord(trade *bitso.WebSocketTrade) error
	GetLatestTradeRecord() (bitso.WebSocketTrade, error)
	GetTradeRecordsByTimestampRange(start, end uint64) ([]bitso.WebSocketTrade, error)
	DeleteAllWSTradeRecords() error
	GetKeysMatchingPattern(pattern string) ([]string, error)
	DeleteKafkaBatchKeysByPattern(pattern string) error
	SetOrderWithTTL(oid string, timeout time.Duration) error
	GetOrdersByStatus(status string) (map[string]string, error)
	GetOrdersBySide(side string) (map[string]string, error)
}
