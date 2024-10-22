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
	deleteAllWSTrades := true
	mayor_currency := bitso.ToCurrency("btc")
	minor_currency := bitso.ToCurrency("mxn")
	book := bitso.NewBook(mayor_currency, minor_currency)
	ws, err := bitso.NewWebsocket()
	if err != nil {
		return err
	}
	ws.Subscribe(book, "diff-orders")

	redis_client, err := database.SetupRedis()
	if err != nil {
		return err
	}
	if deleteAllWSDiffOrders {
		err = redis_client.DeleteAllWSDiffOrdersRecords()
		if err != nil {
			log.Fatalln(err)
		}
	}
	defer redis_client.CloseDB()
	for {
		m := <-ws.Receive()
		if orders, ok := m.(bitso.WebsocketOrder); ok {
			if orders.Payload != nil {
				err := redis_client.InsertDiffOrderRecord(&orders)
				if err != nil {
					log.Fatalln("error while inserting orders record: ", err)
				}
			}
		} else {
			// m is not of type WebsocketOrder
			log.Println("m is not of type WebsocketOrder")
			log.Printf("message: %#v\n\n", m)
		}
	}
}
