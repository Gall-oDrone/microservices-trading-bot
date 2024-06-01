package streaming

import (
	"fmt"
	"log"
	"time"

	"github.com/segmentio/kafka-go/example/consumer-logger/database"
)

type TestQuery struct {
	rc *database.RedisClient
}

func TestQueries() {
	deleteAllWSTrades := false
	redis_client, err := InitRedisClient()
	if err != nil {
		log.Fatalln(err)
	}

	if deleteAllWSTrades {
		err = redis_client.DeleteAllWSTradeRecords()
		if err != nil {
			log.Fatalln(err)
		}
	}
	tq := TestQuery{rc: redis_client}
	to_test := "all_trades"
	switch to_test {
	case "all_trades":
		err := tq.GetAllTrades()
		if err != nil {
			log.Fatalln(err)
		}
	case "trades_by_timestamp_range":
		err := tq.GetTradesByTimestampRange()
		if err != nil {
			log.Fatalln(err)
		}
	}
}

func InitRedisClient() (*database.RedisClient, error) {
	redis_client, err := database.SetupRedis()
	if err != nil {
		return &database.RedisClient{}, err
	}
	return redis_client, nil // Dereference the pointer to return a value
}

func (tq *TestQuery) GetAllTrades() error {
	trades, err := tq.rc.GetAllTradeRecords()
	if err != nil {
		log.Println("error getting trade records: %v", err)
		return err
	}
	fmt.Println("Printing trades", trades)
	return nil
}

func (tq *TestQuery) GetTradesByTimestampRange() error {
	duration := 5 * time.Minute
	end_time := uint64(time.Now().UnixNano() / int64(time.Millisecond))
	start_time := end_time - uint64(duration/time.Millisecond)
	fmt.Printf("st: %v, et: %v \n", start_time, end_time)
	trades, err := tq.rc.GetTradeRecordsByTimestampRange(start_time, end_time)
	if err != nil {
		log.Println("error getting trade records: %v", err)
		return err
	}
	fmt.Println("Printing trades", trades)
	return nil
}
