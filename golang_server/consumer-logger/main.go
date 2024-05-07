package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
	kafka "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/example/consumer-logger/controllers"
	"github.com/segmentio/kafka-go/example/consumer-logger/simulation"
	"github.com/segmentio/kafka-go/example/consumer-logger/streaming"
	"github.com/segmentio/kafka-go/example/consumer-logger/utils"
	"github.com/xiam/bitso-go/bitso"
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

func main() {
	runTradingSimulation := false
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error loading .env file:", err)
		return
	}
	if runTradingSimulation {
		simulation.RunSimulation()
	} else {
		streaming.StartStreaming()
	}
}
func main2() {
	// get kafka reader using environment variables.
	kafkaURL := os.Getenv("kafkaURL")
	topic := os.Getenv("topic")
	groupID := os.Getenv("groupID")

	reader := getKafkaReader(kafkaURL, topic, groupID)

	defer reader.Close()

	setClient()
	clientFees, err := client.Fees(nil)
	if err != nil {
		log.Fatalln("clientFees error: ", err)
	}
	clientBalances, err := client.Balances(nil)
	if err != nil {
		log.Fatalln("clientBalances error: ", err)
	}

	fmt.Println("start consuming ... !!")
	for {
		m, err := reader.ReadMessage(context.Background())
		if err != nil {
			log.Fatalln(err)
		}
		fmt.Printf("message at topic:%v partition:%v offset:%v	%s = %s\n", m.Topic, m.Partition, m.Offset, string(m.Key), string(m.Value))
		var order bitso.WebsocketOrder
		err = json.Unmarshal(m.Value, &order)
		if err != nil {
			log.Println("Error while unmarshaled message: ", err)
		}
		log.Println("minor: ", order.Book.Minor().String(), ", mayor: ", order.Book.Major().String())
		if len(order.Book.Minor().String()) > 0 {
			err = controllers.ProfitHandler(&order, client, clientFees, clientBalances)
			if err != nil {
				log.Fatalln(err)
			}
			utils.Rest(5)
		}
	}
}

func main3() {
	fmt.Print("hello\n")
	http.HandleFunc("/hello", hello)
	fmt.Println("Server up and listening...")
	http.ListenAndServe(":80", nil)
}
