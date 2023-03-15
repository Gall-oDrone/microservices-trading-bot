package controllers

import (
	"fmt"
	"log"
	"net/http"
	"os"

	kafka "github.com/segmentio/kafka-go"
	"github.com/xiam/bitso-go/bitso"
)

func getKafkaWriter(kafkaURL, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:     kafka.TCP(kafkaURL),
		Topic:    topic,
		Balancer: &kafka.LeastBytes{},
	}
}

func ProducerHandler(w http.ResponseWriter, r *http.Request) {
	kafkaURL := os.Getenv("kafkaURL")
	topic := os.Getenv("topic")
	kafkaWriter := getKafkaWriter(kafkaURL, topic)
	fmt.Println("start producer-api...!!")
	defer kafkaWriter.Close()

	ws, err := bitso.NewWebsocket()
	if err != nil {
		log.Fatal("ws conn error: ", err)
	}

	mayor_currency := bitso.ToCurrency("btc")
	minor_currency := bitso.ToCurrency("mxn")
	// target_currency := bitso.ToCurrency("mxn")
	book := bitso.NewBook(mayor_currency, minor_currency)
	ws.Subscribe(book, "orders")
	for {
		m := <-ws.Receive()
		// mbyte, _ := json.Marshal(m)
		msg := kafka.Message{
			Key:   []byte(fmt.Sprintf("address-%s", r.RemoteAddr)),
			Value: []byte(fmt.Sprint(m)),
		}
		err = kafkaWriter.WriteMessages(r.Context(), msg)

		if err != nil {
			w.Write([]byte(err.Error()))
			log.Fatalln("error during kafka writting msg: ", err)
		}
	}
}

// func ProducerHandler(w http.ResponseWriter, r *http.Request) {
// 	kafkaURL := os.Getenv("kafkaURL")
// 	topic := os.Getenv("topic")
// 	kafkaWriter := getKafkaWriter(kafkaURL, topic)
// 	fmt.Println("start producer-api...!!")
// 	defer kafkaWriter.Close()

// 	ws, err := bitso.NewWebsocket()
// 	if err != nil {
// 		log.Fatal("ws conn error: ", err)
// 	}

// 	client := bitso.NewClient(nil)
// 	key := os.Getenv("BITSO_API_KEY")
// 	secret := os.Getenv("BITSO_API_SECRET")
// 	client.SetAPIKey(key)
// 	client.SetAPISecret(secret)

// 	// what crypto has the highest price change
// 	cws, err := feclient.NewWebsocket(w, r)
// 	if err != nil {
// 		log.Println(err)
// 		return
// 	}

// 	mayor_currency := bitso.ToCurrency("btc")
// 	minor_currency := bitso.ToCurrency("mxn")
// 	// target_currency := bitso.ToCurrency("mxn")
// 	book := bitso.NewBook(mayor_currency, minor_currency)
// 	ws.Subscribe(book, "orders")
// 	for {
// 		defer cws.Close()
// 		// t, _ := client.Ticker(book)
// 		m := <-ws.Receive()
// 		err := cws.Sent(m)
// 		if err != nil {
// 			log.Println(err)
// 		}
// 		mbyte, _ := json.Marshal(m)
// 		msg := kafka.Message{
// 			Key:   []byte(fmt.Sprintf("address-%s", r.RemoteAddr)),
// 			Value: mbyte,
// 		}
// 		err = kafkaWriter.WriteMessages(r.Context(), msg)

// 		if err != nil {
// 			w.Write([]byte(err.Error()))
// 			log.Fatalln(err)
// 		}
// 	}
// }
