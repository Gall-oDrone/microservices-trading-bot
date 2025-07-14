package trade

import (
	"fmt"
	"log"
	"net/url"
	"time"

	"bitso_trading_bot/internal/database"
	"bitso_trading_bot/pkg/bitso"
)

// Manager handles trade operations
type Manager struct {
	bitsoClient *bitso.Client
	dbClient    database.RedisClient
	book        *bitso.Book
	tradeQueue  []string // Queue of trade IDs
}

// NewManager creates a new trade manager instance
func NewManager(bitsoClient *bitso.Client, dbClient database.RedisClient, book *bitso.Book) *Manager {
	return &Manager{
		bitsoClient: bitsoClient,
		dbClient:    dbClient,
		book:        book,
		tradeQueue:  make([]string, 0),
	}
}

// Getter methods

// GetBitsoClient returns the bitso client
func (m *Manager) GetBitsoClient() *bitso.Client {
	return m.bitsoClient
}

// GetBitsoBook returns the bitso book
func (m *Manager) GetBitsoBook() *bitso.Book {
	return m.book
}

// GetDBClient returns the db client
func (m *Manager) GetDBClient() *database.RedisClient {
	return &m.dbClient
}

// Trade operations

// SaveTrade saves a trade to the database
func (m *Manager) SaveTrade(trade *bitso.UserTrade) error {
	err := m.dbClient.SaveTrade(trade)
	if err != nil {
		log.Printf("error saving trade: %v", err)
		return err
	}
	return nil
}

// GetUserTrade retrieves a specific trade from the database
func (m *Manager) GetUserTrade(tradeID string) (bitso.UserTrade, error) {
	return m.dbClient.GetUserTrade(tradeID)
}

// GetAllTrades retrieves all trades from the database
func (m *Manager) GetAllTrades() ([]*bitso.UserTrade, error) {
	// This would need to be implemented in the database layer
	// For now, we'll return an empty slice
	return []*bitso.UserTrade{}, nil
}

// GetTradesByFilter retrieves trades by side or other criteria
func (m *Manager) GetTradesByFilter(key, value string) ([]*bitso.UserTrade, error) {
	// This would need to be implemented in the database layer
	// For now, we'll return an empty slice
	return []*bitso.UserTrade{}, nil
}

// Bitso API operations

// GetMyTrades retrieves user trades from Bitso
func (m *Manager) GetMyTrades() ([]bitso.UserTrade, error) {
	params := url.Values{}
	params.Add("book", m.book.String())
	return m.bitsoClient.MyTrades(params)
}

// GetOrderTrades retrieves trades for a specific order from Bitso
func (m *Manager) GetOrderTrades(orderID string) ([]bitso.UserOrderTrade, error) {
	params := url.Values{}
	params.Add("book", m.book.String())
	return m.bitsoClient.OrderTrades(orderID, params)
}

// GetRecentTrades retrieves recent trades from Bitso
func (m *Manager) GetRecentTrades(limit int) ([]bitso.Trade, error) {
	params := url.Values{}
	params.Add("book", m.book.String())
	if limit > 0 {
		params.Add("limit", fmt.Sprintf("%d", limit))
	}
	return m.bitsoClient.Trades(params)
}

// SaveTradeWithTTL sets a trade with a time-to-live in the database
func (m *Manager) SaveTradeWithTTL(trade *bitso.UserTrade, ttl time.Duration) error {
	// This would need to be implemented in the database layer
	// For now, we'll just save the trade normally
	return m.SaveTrade(trade)
}

// Batch trade operations

// SaveBatchTrade saves a batch trade to the database
func (m *Manager) SaveBatchTrade(batch interface{}) error {
	// This would need to be implemented based on the specific batch type
	// For now, we'll return an error indicating it's not implemented
	return fmt.Errorf("batch trade saving not implemented")
}

// GetLastNthBatches retrieves the last N batch trades
func (m *Manager) GetLastNthBatches(n int) ([]interface{}, error) {
	// This would need to be implemented based on the specific batch type
	// For now, we'll return an empty slice
	return []interface{}{}, nil
}

// WebSocket trade operations

// SaveWebSocketTrade saves a websocket trade record
func (m *Manager) SaveWebSocketTrade(trade *bitso.WebSocketTrade) error {
	return m.dbClient.InsertTradeRecord(trade)
}

// GetLatestWebSocketTrade retrieves the latest websocket trade record
func (m *Manager) GetLatestWebSocketTrade() (*bitso.WebSocketTrade, error) {
	return m.dbClient.GetLatestTradeRecord()
}

// GetWebSocketTradesByTimestampRange retrieves websocket trades within a timestamp range
func (m *Manager) GetWebSocketTradesByTimestampRange(start, end uint64) ([]*bitso.WebSocketTrade, error) {
	return m.dbClient.GetTradeRecordsByTimestampRange(start, end)
}

// DeleteAllWebSocketTrades deletes all websocket trade records
func (m *Manager) DeleteAllWebSocketTrades() error {
	return m.dbClient.DeleteAllWSTradeRecords()
}

// Trade analysis operations

// GetTradeStatistics calculates basic statistics for trades
func (m *Manager) GetTradeStatistics() (map[string]interface{}, error) {
	trades, err := m.GetMyTrades()
	if err != nil {
		return nil, fmt.Errorf("failed to get trades for statistics: %w", err)
	}

	if len(trades) == 0 {
		return map[string]interface{}{
			"total_trades": 0,
			"total_volume": 0.0,
			"avg_price":    0.0,
		}, nil
	}

	var totalVolume float64
	var totalValue float64
	buyCount := 0
	sellCount := 0

	for _, trade := range trades {
		volume := trade.Major.Float64()
		price := trade.Price.Float64()
		value := volume * price

		totalVolume += volume
		totalValue += value

		if trade.Side == bitso.OrderSideBuy {
			buyCount++
		} else if trade.Side == bitso.OrderSideSell {
			sellCount++
		}
	}

	avgPrice := 0.0
	if totalVolume > 0 {
		avgPrice = totalValue / totalVolume
	}

	return map[string]interface{}{
		"total_trades": len(trades),
		"buy_trades":   buyCount,
		"sell_trades":  sellCount,
		"total_volume": totalVolume,
		"total_value":  totalValue,
		"avg_price":    avgPrice,
	}, nil
}

// GetTradesBySide retrieves trades filtered by side (buy/sell)
func (m *Manager) GetTradesBySide(side bitso.OrderSide) ([]bitso.UserTrade, error) {
	trades, err := m.GetMyTrades()
	if err != nil {
		return nil, err
	}

	var filteredTrades []bitso.UserTrade
	for _, trade := range trades {
		if trade.Side == side {
			filteredTrades = append(filteredTrades, trade)
		}
	}

	return filteredTrades, nil
}

// GetTradesByDateRange retrieves trades within a date range
func (m *Manager) GetTradesByDateRange(start, end time.Time) ([]bitso.UserTrade, error) {
	trades, err := m.GetMyTrades()
	if err != nil {
		return nil, err
	}

	var filteredTrades []bitso.UserTrade
	for _, trade := range trades {
		tradeTime := time.Time(trade.CreatedAt)
		if tradeTime.After(start) && tradeTime.Before(end) {
			filteredTrades = append(filteredTrades, trade)
		}
	}

	return filteredTrades, nil
}

// Queue Operations

// AppendToQueue adds a trade ID to the queue
func (m *Manager) AppendToQueue(tradeID string) error {
	if tradeID == "" {
		return fmt.Errorf("trade ID cannot be empty")
	}
	m.tradeQueue = append(m.tradeQueue, tradeID)
	return nil
}

// RemoveFromQueue removes a trade ID from the queue
func (m *Manager) RemoveFromQueue(tradeID string) error {
	if tradeID == "" {
		return fmt.Errorf("trade ID cannot be empty")
	}
	for i, id := range m.tradeQueue {
		if id == tradeID {
			m.tradeQueue = append(m.tradeQueue[:i], m.tradeQueue[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("trade ID %s not found in queue", tradeID)
}

// GetQueueTrades returns the list of trades in the queue
func (m *Manager) GetQueueTrades() []string {
	return m.tradeQueue
}

// ClearQueue clears all trades from the queue
func (m *Manager) ClearQueue() {
	m.tradeQueue = make([]string, 0)
}
