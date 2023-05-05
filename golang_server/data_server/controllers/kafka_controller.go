package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	kafka "github.com/segmentio/kafka-go"
	"github.com/xiam/bitso-go/bitso"
	"github.com/xiam/bitso-go/utils/sleep"
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

	book := bitso.NewBook(mayor_currency, minor_currency)
	ws.Subscribe(book, "orders")
	for {
		m := <-ws.Receive()
		if order, ok := m.(bitso.WebsocketOrder); ok {
			if order.Payload.Bids != nil {
				mbyte, _ := json.Marshal(m)
				msg := kafka.Message{
					Key:   []byte(fmt.Sprintf("address-%s", r.RemoteAddr)),
					Value: mbyte,
				}
				log.Printf("message: %#v\n\n", m)
				err = kafkaWriter.WriteMessages(r.Context(), msg)

				if err != nil {
					w.Write([]byte(err.Error()))
					log.Fatalln("error during kafka writting msg: ", err)
				}
				// Payload is nil
				sleep.Rest(0)
			}
		} else {
			// m is not of type WebsocketOrder
			log.Println("m is not of type WebsocketOrder")
			log.Printf("message: %#v\n\n", m)
		}
	}
}
