package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
	kafka "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
	"github.com/segmentio/kafka-go/example/consumer-logger/database"
	"github.com/segmentio/kafka-go/example/consumer-logger/simulation"
)

var client = bitso.NewClient(nil)

func setClient() {
	key := os.Getenv("BITSO_API_KEY")
	secret := os.Getenv("BITSO_API_SECRET")
	client.SetAPIKey(key)
	client.SetAPISecret(secret)
}

func getKafkaReader(kafkaURL, topic, groupID string) *kafka.Reader {
	brokers := strings.Split(kafkaURL, ",")
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:  brokers,
		GroupID:  groupID,
		Topic:    topic,
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
	})
}

func main2() {
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error loading .env file:", err)
		return
	}
	simulation.RunSimulation()
}
func main() {
	mayor_currency := bitso.ToCurrency("btc")
	minor_currency := bitso.ToCurrency("mxn")
	book := bitso.NewBook(mayor_currency, minor_currency)

	ws, err := bitso.NewWebsocket()
	if err != nil {
		log.Fatal("ws conn error: ", err)
	}
	ws.Subscribe(book, "diff-orders")
	cassandra_client, err := database.CassandraInitConnection(false)
	if err != nil {
		log.Fatalln("error during cassandra setup: ", err)
	}
	defer cassandra_client.CloseDB()
	for {
		m := <-ws.Receive()
		if order, ok := m.(bitso.WebsocketDiffOrder); ok {
			if order.Payload != nil {
				err = cassandra_client.InsertDiffOrderRecord(&order)
				if err != nil {
					log.Fatalln("error while inserting trade record: ", err)
				}
			}
		} else {
			// m is not of type WebsocketOrder
			log.Println("m is not of type WebsocketOrder")
			log.Printf("message: %#v\n\n", m)
		}
	}
}

func hello(w http.ResponseWriter, req *http.Request) {
	fmt.Println("/hello endpoint called")
	fmt.Fprintf(w, "hello\n")
}

func main3() {
	fmt.Print("hello\n")
	http.HandleFunc("/hello", hello)
	fmt.Println("Server up and listening...")
	http.ListenAndServe(":80", nil)
}
