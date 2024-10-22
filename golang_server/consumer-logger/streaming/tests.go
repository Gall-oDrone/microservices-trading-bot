package streaming

import (
	"fmt"
	"log"
	"time"

	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
	"github.com/segmentio/kafka-go/example/consumer-logger/database"
	"github.com/segmentio/kafka-go/example/consumer-logger/operations"
	"github.com/segmentio/kafka-go/example/consumer-logger/utils"
)

type TestQuery struct {
	rc *database.RedisClient
}

const (
	to_test = "tsops"
)

func TestQueries() {
	redis_client, err := InitRedisClient()
	if err != nil {
		log.Fatalln(err)
	}
	deleteAllWSTrades := false
	if deleteAllWSTrades {
		err = redis_client.DeleteAllWSTradeRecords()
		if err != nil {
			log.Fatalln(err)
		}
	}
	tq := TestQuery{rc: redis_client}
	switch to_test {
	case "all_trades":
		err := tq.GetAllTrades()
		if err != nil {
			log.Fatalln(err)
		}
	case "trades_by_timestamp_range":
		_, err := tq.GetTradesByTimestampRange()
		if err != nil {
			log.Fatalln(err)
		}
	case "latest_trade":
		err := tq.GetLatestTradeRecord()
		if err != nil {
			log.Fatalln(err)
		}
	case "tsops":
		err := tq.TestCalculateTimeSeriesOps()
		if err != nil {
			log.Fatalln(err)
		}
	case "ma":
		err := tq.TestCalculateMovingAverage()
		if err != nil {
			log.Fatalln(err)
		}
	}
}

func InitRedisClient() (*database.RedisClient, error) {
	redis_client, err := database.SetupRedis()
	if err != nil {
		log.Println("error while setting up redis")
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

func (tq *TestQuery) GetTradesByTimestampRange() ([]bitso.WebsocketTrade, error) {
	duration := 5 * time.Minute
	end_time := uint64(time.Now().UnixNano() / int64(time.Millisecond))
	start_time := end_time - uint64(duration/time.Millisecond)
	fmt.Printf("st: %v, et: %v \n", start_time, end_time)
	utils.PrintReadableTime(start_time, end_time)

	trades, err := tq.rc.GetTradeRecordsByTimestampRange(start_time, end_time)
	if err != nil {
		log.Println("error getting trade records: %v", err)
		return nil, err
	}
	// Debug
	// fmt.Println("Printing trades", trades)
	return trades, nil
}

func (tq *TestQuery) TestCalculateTimeSeriesOps() error {
	err := tq.TestCalculateMovingAverage()
	if err != nil {
		return err
	}
	err = tq.TestCalculateWeightedMovingAverage()
	if err != nil {
		return err
	}
	err = tq.TestCalculateSimpleExponentialWeightedMovingAverage()
	if err != nil {
		return err
	}
	err = tq.TestCalculateVariableExponentialWeightedMovingAverage()
	if err != nil {
		return err
	}
	return nil
}
func (tq *TestQuery) TestCalculateMovingAverage() error {
	trades, err := tq.GetTradesByTimestampRange()
	if err != nil {
		return err
	}
	if len(trades) > 0 {
		fmt.Println("No. of trades queried: ", len(trades))
		ma, err := operations.CalculateMovingAverage(trades, "r")
		if err != nil {
			return err
		}
		fmt.Printf("MA: %v\n", ma)
	}
	return nil
}

func (tq *TestQuery) TestCalculateWeightedMovingAverage() error {
	trades, err := tq.GetTradesByTimestampRange()
	if err != nil {
		return err
	}
	if len(trades) > 0 {
		wma, err := operations.CalculateWeightedMovingAverage(trades, "r")
		if err != nil {
			return err
		}
		fmt.Printf("WMA: %v\n", wma)
	}
	return nil
}

func (tq *TestQuery) TestCalculateSimpleExponentialWeightedMovingAverage() error {
	trades, err := tq.GetTradesByTimestampRange()
	if err != nil {
		return err
	}
	if len(trades) > 0 {
		wma, err := operations.CalculateSimpleExponentialWeightedMovingAverage(trades, "r")
		if err != nil {
			return err
		}
		fmt.Printf("SEWMA: %v\n", wma)
	}
	return nil
}

func (tq *TestQuery) TestCalculateVariableExponentialWeightedMovingAverage() error {
	trades, err := tq.GetTradesByTimestampRange()
	if err != nil {
		return err
	}
	if len(trades) > 0 {
		vwma, err := operations.CalculateVariableExponentialWeightedMovingAverage(trades, "r")
		if err != nil {
			return err
		}
		fmt.Printf("VEWMA: %v\n", vwma)
		vwma2, err := operations.CalculateVariableExponentialWeightedMovingAverage2(trades, "r")
		if err != nil {
			return err
		}
		fmt.Printf("VEWMA2: %v\n", vwma2)
	}
	return nil
}

func (tq *TestQuery) GetLatestTradeRecord() error {
	trade, err := tq.rc.GetLatestTradeRecord()
	if err != nil {
		log.Println("error on GetLatestTradeRecord()")
		return err
	}
	println("Latest trade: ", trade)
	return nil
}
