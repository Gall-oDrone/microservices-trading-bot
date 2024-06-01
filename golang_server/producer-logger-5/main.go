package main

import (
	"log"
	"os"

	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
	"github.com/segmentio/kafka-go/example/consumer-logger/database"
)

var client = bitso.NewClient(nil)

func setClient() {
	key := os.Getenv("BITSO_API_KEY")
	secret := os.Getenv("BITSO_API_SECRET")
	client.SetAPIKey(key)
	client.SetAPISecret(secret)
}

func main() {
	err := InitWs()
	if err != nil {
		log.Fatalln("Error during Websocket: ", err)
	}
}

func InitWs() error {
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
