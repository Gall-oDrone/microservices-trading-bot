package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/segmentio/kafka-go/example/consumer-logger/queue"

	// "command-line-arguments/Users/diegogallovalenzuela/microservices-trading-bot/golang_server/consumer-logger/simulation/simulation.go"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
	// "github.com/xiam/bitso-go/bitso"
)

type RedisClient struct {
	client *redis.Client
	ctxbg  context.Context
}

func NewClient(rdb *redis.Client) *RedisClient {
	return &RedisClient{
		client: rdb,
	}
}

func SetupRedis() (*RedisClient, error) {
	ctx := context.Background()
	// Create a new Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // Replace with your Redis server address
		Password: "",               // Replace with your Redis password
		DB:       0,                // Replace with your Redis database index
	})
	rc := NewClient(rdb)
	rc.ctxbg = ctx
	// Ping the Redis server to check the connection
	pong, err := rc.client.Ping(ctx).Result()
	if err != nil {
		return rc, fmt.Errorf("Failed to connect to Redis: %v", err)
	}
	fmt.Println("Connected to Redis:", pong)

	return rc, nil
}
func (rc *RedisClient) CloseDB() error {
	// Close the Redis client connection when done
	err := rc.client.Close()
	if err != nil {
		log.Fatalf("Failed to close Redis connection: %v", err)
		return err
	}
	fmt.Println("Redis connection closed")
	return nil
}

func (rc *RedisClient) SaveTicker(ticker bitso.Ticker) error {
	// Convert the Ticker struct to JSON
	tickerJSON, err := json.Marshal(ticker)
	if err != nil {
		return err
	}

	// Use SET command to store the JSON data in Redis
	err = rc.client.Set(rc.ctxbg, "ticker", tickerJSON, 0).Err()
	if err != nil {
		return err
	}

	return nil
}

func (rc *RedisClient) GetTicker() (bitso.Ticker, error) {
	var ticker bitso.Ticker
	// Retrieve the JSON data from Redis
	tickerJSON, err := rc.client.Get(rc.ctxbg, "ticker_data").Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key does not exist in Redis
			return ticker, errors.New("ticker data not found in Redis")
		}
		// Other error occurred
		return ticker, err
	}

	// Unmarshal the JSON data into a Ticker struct
	err = json.Unmarshal([]byte(tickerJSON), &ticker)
	if err != nil {
		return ticker, err
	}

	return ticker, nil
}

func (rc *RedisClient) SaveUserOrderTrade(order *bitso.UserTrade) error {
	// Convert the Ticker struct to JSON
	userOrderTradedJSON, err := json.Marshal(order)
	if err != nil {
		return err
	}

	// Use SET command to store the JSON data in Redis
	err = rc.client.Set(rc.ctxbg, "order_trade", userOrderTradedJSON, 0).Err()
	if err != nil {
		return err
	}

	return nil
}

func (rc *RedisClient) SaveUserBalance(balance *bitso.Balance) error {
	// Convert the Ticker struct to JSON
	userBalanceSON, err := json.Marshal(balance)
	if err != nil {
		return err
	}
	cb := fmt.Sprintf("%s_balance", balance.Currency.String())
	// Use SET command to store the JSON data in Redis
	err = rc.client.Set(rc.ctxbg, cb, userBalanceSON, 0).Err()
	if err != nil {
		return err
	}

	return nil
}

func (rc *RedisClient) PutUserBalance(balance *bitso.Balance) error {
	// Convert the Ticker struct to JSON
	userBalanceSON, err := json.Marshal(balance)
	if err != nil {
		return err
	}

	cb := fmt.Sprintf("%s_balance", balance.Currency.String())
	// Use SET command to store the JSON data in Redis
	err = rc.client.Set(rc.ctxbg, cb, userBalanceSON, 0).Err()
	if err != nil {
		return err
	}

	return nil
}

func (rc *RedisClient) GetUserBalance(currency string) (bitso.Balance, error) {
	var balance bitso.Balance
	// Retrieve the JSON data from Redis
	cb := fmt.Sprintf("%s_balance", currency)
	balanceJSON, err := rc.client.Get(rc.ctxbg, cb).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key does not exist in Redis
			return balance, errors.New("balance data not found in Redis")
		}
		// Other error occurred
		return balance, err
	}

	// Unmarshal the JSON data into a Balance struct
	err = json.Unmarshal([]byte(balanceJSON), &balance)
	if err != nil {
		return balance, err
	}

	return balance, nil
}

func (rc *RedisClient) SaveFees(fees []bitso.Fee) error {

	for _, fee := range fees {

		book_fee := fmt.Sprintf("fee_%s", fee.Book.String())
		// Convert the Ticker struct to JSON
		bookJSON, err := json.Marshal(fee)
		if err != nil {
			return err
		}

		// Use SET command to store the JSON data in Redis
		err = rc.client.Set(rc.ctxbg, book_fee, bookJSON, 0).Err()
		if err != nil {
			return err
		}

	}
	return nil
}

func (rc *RedisClient) GetFee(book bitso.Book) (bitso.Fee, error) {
	var fee bitso.Fee
	// Retrieve the JSON data from Redis
	book_fee := fmt.Sprintf("fee_%s", book.String())
	feeJSON, err := rc.client.Get(rc.ctxbg, book_fee).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key does not exist in Redis
			return fee, errors.New("fee data not found in Redis")
		}
		// Other error occurred
		return fee, err
	}

	// Unmarshal the JSON data into a Fee struct
	err = json.Unmarshal([]byte(feeJSON), &fee)
	if err != nil {
		return fee, err
	}

	return fee, nil
}

func (rc *RedisClient) SaveExchangeOrderBooks(exchange_order_books []bitso.ExchangeOrderBook) error {

	for _, exchange_order_book := range exchange_order_books {

		book := fmt.Sprintf("exchange_order_book_%s", exchange_order_book.Book)
		// Convert the Ticker struct to JSON
		bookJSON, err := json.Marshal(exchange_order_book)
		if err != nil {
			return err
		}

		// Use SET command to store the JSON data in Redis
		err = rc.client.Set(rc.ctxbg, book, bookJSON, 0).Err()
		if err != nil {
			return err
		}

	}
	return nil
}

func (rc *RedisClient) GetExchangeOrderBook(book bitso.Book) (bitso.ExchangeOrderBook, error) {
	var exchange_order_book bitso.ExchangeOrderBook
	// Retrieve the JSON data from Redis
	eo_book := fmt.Sprintf("exchange_order_book_%s", book.String())
	eo_bookJSON, err := rc.client.Get(rc.ctxbg, eo_book).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key does not exist in Redis
			return exchange_order_book, errors.New("fee data not found in Redis")
		}
		// Other error occurred
		return exchange_order_book, err
	}

	// Unmarshal the JSON data into a Fee struct
	err = json.Unmarshal([]byte(eo_bookJSON), &exchange_order_book)
	if err != nil {
		return exchange_order_book, err
	}

	return exchange_order_book, nil
}

func (rc *RedisClient) SaveUserOrder(user_order bitso.UserOrder) error {

	type temp struct {
		Book           bitso.Book        `json:"book"`
		OriginalAmount bitso.Monetary    `json:"original_amount"`
		UnfilledAmount bitso.Monetary    `json:"unfilled_amount"`
		OriginalValue  bitso.Monetary    `json:"original_value"`
		CreatedAt      string            `json:"created_at"`
		UpdatedAt      string            `json:"updated_at"`
		Price          bitso.Monetary    `json:"price"`
		OID            string            `json:"oid"`
		Side           bitso.OrderSide   `json:"side"`
		Status         bitso.OrderStatus `json:"status"`
		Type           string            `json:"type"`
	}
	temp_user_order := &temp{
		Book:           user_order.Book,
		OriginalAmount: user_order.OriginalAmount,
		UnfilledAmount: user_order.UnfilledAmount,
		OriginalValue:  user_order.OriginalValue,
		CreatedAt:      user_order.CreatedAt.String(),
		UpdatedAt:      user_order.UpdatedAt.String(),
		Price:          user_order.Price,
		OID:            user_order.OID,
		Side:           user_order.Side,
		Status:         user_order.Status,
		Type:           user_order.Type,
	}
	order_id := fmt.Sprintf("user_order_%s", user_order.OID)
	// Convert the Ticker struct to JSON
	userorderJSON, err := json.Marshal(temp_user_order)
	if err != nil {
		return err
	}

	// Use SET command to store the JSON data in Redis
	err = rc.client.Set(rc.ctxbg, order_id, userorderJSON, 0).Err()
	if err != nil {
		return err
	}
	return nil
}

func (rc *RedisClient) SaveUserOrders(user_orders []bitso.UserOrder) error {

	for _, user_order := range user_orders {

		order_id := fmt.Sprintf("user_order_%s", user_order.OID)
		// Convert the Ticker struct to JSON
		userorderJSON, err := json.Marshal(user_order)
		if err != nil {
			return err
		}

		// Use SET command to store the JSON data in Redis
		err = rc.client.Set(rc.ctxbg, order_id, userorderJSON, 0).Err()
		if err != nil {
			return err
		}

	}
	return nil
}

func (rc *RedisClient) GetUserOrderById(oid string) (bitso.UserOrder, error) {
	var user_order bitso.UserOrder
	// Retrieve all user orders from Redis for the specified type
	orders, err := rc.GetAllUserOrders()
	if err != nil {
		return user_order, err
	}

	for _, order := range orders {
		if order.OID == oid {
			user_order = order
		}
	}

	return user_order, nil
}

func (rc *RedisClient) GetUserOrdersById(oids []string) ([]bitso.UserOrder, error) {
	// Retrieve all user orders from Redis for the specified type
	orders, err := rc.GetAllUserOrders()
	if err != nil {
		return nil, err
	}

	// Filter the user orders by type
	filteredOrders := make([]bitso.UserOrder, 0)
	for _, oid := range oids {
		for _, order := range orders {
			if order.OID == oid {
				filteredOrders = append(filteredOrders, order)
			}
		}
	}

	return filteredOrders, nil
}

func (rc *RedisClient) GetUserOrdersByBook(book bitso.Book) ([]bitso.UserOrder, error) {
	// Retrieve all user orders from Redis for the specified type
	orders, err := rc.GetAllUserOrders()
	if err != nil {
		return nil, err
	}

	// Filter the user orders by type
	filteredOrders := make([]bitso.UserOrder, 0)
	for _, order := range orders {
		if order.Book == book {
			filteredOrders = append(filteredOrders, order)
		}
	}

	return filteredOrders, nil
}

func (rc *RedisClient) GetUserOrdersByType(book bitso.Book, userType string) ([]bitso.UserOrder, error) {
	// Retrieve all user orders from Redis for the specified type
	orders, err := rc.GetUserOrdersByBook(book)
	if err != nil {
		return nil, err
	}

	// Filter the user orders by type
	filteredOrders := make([]bitso.UserOrder, 0)
	for _, order := range orders {
		if order.Type == userType {
			filteredOrders = append(filteredOrders, order)
		}
	}

	return filteredOrders, nil
}

func (rc *RedisClient) GetUserOrdersBySide(book bitso.Book, side string) ([]bitso.UserOrder, error) {
	// Retrieve all user orders from Redis for the specified side
	orders, err := rc.GetUserOrdersByBook(book)
	if err != nil {
		return nil, err
	}

	// Filter the user orders by side
	filteredOrders := make([]bitso.UserOrder, 0)
	for _, order := range orders {
		if order.Side.String() == side {
			filteredOrders = append(filteredOrders, order)
		}
	}

	return filteredOrders, nil
}

func (rc *RedisClient) GetUserOrdersByStatus(book bitso.Book, status string) ([]bitso.UserOrder, error) {
	// Retrieve all user orders from Redis for the specified side
	orders, err := rc.GetUserOrdersByBook(book)
	if err != nil {
		return nil, err
	}

	// Filter the user orders by side
	filteredOrders := make([]bitso.UserOrder, 0)
	for _, order := range orders {
		if order.Status.String() == status {
			filteredOrders = append(filteredOrders, order)
		}
	}

	return filteredOrders, nil
}

// Helper function to retrieve all user orders from Redis
func (rc *RedisClient) GetAllUserOrders() ([]bitso.UserOrder, error) {
	// Query all keys matching the user order pattern
	keys, err := rc.client.Keys(rc.ctxbg, "user_order_*").Result()
	if err != nil {
		return nil, err
	}

	// Retrieve JSON data for each key
	jsonDataList := make([]string, len(keys))
	for i, key := range keys {
		jsonData, err := rc.client.Get(rc.ctxbg, key).Result()
		if err != nil {
			return nil, err
		}
		jsonDataList[i] = jsonData
	}

	// Unmarshal the JSON data into UserOrder slice
	var userOrders []bitso.UserOrder
	// var userOrder bitso.UserOrder
	for _, jsonData := range jsonDataList {
		var temp struct {
			Book           bitso.Book        `json:"book"`
			OriginalAmount bitso.Monetary    `json:"original_amount"`
			UnfilledAmount bitso.Monetary    `json:"unfilled_amount"`
			OriginalValue  bitso.Monetary    `json:"original_value"`
			CreatedAt      string            `json:"created_at"`
			UpdatedAt      string            `json:"updated_at"`
			Price          bitso.Monetary    `json:"price"`
			OID            string            `json:"oid"`
			Side           bitso.OrderSide   `json:"side"`
			Status         bitso.OrderStatus `json:"status"`
			Type           string            `json:"type"`
		}
		const iso8601Time = "2006-01-02T15:04:05-0700"
		if err := json.Unmarshal([]byte(jsonData), &temp); err != nil {
			return nil, err
		}
		createdAtTime, err := time.Parse(iso8601Time, temp.CreatedAt)
		if err != nil {
			return nil, err // Handle the error appropriately
		}

		updatedAtTime, err := time.Parse(iso8601Time, temp.UpdatedAt)
		if err != nil {
			return nil, err // Handle the error appropriately
		}

		userOrder := bitso.UserOrder{
			Book:           temp.Book,
			OriginalAmount: temp.OriginalAmount,
			UnfilledAmount: temp.UnfilledAmount,
			OriginalValue:  temp.OriginalValue,
			CreatedAt:      bitso.Time(createdAtTime),
			UpdatedAt:      bitso.Time(updatedAtTime),
			Price:          temp.Price,
			OID:            temp.OID,
			Side:           temp.Side,
			Status:         temp.Status,
			Type:           temp.Type,
		}
		userOrders = append(userOrders, userOrder)
	}
	return userOrders, nil
}

func (rc *RedisClient) SaveTrade(userTrade *bitso.UserTrade) error {
	// Convert the Ticker struct to JSON
	type temp struct {
		Book         bitso.Book      `json:"book"`
		Major        bitso.Monetary  `json:"major"`
		CreatedAt    string          `json:"created_at"`
		Minor        bitso.Monetary  `json:"minor"`
		FeesAmount   bitso.Monetary  `json:"fees_amount"`
		FeesCurrency bitso.Currency  `json:"currency"`
		Price        bitso.Monetary  `json:"price"`
		TID          bitso.TID       `json:"tid"`
		OID          string          `json:"oid"`
		Side         bitso.OrderSide `json:"side"`
	}
	parsed_usertrade := &temp{
		Book:         userTrade.Book,
		Major:        userTrade.Major,
		CreatedAt:    userTrade.CreatedAt.String(),
		Minor:        userTrade.Minor,
		FeesAmount:   userTrade.FeesAmount,
		FeesCurrency: userTrade.FeesCurrency,
		Price:        userTrade.Price,
		TID:          userTrade.TID,
		OID:          userTrade.OID,
		Side:         userTrade.Side,
	}

	// parsed_usertrade := map[string]interface{}{
	// 	"book":          userTrade.Book,
	// 	"major":         userTrade.Major,
	// 	"created_at":    userTrade.CreatedAt.String(),
	// 	"minor":         userTrade.Minor,
	// 	"fees_amount":   userTrade.FeesAmount,
	// 	"fees_currency": userTrade.FeesCurrency,
	// 	"price":         userTrade.Price,
	// 	"tid":           userTrade.TID,
	// 	"oid":           userTrade.OID,
	// 	"side":          userTrade.Side,
	// }
	userTradeJSON, err := json.Marshal(parsed_usertrade)
	if err != nil {
		return err
	}
	trade_id := fmt.Sprintf("trade_%s", userTrade.OID)
	// Use SET command to store the JSON data in Redis
	err = rc.client.Set(rc.ctxbg, trade_id, userTradeJSON, 0).Err()
	if err != nil {
		return err
	}
	return nil
}

func (rc *RedisClient) GetUserTrade(trade_id string) (bitso.UserTrade, error) {
	var trade bitso.UserTrade
	tradeId := fmt.Sprintf("trade_%s", trade_id)
	// Retrieve the JSON data from Redis
	tradeJSON, err := rc.client.Get(rc.ctxbg, tradeId).Result()
	if err != nil {
		return trade, err
	}
	var tradeMap map[string]interface{}
	err = json.Unmarshal([]byte(tradeJSON), &tradeMap)
	if err != nil {
		return trade, err
	}
	// Assign the value to the trade.CreatedAt field
	// trade.CreatedAt = bitso.Time(createdAt)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key does not exist in Redis
			return trade, errors.New("trade data not found in Redis")
		}
		// Other error occurred
		return trade, err
	}

	// Unmarshal the JSON data into a Trade struct
	err = json.Unmarshal([]byte(tradeJSON), &trade)
	if err != nil {
		return trade, err
	}

	return trade, nil
}

// func (rc *RedisClient) SaveProfit(profit *simulation.TradingProfit) error {
// 	profitJSON, err := json.Marshal(profit)
// 	if err != nil {
// 		return err
// 	}
// 	profit_currency := fmt.Sprintf("profit_%s", profit.Currency.String())
// 	// Use SET command to store the JSON data in Redis
// 	err = rc.client.Set(rc.ctxbg, profit_currency, profitJSON, 0).Err()
// 	if err != nil {
// 		return err
// 	}
// 	return nil
// }

// func (rc *RedisClient) GetProfit(currency string) (bitso.UserTrade, error) {
// 	var profit simulation.TradingProfit
// 	profit_currency := fmt.Sprintf("profit_%s", currency)
// 	// Retrieve the JSON data from Redis
// 	profitJSON, err := rc.client.Get(rc.ctxbg, profit_currency).Result()
// 	if err != nil {
// 		return profit, err
// 	}

// 	// Unmarshal the JSON data into a Trade struct
// 	err = json.Unmarshal([]byte(profitJSON), &profit)
// 	if err != nil {
// 		return profit, err
// 	}

// 	return profit, nil
// }

func (rc *RedisClient) GetTrade(trade_id string) (bitso.Trade, error) {
	var trade bitso.Trade
	tradeId := fmt.Sprintf("trade_%s", trade_id)
	// Retrieve the JSON data from Redis
	tradeJSON, err := rc.client.Get(rc.ctxbg, tradeId).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key does not exist in Redis
			return trade, errors.New("Trade data not found in Redis")
		}
		// Other error occurred
		return trade, err
	}

	// Unmarshal the JSON data into a Trade struct
	err = json.Unmarshal([]byte(tradeJSON), &trade)
	if err != nil {
		return trade, err
	}

	return trade, nil
}

func (rc *RedisClient) SaveBatchTrade(batch queue.BidTradeTrendConsumer) error {
	batch_id := fmt.Sprintf("batchid_%d_index_%d_start_%v_end_%v", batch.BatchId, batch.Index, batch.WindowStart, batch.WindowEnd)
	// Convert the Ticker struct to JSON
	batchJSON, err := json.Marshal(batch)
	if err != nil {
		return err
	}

	// Use SET command to store the JSON data in Redis
	err = rc.client.Set(rc.ctxbg, batch_id, batchJSON, 0).Err()
	if err != nil {
		return err
	}
	return nil
}

// Helper function to retrieve all user orders from Redis
func (rc *RedisClient) GetAllBatchTrades() ([]queue.BidTradeTrendConsumer, error) {
	// Query all keys matching the user order pattern
	keys, err := rc.client.Keys(rc.ctxbg, "batchid_*").Result()
	if err != nil {
		return nil, err
	}

	// Retrieve JSON data for each key
	jsonDataList := make([]string, len(keys))
	for i, key := range keys {
		jsonData, err := rc.client.Get(rc.ctxbg, key).Result()
		if err != nil {
			return nil, err
		}
		jsonDataList[i] = jsonData
	}

	// Unmarshal the JSON data into UserOrder slice
	var batchTrades []queue.BidTradeTrendConsumer
	for _, jsonData := range jsonDataList {
		var batchtrades []queue.BidTradeTrendConsumer
		err := json.Unmarshal([]byte(jsonData), &batchtrades)
		if err != nil {
			return nil, err
		}
		batchTrades = append(batchTrades, batchtrades...)
	}

	return batchTrades, nil
}

func (rc *RedisClient) GetBatchTradesByIds(ids []int) ([]queue.BidTradeTrendConsumer, error) {
	// Retrieve all user orders from Redis for the specified type
	trades, err := rc.GetAllBatchTrades()
	if err != nil {
		return nil, err
	}

	// Filter the user orders by type
	filteredBatches := make([]queue.BidTradeTrendConsumer, 0)
	for _, id := range ids {
		for _, trade := range trades {
			if trade.BatchId == id {
				filteredBatches = append(filteredBatches, trade)
			}
		}
	}

	return filteredBatches, nil
}

func (rc *RedisClient) GetLastNthBatches(batches int) ([]queue.BidTradeTrendConsumer, error) {
	var lastNthBatches []queue.BidTradeTrendConsumer

	// Retrieve all keys matching the pattern
	keys, err := rc.client.Keys(rc.ctxbg, "batchid_*").Result()
	if err != nil {
		return nil, err
	}

	// Extract the batch IDs from the keys and sort them
	var batchIDs []int
	uniqueBatchIDs := make(map[int]bool)
	for _, key := range keys {

		batchIDStr := strings.TrimPrefix(key, "batchid_")
		parts := strings.Split(batchIDStr, "_")
		batchID, _ := strconv.Atoi(parts[0])
		if !uniqueBatchIDs[batchID] {
			batchIDs = append(batchIDs, batchID)
			uniqueBatchIDs[batchID] = true
		}
	}

	sort.Slice(batchIDs, func(i, j int) bool {
		return batchIDs[i] < batchIDs[j]
	})
	if len(batchIDs) <= batches {
		batches = len(batchIDs)
	}

	// Retrieve data for the last two batch IDs
	// i < 272 && i < 2
	log.Println("batchIDs: ", batchIDs)
	listName := "batchIdsUsed"
	if len(batchIDs) > 1 {
		start_index := (len(batchIDs) - 1) - batches
		for i := start_index; i < len(batchIDs); i++ {
			batchIDStr := strconv.Itoa(int(batchIDs[i]))
			keyPattern := fmt.Sprintf("batchid_%s_*", batchIDStr)
			keys, err := rc.client.Keys(rc.ctxbg, keyPattern).Result()
			if err != nil {
				return nil, err
			}

			if i < (len(batchIDs) - 1) {
				// Check if batchID is already in the list
				isInList, err := rc.client.SIsMember(rc.ctxbg, listName, batchIDStr).Result()
				if err != nil {
					return nil, err
				}

				// If not in the list, add it and update the list
				if !isInList {
					// Add batchIDStr to the list
					log.Println("batchId: ", batchIDStr, " is being added")
					err := rc.client.SAdd(rc.ctxbg, listName, batchIDStr).Err()
					if err != nil {
						return nil, err
					}

					// Update the list
					// err = rc.UpdateBatchIDsList(listName, batchIDs[i])
					// if err != nil {
					// 	return nil, err
					// }
				}
			}

			for _, key := range keys {
				dataJSON, err := rc.client.Get(rc.ctxbg, key).Result()
				if err != nil {
					return nil, err
				}

				var batchData queue.BidTradeTrendConsumer
				err = json.Unmarshal([]byte(dataJSON), &batchData)
				if err != nil {
					return nil, err
				}
				lastNthBatches = append(lastNthBatches, batchData)
			}
		}
	}
	list, err := rc.RetrieveBatchIDsList(listName)
	if err != nil {
		log.Fatalln("err while getting unique batch ids registered")
	}
	log.Println("list of unique registered Batch Ids: ", list)
	return lastNthBatches, nil
}

func (rc *RedisClient) SaveBidCumulativeMAPercentageChange(ma_per_chg int) error {
	cum_ma_per_ch := "bid_trade_cum_ma_percentage_change"
	cum_maJSON, err := json.Marshal(cum_ma_per_ch)
	if err != nil {
		return err
	}

	// Use SET command to store the JSON data in Redis
	err = rc.client.Set(rc.ctxbg, cum_ma_per_ch, cum_maJSON, 0).Err()
	if err != nil {
		return err
	}
	return nil
}

func (rc *RedisClient) GetBidCumulativeMAPercentageChange(batchId int) (int, error) {
	var cum_ma int
	cum_ma_per_ch := "bid_trade_cum_ma_percentage_change"
	cum_maJSON, err := rc.client.Get(rc.ctxbg, cum_ma_per_ch).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key does not exist in Redis
			return cum_ma, errors.New("Trade data not found in Redis")
		}
		// Other error occurred
		return cum_ma, err
	}
	// Unmarshal the JSON data into a Trade struct
	err = json.Unmarshal([]byte(cum_maJSON), &cum_ma)
	if err != nil {
		return cum_ma, err
	}

	return cum_ma, nil
}

func (rc *RedisClient) RetrieveBatchIDsList(listName string) ([]int, error) {
	// Retrieve the list of batch IDs from Redis
	batchIDsInList, err := rc.client.SMembers(rc.ctxbg, listName).Result()
	if err != nil {
		return nil, err
	}

	// Convert the batch IDs from strings to integers
	var batchIDs []int
	for _, batchIDStr := range batchIDsInList {
		batchID, err := strconv.Atoi(batchIDStr)
		if err != nil {
			return nil, err
		}
		batchIDs = append(batchIDs, batchID)
	}
	// Sort the batch IDs in ascending order
	sort.Ints(batchIDs)

	return batchIDs, nil
}

func (rc *RedisClient) UpdateBatchIDsList(listName string, batchID int) error {
	// Convert batchID to string
	batchIDStr := strconv.Itoa(batchID)

	// Add the batch ID to the list
	err := rc.client.SAdd(rc.ctxbg, listName, batchIDStr).Err()
	if err != nil {
		return err
	}

	return nil
}

func (rc *RedisClient) ClearBatchIDsList(listName string) error {
	// Use SRem to remove all members from the list
	err := rc.client.Del(rc.ctxbg, listName).Err()
	if err != nil {
		return err
	}

	return nil
}

func (rc *RedisClient) DeleteKafkaBatchKeysByPattern(pattern string) error {
	// Retrieve all keys matching the pattern
	keys, err := rc.client.Keys(rc.ctxbg, pattern).Result()
	if err != nil {
		return err
	}

	// Delete each key
	for _, key := range keys {
		err := rc.client.Del(rc.ctxbg, key).Err()
		if err != nil {
			return err
		}
	}

	return nil
}

func (rc *RedisClient) DeleteAllUserOrders() error {
	// Retrieve all keys
	keys, err := rc.client.Keys(rc.ctxbg, "user_order_*").Result()
	if err != nil {
		return err
	}

	// Delete each key
	for _, key := range keys {
		err := rc.client.Del(rc.ctxbg, key).Err()
		if err != nil {
			return err
		}
	}

	return nil
}

// InsertTradeRecord inserts trade records into Redis using Sent attribute as the key
func (rc *RedisClient) InsertTradeRecord(trades *bitso.WebsocketTrade) error {
	// Marshal trades payload to JSON
	tradesJSON, err := json.Marshal(trades)
	if err != nil {
		log.Printf("Error marshaling trades payload to JSON: %v", err)
		return err
	}

	// Convert JSON bytes to string
	tradesStr := string(tradesJSON)

	// Store trades payload as a list in Redis using Sent attribute as the key
	if err := rc.client.RPush(context.Background(), fmt.Sprintf("trades:%s", trades.Sent), tradesStr).Err(); err != nil {
		log.Printf("Error storing trades payload in Redis: %v", err)
		return err
	}

	fmt.Println("Trade data inserted successfully!\n", tradesStr)
	return nil
}

func (rc *RedisClient) DeleteAllWSTradeRecords() error {
	// Retrieve all keys
	keys, err := rc.client.Keys(rc.ctxbg, "trades:*").Result()
	if err != nil {
		return err
	}

	// Delete each key
	for _, key := range keys {
		err := rc.client.Del(rc.ctxbg, key).Err()
		if err != nil {
			return err
		}
	}
	fmt.Println("All ws trade data were deleted successfully!")
	return nil
}

// GetKeysMatchingPattern retrieves all keys matching the specified pattern
func (rc *RedisClient) GetKeysMatchingPattern(pattern string) ([]string, error) {
	keys, err := rc.client.Keys(context.Background(), pattern).Result()
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// GetAllTradeRecords retrieves all trade records from Redis
func (rc *RedisClient) GetAllTradeRecords() ([]bitso.WebsocketTrade, error) {
	var trades []bitso.WebsocketTrade

	// Get all lists without any timestamp range
	keys, err := rc.GetKeysMatchingPattern("trades:*")
	if err != nil {
		return nil, err
	}

	// Iterate over each key to retrieve data
	for _, key := range keys {
		// Get all items from the list
		tradesStr, err := rc.client.LRange(context.Background(), key, 0, -1).Result()
		if err != nil {
			return nil, err
		}

		// Unmarshal each trade record
		for _, tradeStr := range tradesStr {
			var trade bitso.WebsocketTrade
			if err := json.Unmarshal([]byte(tradeStr), &trade); err != nil {
				return nil, err
			}
			trades = append(trades, trade)
		}
	}

	return trades, nil
}

// GetTradeRecords retrieves trade records from Redis within a specific timestamp range
func (rc *RedisClient) GetTradeRecordsByTimestampRange(start, end uint64) ([]bitso.WebsocketTrade, error) {
	var trades []bitso.WebsocketTrade

	// Get all lists within the specified timestamp range
	keys, err := rc.GetKeysMatchingPattern("trades:*")
	if err != nil {
		return nil, err
	}

	// Iterate over each key to retrieve data
	for _, key := range keys {
		// Extract the timestamp from the key
		var sent uint64
		fmt.Sscanf(key, "trades:%d", &sent)

		// Check if the timestamp is within the specified range
		if sent >= start && sent <= end {
			// Get all items from the list
			tradesStr, err := rc.client.LRange(context.Background(), key, 0, -1).Result()
			if err != nil {
				return nil, err
			}
			// Unmarshal each trade record
			for _, tradeStr := range tradesStr {
				var trade bitso.WebsocketTrade
				if err := json.Unmarshal([]byte(tradeStr), &trade); err != nil {
					return nil, err
				}
				trades = append(trades, trade)
			}
		}
	}
	return trades, nil
}

// GetLatestTradeRecord retrieves the latest trade record from Redis
func (rc *RedisClient) GetLatestTradeRecord() (*bitso.WebsocketTrade, error) {
	// Get all keys matching the pattern "trades:*"
	keys, err := rc.GetKeysMatchingPattern("trades:*")
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("no trade records found")
	}

	// Debug: Print all keys
	// fmt.Println("Retrieved keys:", keys)

	// Find the key with the latest timestamp
	var latestKey string
	var latestTimestamp uint64
	for _, key := range keys {
		var sent uint64
		// Debug: Print the key being processed
		fmt.Println("Processing key:", key)

		// Attempt to parse the key
		n, err := fmt.Sscanf(key, "trades:%d", &sent)
		if err != nil || n != 1 {
			// Debug: Print error if parsing fails
			fmt.Printf("Error parsing key %s: %v\n", key, err)
			continue
		}

		// Debug: Print the parsed sent value
		// fmt.Println("Parsed sent:", sent)

		// Compare and assign the latest timestamp and key
		if sent > latestTimestamp {
			latestTimestamp = sent
			latestKey = key
		}

		// Debug: Print current latest timestamp and key
		// fmt.Println("Latest timestamp:", latestTimestamp, "Latest key:", latestKey)

	}

	if latestKey == "" {
		return nil, fmt.Errorf("no valid trade records found")
	}

	// Get the latest record from the list associated with the latest key
	tradeStr, err := rc.client.LIndex(context.Background(), latestKey, -1).Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("no elements in the list for key %s", latestKey)
	} else if err != nil {
		return nil, err
	}

	var trade bitso.WebsocketTrade
	if err := json.Unmarshal([]byte(tradeStr), &trade); err != nil {
		return nil, err
	}

	return &trade, nil
}
