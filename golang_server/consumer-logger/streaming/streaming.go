package streaming

import (
	"fmt"
	"log"
	"time"

	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
	"github.com/segmentio/kafka-go/example/consumer-logger/database"
	"github.com/segmentio/kafka-go/example/consumer-logger/operations"
)

type StreamingStorage interface {
}

type StreamingFlow struct {
	bitso_ws      *bitso.Websocket
	ws_trades     bitso.WebsocketTrade
	ws_difforders bitso.WebsocketDiffOrder
	redis_client  database.RedisClient
}

var (
	deleteAllWSTrades bool
)

func StartStreaming() {
	test_querying_trades := true
	if test_querying_trades {
		TestQueries()
	} else {
		err := InitWs()
		if err != nil {
			log.Fatalln("error during InitWs: ", err)
		}
	}
}
func InitVariables() {
	deleteAllWSTrades = true
}

func InitWs() error {
	InitVariables()
	mayor_currency := bitso.ToCurrency("btc")
	minor_currency := bitso.ToCurrency("mxn")
	book := bitso.NewBook(mayor_currency, minor_currency)
	ws, err := bitso.NewWebsocket()
	if err != nil {
		return err
	}
	ws.Subscribe(book, "trades")

	redis_client, err := database.SetupRedis()
	if err != nil {
		return err
	}
	if deleteAllWSTrades {
		err = redis_client.DeleteAllWSTradeRecords()
		if err != nil {
			return err
		}
	}
	defer redis_client.CloseDB()
	for {
		m := <-ws.Receive()
		if trades, ok := m.(bitso.WebsocketTrade); ok {
			if trades.Payload != nil {
				err := redis_client.InsertTradeRecord(&trades)
				if err != nil {
					log.Fatalln("error while inserting trades record: ", err)
				}
			}
		} else {
			// m is not of type WebsocketOrder
			log.Println("m is not of type WebsocketTrade")
			log.Printf("message: %#v\n\n", m)
		}
	}
}

func InitSOps() {
	redis_client, err := database.SetupRedis()
	if err != nil {
		log.Fatalln(err)
	}
	defer redis_client.CloseDB()
	startOperations := make(chan bool, 1)
	co_done := make(chan bool, 1)
	for {
		CalcTimeSeriesOperations(startOperations, co_done, redis_client)
	}
}
func CalcTimeSeriesOperations(startOperations <-chan bool, co_done chan<- bool, redis_client *database.RedisClient) {
	time_ticker := time.NewTicker(5 * time.Minute)
	defer time_ticker.Stop()
	start_time := uint64(time.Now().UnixNano() / int64(time.Millisecond))
	for range time_ticker.C {
		log.Println("1 minute has passed")
		so := <-startOperations
		if so {
			log.Println("starting ops")
			end_time := start_time + uint64(5*time.Minute/time.Millisecond)
			// Retrieve trade records from Redis within the specified time range
			trades, err := redis_client.GetTradeRecordsByTimestampRange(start_time, end_time)
			if err != nil {
				log.Fatalf("error getting trade records: %v", err)
			}
			fmt.Println("Printing trades", trades)
			// Calculate moving average (you need to implement this function)
			ma, err := operations.CalculateMovingAverage(trades, "p")
			if err != nil {
				log.Fatalf("error calculating moving average: %v", err)
			}
			// Convert int64 timestamps to time.Time
			c_startTime := time.Unix(0, int64(start_time)*int64(time.Millisecond))
			c_endTime := time.Unix(0, int64(end_time)*int64(time.Millisecond))

			log.Printf("MA: %f, window start: %v, window end: %v", ma, c_startTime.Format(time.RFC3339), c_endTime.Format(time.RFC3339))
			/*
				// Store the moving average result in Redis
				err = redis_client.SetMovingAverage("Price", "ma", uint64(start_time), uint64(end_time), ma)
				if err != nil {
					log.Fatalf("error setting moving average in Redis: %v", err)
				}
			*/
			// Update start_time for the next iteration
			start_time = end_time
			co_done <- true
		}
	}
}
