package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

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

func WsOrdersProducerHandler(w http.ResponseWriter, r *http.Request) {
	kafkaURL := os.Getenv("kafkaURL")
	topic := os.Getenv("topic")
	kafkaWriter := getKafkaWriter(kafkaURL, topic)
	fmt.Println("start producer-api...!!")
	defer kafkaWriter.Close()

	mayor_currency := bitso.ToCurrency("btc")
	minor_currency := bitso.ToCurrency("mxn")
	book := bitso.NewBook(mayor_currency, minor_currency)

	ws, err := bitso.NewWebsocket()
	if err != nil {
		log.Fatal("ws conn error: ", err)
	}
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
				sleep.Rest(0, 0)
			}
		} else {
			// m is not of type WebsocketOrder
			log.Println("m is not of type WebsocketOrder")
			log.Printf("message: %#v\n\n", m)
		}
	}
}

func WsTradesProducerHandler(w http.ResponseWriter, r *http.Request) {
	kafkaURL := os.Getenv("kafkaURL")
	topic := os.Getenv("topic")
	kafkaWriter := getKafkaWriter(kafkaURL, topic)
	fmt.Println("start producer-api...!!")
	defer kafkaWriter.Close()

	mayor_currency := bitso.ToCurrency("btc")
	minor_currency := bitso.ToCurrency("mxn")
	book := bitso.NewBook(mayor_currency, minor_currency)

	ws, err := bitso.NewWebsocket()
	if err != nil {
		log.Fatal("ws conn error: ", err)
	}
	ws.Subscribe(book, "trades")
	for {
		m := <-ws.Receive()
		if trade, ok := m.(bitso.WebsocketTrade); ok {
			if trade.Payload != nil {
				mbyte, err := json.Marshal(m)
				if err != nil {
					fmt.Println("Error while json marshaling: ", err)
					return
				}
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
				sleep.Rest(0, 0)
			}
		} else {
			// m is not of type WebsocketTrade
			log.Println("m is not of type WebsocketTrade")
			log.Printf("message: %#v\n\n", m)
		}
	}
}

func ProducerHandler2(w http.ResponseWriter, r *http.Request) {

	client := bitso.NewClient(nil)
	key := os.Getenv("BITSO_API_KEY")
	secret := os.Getenv("BITSO_API_SECRET")
	client.SetAPIKey(key)
	client.SetAPISecret(secret)

	kafkaURL := os.Getenv("kafkaURL")
	topic := os.Getenv("topic")
	kafkaWriter := getKafkaWriter(kafkaURL, topic)
	fmt.Println("start producer-api...!!")
	defer kafkaWriter.Close()

	mayor_currency := bitso.ToCurrency("btc")
	minor_currency := bitso.ToCurrency("mxn")
	book := bitso.NewBook(mayor_currency, minor_currency)

	done := make(chan bool)

	for {
		go func() {
			start := time.Now()
			limit := 59 * time.Second
			rest := (60 * time.Second) - limit
			for time.Since(start) < limit {
				ticker, err := client.Ticker(book)
				if err != nil {
					log.Fatalln("Error during ticker request: ", err)
				}
				parsed_ticker := map[string]interface{}{
					"book":       ticker.Book,
					"volume":     ticker.Volume,
					"high":       ticker.High,
					"last":       ticker.Last,
					"low":        ticker.Low,
					"vwap":       ticker.Vwap,
					"ask":        ticker.Ask,
					"bid":        ticker.Bid,
					"created_at": ticker.CreatedAt.String(),
				}
				mbyte, err := json.Marshal(parsed_ticker)
				if err != nil {
					fmt.Println("Error while json marshaling: ", err)
					return
				}
				msg := kafka.Message{
					Key:   []byte(fmt.Sprintf("address-%s", r.RemoteAddr)),
					Value: mbyte,
				}
				log.Printf("message: %#v\n\n", ticker)
				err = kafkaWriter.WriteMessages(r.Context(), msg)

				if err != nil {
					w.Write([]byte(err.Error()))
					log.Fatalln("error during kafka writting msg: ", err)
				}
			}
			time.Sleep(rest)
			done <- true
		}()
		select {
		case <-done:
			// Method completed successfully, resume loop
			fmt.Println("Method completed successfully, resume loop")
		case <-time.After(300 * time.Second):
			fmt.Println("Timed out")
			return
		}
	}
}
func ProducerHandler3(w http.ResponseWriter, r *http.Request) {

	client := bitso.NewClient(nil)
	key := os.Getenv("BITSO_API_KEY")
	secret := os.Getenv("BITSO_API_SECRET")
	client.SetAPIKey(key)
	client.SetAPISecret(secret)

	kafkaURL := os.Getenv("kafkaURL")
	topic := os.Getenv("topic")
	kafkaWriter := getKafkaWriter(kafkaURL, topic)
	fmt.Println("start producer-api...!!")
	defer kafkaWriter.Close()

	mayor_currency := bitso.ToCurrency("btc")
	minor_currency := bitso.ToCurrency("mxn")
	book := bitso.NewBook(mayor_currency, minor_currency)

	done := make(chan bool)
	params := url.Values{}
	params.Set("book", book.String())
	params.Add("limit", "100")
	// params.Add("marker", "some_id")
	// params.Add("sort", "asc")
	for {
		go func() {
			start := time.Now()
			limit := 56 * time.Second
			rest := (60 * time.Second) - limit
			for time.Since(start) < limit {
				ticker, err := client.Trades(params)
				if err != nil {
					log.Fatalln("Error during ticker request: ", err)
				}
				mbyte, _ := json.Marshal(ticker)
				msg := kafka.Message{
					Key:   []byte(fmt.Sprintf("address-%s", r.RemoteAddr)),
					Value: mbyte,
				}
				log.Printf("message: %#v\n\n", ticker)
				err = kafkaWriter.WriteMessages(r.Context(), msg)

				if err != nil {
					w.Write([]byte(err.Error()))
					log.Fatalln("error during kafka writting msg: ", err)
				}
				if time.Since(start)%(16*time.Second) == 0 { // Total 40 secs
					time.Sleep(8 * time.Second) // 16-24 secs, 40-48 secs
				}
			}
			time.Sleep(rest)
			done <- true
		}()
		select {
		case <-done:
			// Method completed successfully, resume loop
			fmt.Println("Method completed successfully, resume loop")
		case <-time.After(300 * time.Second):
			fmt.Println("Timed out")
			return
		}
	}
}

func FeesHandler(w http.ResponseWriter, r *http.Request) {
	client := bitso.NewClient(nil)
	key := os.Getenv("BITSO_API_KEY")
	secret := os.Getenv("BITSO_API_SECRET")
	client.SetAPIKey(key)
	client.SetAPISecret(secret)

	fees, err := client.Fees(nil)
	if err != nil {
		log.Fatalln("Error: ", err)
	}

	log.Printf("message: %#v\n\n", fees)
	// Convert the data to JSON
	jsonData, err := json.Marshal(fees)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set the response content type
	w.Header().Set("Content-Type", "application/json")

	// Write the JSON data to the response
	w.Write(jsonData)
}

func UserTradeHandler(w http.ResponseWriter, r *http.Request) {
	client := bitso.NewClient(nil)
	key := os.Getenv("BITSO_API_KEY")
	secret := os.Getenv("BITSO_API_SECRET")
	client.SetAPIKey(key)
	client.SetAPISecret(secret)
	trades, err := client.MyTrades(nil)
	if err != nil {
		log.Fatalln("Error: ", err)
	}
	data := []map[string]interface{}{}

	for _, trade := range trades {
		slice := map[string]interface{}{
			"book":          trade.Book,
			"major":         trade.Major,
			"created_at":    trade.CreatedAt.String(),
			"minor":         trade.Minor,
			"fees_amount":   trade.FeesAmount,
			"fees_currency": trade.FeesCurrency,
			"price":         trade.Price,
			"tid":           trade.TID,
			"oid":           trade.OID,
			"side":          trade.Side,
		}
		data = append(data, slice)
	}

	log.Printf("message: %#v\n\n", data)
	// Convert the data to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set the response content type
	w.Header().Set("Content-Type", "application/json")

	// Write the JSON data to the response
	w.Write(jsonData)
}
