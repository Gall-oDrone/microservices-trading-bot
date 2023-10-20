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
	// get kafka reader using environment variables.
	kafkaURL := os.Getenv("kafkaURL")
	topic := os.Getenv("topic")
	groupID := os.Getenv("groupID")

	reader := getKafkaReader(kafkaURL, topic, groupID)

	defer reader.Close()

	fmt.Println("start consuming ... !!")
	for {
		m, err := reader.ReadMessage(context.Background())
		if err != nil {
			log.Fatalln(err)
		}
		fmt.Printf("message at topic:%v partition:%v offset:%v	%s = %s\n", m.Topic, m.Partition, m.Offset, string(m.Key), string(m.Value))
		var trade bitso.WebsocketTrade
		err = json.Unmarshal(m.Value, &trade)
		if err != nil {
			log.Println("Error while unmarshaled message: ", err)
		}
		cassandra_client, err := database.CassandraInitConnection()
		if err != nil {
			log.Fatalln("error during cassandra setup: ", err)
		}
		defer cassandra_client.CloseDB()
		err = cassandra_client.InsertTradeRecord(&trade)
		if err != nil {
			log.Fatalln("error while inserting trade record: ", err)
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
