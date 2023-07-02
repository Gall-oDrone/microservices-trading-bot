package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
	"github.com/xiam/bitso-go/bitso"
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

func (rc *RedisClient) SaveTrade(userTrade *bitso.UserTrade) error {
	// Convert the Ticker struct to JSON
	parsed_usertrade := map[string]interface{}{
		"book":          userTrade.Book,
		"major":         userTrade.Major,
		"created_at":    userTrade.CreatedAt.String(),
		"minor":         userTrade.Minor,
		"fees_amount":   userTrade.FeesAmount,
		"fees_currency": userTrade.FeesCurrency,
		"price":         userTrade.Price,
		"tid":           userTrade.TID,
		"oid":           userTrade.OID,
		"side":          userTrade.Side,
	}
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