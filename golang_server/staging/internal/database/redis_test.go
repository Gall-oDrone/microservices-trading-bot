package database

import (
	"testing"
	"time"

	"bitso_trading_bot/internal/queue"
	"bitso_trading_bot/pkg/bitso"

	"github.com/stretchr/testify/assert"
)

// setupTestRedis creates a test Redis client
func setupTestRedis(t *testing.T) *RedisClient {
	cfg := &Config{
		Host:     "localhost",
		Port:     6379,
		Password: "",
		DB:       0,
		PoolSize: 10,
		Timeout:  5 * time.Second,
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}

	// Clean up any existing test data
	client.DeleteAllUserOrders()
	client.DeleteAllWSTradeRecords()

	return client
}

// createDummyTicker creates a dummy ticker for testing
func createDummyTicker() bitso.Ticker {
	return bitso.Ticker{
		Book:      *bitso.NewBook(bitso.BTC, bitso.USD),
		High:      bitso.ToMonetary(50000.00),
		Last:      bitso.ToMonetary(49000.00),
		Volume:    bitso.ToMonetary(100.5),
		Vwap:      bitso.ToMonetary(49500.00),
		Low:       bitso.ToMonetary(48000.00),
		Ask:       bitso.ToMonetary(49100.00),
		Bid:       bitso.ToMonetary(48900.00),
		CreatedAt: bitso.Time(time.Now()),
	}
}

// createDummyUserOrder creates a dummy user order for testing
func createDummyUserOrder() bitso.UserOrder {
	return bitso.UserOrder{
		OID:            "test-order-1",
		Book:           *bitso.NewBook(bitso.BTC, bitso.USD),
		Side:           bitso.OrderSideBuy,
		Type:           "limit",
		Price:          bitso.ToMonetary(49000.00),
		OriginalAmount: bitso.ToMonetary(0.1),
		UnfilledAmount: bitso.ToMonetary(0.1),
		OriginalValue:  bitso.ToMonetary(4900.00),
		Status:         bitso.OrderStatusOpen,
		CreatedAt:      bitso.Time(time.Now()),
		UpdatedAt:      bitso.Time(time.Now()),
	}
}

// createDummyUserTrade creates a dummy user trade for testing
func createDummyUserTrade() *bitso.UserTrade {
	return &bitso.UserTrade{
		OID:       "test-trade-1",
		Book:      *bitso.NewBook(bitso.BTC, bitso.USD),
		Side:      bitso.OrderSideBuy,
		MakerSide: bitso.OrderSideSell,
		Major:     bitso.ToMonetary(0.1),
		Price:     bitso.ToMonetary(49000.00),
		CreatedAt: bitso.Time(time.Now()),
	}
}

// createDummyBatchTrade creates a dummy batch trade for testing
func createDummyBatchTrade() queue.BidTradeTrendConsumer {
	return queue.BidTradeTrendConsumer{
		BatchId:     1,
		Index:       0,
		WindowStart: time.Now().Add(-time.Minute * 5),
		WindowEnd:   time.Now(),
	}
}

// createDummyWebsocketTrade creates a dummy websocket trade for testing
func createDummyWebsocketTrade() *bitso.WebSocketTrade {
	return &bitso.WebSocketTrade{
		Book: *bitso.NewBook(bitso.BTC, bitso.USD),
		Payload: []struct {
			TID               uint64         `json:"i"`
			Amount            bitso.Monetary `json:"a"`
			Price             bitso.Monetary `json:"r"`
			Value             bitso.Monetary `json:"v"`
			MakerSide         string         `json:"t"`
			CreationTimestamp uint64         `json:"x"`
			MakerOrderID      string         `json:"mo"`
			TakerOrderID      string         `json:"to"`
		}{
			{
				TID:               123456,
				Amount:            bitso.ToMonetary(0.1),
				Price:             bitso.ToMonetary(49000.00),
				Value:             bitso.ToMonetary(4900.00),
				MakerOrderID:      "maker-oid",
				TakerOrderID:      "taker-oid",
				MakerSide:         "sell",
				CreationTimestamp: uint64(time.Now().UnixNano()),
			},
		},
		Sent: uint64(time.Now().UnixNano()),
	}
}

func TestRedisClient_TickerOperations(t *testing.T) {
	client := setupTestRedis(t)
	defer client.Close()

	ticker := createDummyTicker()

	// Test SaveTicker
	err := client.SaveTicker(ticker)
	assert.NoError(t, err)

	// Test GetTicker
	retrieved, err := client.GetTicker()
	assert.NoError(t, err)
	assert.Equal(t, ticker.Book.String(), retrieved.Book.String())
	assert.Equal(t, string(ticker.High), string(retrieved.High))
	assert.Equal(t, string(ticker.Last), string(retrieved.Last))

	// Compare time values using the Equal method
	assert.True(t, ticker.CreatedAt.Equal(retrieved.CreatedAt),
		"Expected CreatedAt times to be equal. Got: %v, Want: %v",
		retrieved.CreatedAt, ticker.CreatedAt)
}

func TestRedisClient_UserOrderOperations(t *testing.T) {
	client := setupTestRedis(t)
	defer client.Close()

	order := createDummyUserOrder()

	// Test SaveUserOrder
	err := client.SaveUserOrder(order)
	assert.NoError(t, err)

	// Test GetUserOrderById
	retrieved, err := client.GetUserOrderById(order.OID)
	assert.NoError(t, err)
	assert.Equal(t, order.OID, retrieved.OID)
	assert.Equal(t, order.Book.String(), retrieved.Book.String())
	assert.Equal(t, order.Side, retrieved.Side)
	assert.Equal(t, order.Type, retrieved.Type)
	assert.Equal(t, string(order.Price), string(retrieved.Price))
	assert.Equal(t, string(order.OriginalAmount), string(retrieved.OriginalAmount))
	assert.Equal(t, order.Status, retrieved.Status)

	// Test GetAllUserOrders
	orders, err := client.GetAllUserOrders()
	assert.NoError(t, err)
	assert.Len(t, orders, 1)
	assert.Equal(t, order.OID, orders[0].OID)

	// Test DeleteAllUserOrders
	err = client.DeleteAllUserOrders()
	assert.NoError(t, err)

	orders, err = client.GetAllUserOrders()
	assert.NoError(t, err)
	assert.Empty(t, orders)
}

func TestRedisClient_TradeOperations(t *testing.T) {
	client := setupTestRedis(t)
	defer client.Close()

	trade := createDummyUserTrade()

	// Test SaveTrade
	err := client.SaveTrade(trade)
	assert.NoError(t, err)

	// Test GetUserTrade
	retrieved, err := client.GetUserTrade(trade.OID)
	assert.NoError(t, err)
	assert.Equal(t, trade.OID, retrieved.OID)
	assert.Equal(t, trade.Book.String(), retrieved.Book.String())
	assert.Equal(t, trade.Side, retrieved.Side)
	assert.Equal(t, string(trade.Major), string(retrieved.Major))
	assert.Equal(t, string(trade.Price), string(retrieved.Price))
}

func TestRedisClient_BatchTradeOperations(t *testing.T) {
	client := setupTestRedis(t)
	defer client.Close()

	batch := createDummyBatchTrade()

	// Test SaveBatchTrade
	err := client.SaveBatchTrade(batch)
	assert.NoError(t, err)

	// Test GetLastNthBatches
	batches, err := client.GetLastNthBatches(1)
	assert.NoError(t, err)
	assert.Len(t, batches, 1)
	assert.Equal(t, batch.BatchId, batches[0].BatchId)
	assert.Equal(t, batch.Index, batches[0].Index)
	assert.WithinDuration(t, batch.WindowStart, batches[0].WindowStart, time.Second)
	assert.WithinDuration(t, batch.WindowEnd, batches[0].WindowEnd, time.Second)
}

func TestRedisClient_WebsocketTradeOperations(t *testing.T) {
	client := setupTestRedis(t)
	defer client.Close()

	trade := createDummyWebsocketTrade()

	// Test InsertTradeRecord
	err := client.InsertTradeRecord(trade)
	assert.NoError(t, err)

	// Test GetLatestTradeRecord
	retrieved, err := client.GetLatestTradeRecord()
	assert.NoError(t, err)
	assert.Equal(t, trade.Book.String(), retrieved.Book.String())
	assert.Equal(t, trade.Sent, retrieved.Sent)
	if len(trade.Payload) > 0 && len(retrieved.Payload) > 0 {
		assert.Equal(t, trade.Payload[0].TID, retrieved.Payload[0].TID)
		assert.Equal(t, string(trade.Payload[0].Amount), string(retrieved.Payload[0].Amount))
		assert.Equal(t, string(trade.Payload[0].Price), string(retrieved.Payload[0].Price))
		assert.Equal(t, string(trade.Payload[0].Value), string(retrieved.Payload[0].Value))
		assert.Equal(t, trade.Payload[0].MakerOrderID, retrieved.Payload[0].MakerOrderID)
		assert.Equal(t, trade.Payload[0].TakerOrderID, retrieved.Payload[0].TakerOrderID)
		assert.Equal(t, trade.Payload[0].MakerSide, retrieved.Payload[0].MakerSide)
	}

	// Test GetTradeRecordsByTimestampRange
	start := uint64(time.Now().Add(-time.Hour).UnixNano())
	end := uint64(time.Now().Add(time.Hour).UnixNano())
	trades, err := client.GetTradeRecordsByTimestampRange(start, end)
	assert.NoError(t, err)
	assert.Len(t, trades, 1)
	assert.Equal(t, trade.Book.String(), trades[0].Book.String())
	assert.Equal(t, trade.Sent, trades[0].Sent)
	if len(trade.Payload) > 0 && len(trades[0].Payload) > 0 {
		assert.Equal(t, trade.Payload[0].TID, trades[0].Payload[0].TID)
		assert.Equal(t, string(trade.Payload[0].Amount), string(trades[0].Payload[0].Amount))
		assert.Equal(t, string(trade.Payload[0].Price), string(trades[0].Payload[0].Price))
		assert.Equal(t, string(trade.Payload[0].Value), string(trades[0].Payload[0].Value))
		assert.Equal(t, trade.Payload[0].MakerOrderID, trades[0].Payload[0].MakerOrderID)
		assert.Equal(t, trade.Payload[0].TakerOrderID, trades[0].Payload[0].TakerOrderID)
		assert.Equal(t, trade.Payload[0].MakerSide, trades[0].Payload[0].MakerSide)
	}

	// Test DeleteAllWSTradeRecords
	err = client.DeleteAllWSTradeRecords()
	assert.NoError(t, err)

	trades, err = client.GetTradeRecordsByTimestampRange(start, end)
	assert.NoError(t, err)
	assert.Empty(t, trades)
}

func TestRedisClient_KeyManagementOperations(t *testing.T) {
	client := setupTestRedis(t)
	defer client.Close()

	// Save some test data
	batch := createDummyBatchTrade()
	err := client.SaveBatchTrade(batch)
	assert.NoError(t, err)

	// Test GetKeysMatchingPattern
	keys, err := client.GetKeysMatchingPattern("batch:*")
	assert.NoError(t, err)
	assert.Len(t, keys, 1)
	assert.Contains(t, keys[0], "batch:")
}
