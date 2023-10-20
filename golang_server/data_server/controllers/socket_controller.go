package controllers

import (
	"log"
	"net/http"
	"os"

	"github.com/gorilla/websocket"
	"github.com/xiam/bitso-go/bitso"
	"github.com/xiam/bitso-go/feclient"
)

// "github.com/joho/godotenv"

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()

	msg := []byte("Let's start to talk something.")
	err = conn.WriteMessage(websocket.TextMessage, msg)
	if err != nil {
		log.Println(err)
	}

	// do other stuff...
}

func SocketHandler(w http.ResponseWriter, r *http.Request) {
	// err := godotenv.Load()
	// if err != nil {
	// 	log.Fatal("SIGMA", err)
	// }
	ws, err := bitso.NewWebsocket()
	if err != nil {
		log.Fatal("ws conn error: ", err)
	}

	client := bitso.NewClient(nil)
	key := os.Getenv("BITSO_API_KEY")
	secret := os.Getenv("BITSO_API_SECRET")
	client.SetAPIKey(key)
	client.SetAPISecret(secret)

	// Registers Websocket routes
	//TODO Implement an automatic method that output
	// what crypto has the highest price change
	cws, err := feclient.NewWebsocket(w, r)
	if err != nil {
		log.Println(err)
		return
	}

	mayor_currency := bitso.ToCurrency("btc")
	minor_currency := bitso.ToCurrency("mxn")
	book := bitso.NewBook(mayor_currency, minor_currency)
	ws.Subscribe(book, "orders")
	defer cws.Close()
	for {
		t, err := client.Ticker(book)
		if err != nil {
			log.Println("client Ticker error: ", err)
		}
		m := <-ws.Receive()
		log.Printf("message: %#v\n\n", t)
		err = cws.Sent(m)
		if err != nil {
			log.Println(err)
		}
		// fmt.Println("m", m)
		//SaveTiker(t)

		// log.Printf("error: %#v\n\n", err)
		// operations.Ratio(m, client, target_currency)
		// strategy.MajorBidArbitrage(m, client, target_currency)
		// time.Sleep(10 * time.Second)
	}
}

// func SaveTiker(ticker *bitso.Ticker) (err error) {
// 	cluster := database.GetCluster()
// 	session, err := cluster.CreateSession()
// 	repo := crud.NewCassandraRepositoryCRUD(session)

// 	func(cassRepository repository.CassandraRepository) {
// 		err := cassRepository.SaveTicker(*ticker)
// 		if err != nil {
// 			log.Println(err)
// 			return
// 		}
// 	}(repo)
// 	return err
// }
