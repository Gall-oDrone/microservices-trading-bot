package streaming

import (
	"fmt"
	"log"
	"time"

	"github.com/segmentio/kafka-go/example/consumer-logger/database"
	"github.com/xiam/bitso-go/bitso"
)

type StreamingStorage interface {
}

type StreamingFlow struct {
	ws_trades     bitso.WebsocketTrade
	ws_difforders bitso.WebsocketDiffOrder
}

func startStreaming() {
	mayor_currency := bitso.ToCurrency("btc")
	minor_currency := bitso.ToCurrency("mxn")
	book := bitso.NewBook(mayor_currency, minor_currency)

	ws, err := bitso.NewWebsocket()
	if err != nil {
		log.Fatal("ws conn error: ", err)
	}
	ws.Subscribe(book, "trades")
	redis_client, err := database.SetupRedis()
	if err != nil {
		log.Fatalln("error during redis setup: ", err)
	}
	defer redis_client.CloseDB()
	for {
		go PostTrades()
		go CalcTimeSeriesOperations()

		select {
		case <-done:
			// Method completed successfully, resume loop
			fmt.Println("gorutine OrderMaker done")
		case <-time.After(300 * time.Minute):
			fmt.Println("Timed out")
			return
		case <-done2:
			fmt.Println("gorutine RenewOrCancelOrder done")
			sleepDuration := 1 * time.Minute
			time.Sleep(sleepDuration)
		case <-kafka_avg_price_task_done:
			log.Println("Kafka avg prices done! Now check current prices with kafka data ")
		case <-kafka_ws_trade_trends_task_done:
			log.Println("Kafka price trends done! Now check current prices with kafka data ")
		}
	}
}

func PostTrades(ws bitso.NewWebsocket, redis_client *database.RedisClient) {
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

func CalcTimeSeriesOperations() {
	time_ticker := time.NewTicker(5 * time.Minute)
	defer time_ticker.Stop()
	for range time_ticker.C {
		log.Println("5 minutes have passed")
	}

}
