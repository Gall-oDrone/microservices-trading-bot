package feclient

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type Websocket struct {
	conn *websocket.Conn

	outbox chan interface{}
}

// Receive returns a channel where received messages are sent.
func (ws *Websocket) Sent(msg interface{}) error {
	resp, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return ws.conn.WriteMessage(websocket.TextMessage, resp)
}

func NewWebsocket(w http.ResponseWriter, r *http.Request) (*Websocket, error) {
	ws := &Websocket{
		outbox: make(chan interface{}, 8),
	}

	var err error
	ws.conn, err = upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return ws, nil
}

// Close closes the active connection with Bitso's websocket servers.
func (ws *Websocket) Close() error {
	if ws.conn != nil {
		return ws.conn.Close()
	}
	return nil
}
