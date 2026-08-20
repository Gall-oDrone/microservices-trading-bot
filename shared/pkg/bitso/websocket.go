package bitso

import (
	"encoding/json"
	"log"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

const wssURL = `wss://ws.bitso.com`

// WebSocketReply represents a generic reply from a channel.
type WebSocketReply struct {
	Action   string      `json:"action"`
	Response string      `json:"response"`
	Time     uint64      `json:"time"`
	Type     string      `json:"type"`
	Payload  interface{} `json:"payload,omitempty"`
}

// WebSocketTradePayload is a single trade entry within a "trades" message.
type WebSocketTradePayload struct {
	TID               uint64   `json:"i"`
	Amount            Monetary `json:"a"`
	Price             Monetary `json:"r"`
	Value             Monetary `json:"v"`
	MakerSide         string   `json:"t"`
	CreationTimestamp uint64   `json:"x"`
	MakerOrderID      string   `json:"mo"`
	TakerOrderID      string   `json:"to"`
}

// UnmarshalJSON tolerates Bitso sending the maker-side field ("t") as a JSON
// number (0=buy, 1=sell) — the wire format — while keeping MakerSide a string
// ("0"/"1") for existing consumers. A quoted string is also accepted.
func (p *WebSocketTradePayload) UnmarshalJSON(b []byte) error {
	type alias struct {
		TID               uint64          `json:"i"`
		Amount            Monetary        `json:"a"`
		Price             Monetary        `json:"r"`
		Value             Monetary        `json:"v"`
		MakerSide         json.RawMessage `json:"t"`
		CreationTimestamp uint64          `json:"x"`
		MakerOrderID      string          `json:"mo"`
		TakerOrderID      string          `json:"to"`
	}
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	p.TID = a.TID
	p.Amount = a.Amount
	p.Price = a.Price
	p.Value = a.Value
	p.CreationTimestamp = a.CreationTimestamp
	p.MakerOrderID = a.MakerOrderID
	p.TakerOrderID = a.TakerOrderID
	p.MakerSide = strings.Trim(string(a.MakerSide), `"`)
	return nil
}

// WebSocketTrade represents a message from the "trades" channel.
type WebSocketTrade struct {
	Book    Book
	Payload []WebSocketTradePayload
	Sent    uint64 `json:"sent"`
}

// WebSocketDiffOrder represents a message from the "diff-orders" channel.
type WebSocketDiffOrder struct {
	Book    Book
	Payload []struct {
		Timestamp           uint64   `json:"d"`
		Price               Monetary `json:"r"`
		Status              string   `json:"s"`
		Position            int      `json:"t"`
		Amount              Monetary `json:"a"`
		Value               Monetary `json:"v"`
		LastUpdateTimestamp uint64   `json:"z"`
		OrderID             string   `json:"o"`
	}
}

// WebSocketOrder represents a message from the "diff-orders" channel.
type WebSocketOrder struct {
	Book    Book
	Payload struct {
		Bids []struct {
			Amount    Monetary `json:"a"`
			OrderID   string   `json:"o"`
			Position  int      `json:"t"`
			Price     Monetary `json:"r"`
			Status    string   `json:"s"`
			Timestamp uint64   `json:"d"`
			Value     Monetary `json:"v"`
		} `json:"bids"`
		Asks []struct {
			Amount    Monetary `json:"a"`
			OrderID   string   `json:"o"`
			Position  int      `json:"t"`
			Price     Monetary `json:"r"`
			Status    string   `json:"s"`
			Timestamp uint64   `json:"d"`
			Value     Monetary `json:"v"`
		} `json:"asks"`
	} `json:"payload"`
}

// WebSocketMessage represents a message that can be sent to channel.
type WebSocketMessage struct {
	Action string `json:"action"`
	Book   *Book  `json:"book"`
	Type   string `json:"type"`
}

// A WebSocketConn establishes a connection with Bitso's websocket service to
// send and receive messages over the ws protocol.
type WebSocketConn struct {
	endpoint string
	conn     *websocket.Conn

	inbox     chan interface{}
	closeOnce sync.Once
}

// Receive returns a channel where received messages are sent.
func (ws *WebSocketConn) Receive() chan interface{} {
	return ws.inbox
}

// NewWebSocketConn creates a websocket handler and establishes a connection with
// Bitso's default websocket servers.
func NewWebSocketConn() (*WebSocketConn, error) {
	return NewWebSocketConnWithURL(wssURL)
}

// NewWebSocketConnWithURL creates a websocket handler connected to the given URL.
func NewWebSocketConnWithURL(url string) (*WebSocketConn, error) {
	if url == "" {
		url = wssURL
	}

	ws := &WebSocketConn{
		endpoint: url,
		inbox:    make(chan interface{}, 8),
	}

	var err error
	ws.conn, _, err = websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}

	go func() {
		// Closing inbox signals consumers that the reader has stopped so they
		// can trigger reconnect logic (ok == false on the receive channel).
		// Without this, a dropped socket (e.g. 1006 abnormal closure) left the
		// reader dead while consumers blocked forever on a live channel.
		defer close(ws.inbox)
		defer ws.Close()
		for {
			_, data, err := ws.conn.ReadMessage()
			if err != nil {
				log.Printf("failed to read message: %v", err)
				return
			}

			var reply WebSocketReply
			if err := json.Unmarshal(data, &reply); err != nil {
				log.Printf("failed to unmarshal message: %v", err)
				return
			}

			switch reply.Type {
			case "diff-orders":
				if reply.Payload != nil {
					var diff WebSocketDiffOrder
					if err := json.Unmarshal(data, &diff); err != nil {
						log.Printf("failed to unmarshal diff order: %v", err)
						return
					}
					ws.inbox <- diff
					continue
				}
			case "ka":
				// keep alive
				continue
			case "orders":
				if reply.Payload != nil {
					var order WebSocketOrder
					if err := json.Unmarshal(data, &order); err != nil {
						log.Printf("failed to unmarshal order: %v", err)
						return
					}
					ws.inbox <- order
					continue
				}
			case "trades":
				if reply.Payload != nil {
					var trade WebSocketTrade
					if err := json.Unmarshal(data, &trade); err != nil {
						log.Printf("failed to unmarshal trade: %v", err)
						return
					}
					ws.inbox <- trade
					continue
				}
			}

			ws.inbox <- reply
		}
	}()

	return ws, nil
}

// Close closes the active connection with Bitso's websocket servers. It is safe
// to call multiple times and from multiple goroutines: the underlying socket is
// closed at most once (the reader goroutine's defer and consumer shutdown paths
// may both invoke it).
func (ws *WebSocketConn) Close() error {
	var err error
	ws.closeOnce.Do(func() {
		if ws.conn != nil {
			err = ws.conn.Close()
		}
	})
	return err
}

// Subscribe subscribes to a messages channel.
func (ws *WebSocketConn) Subscribe(book *Book, channelName string) error {
	m := WebSocketMessage{
		Action: "subscribe",
		Book:   book,
		Type:   channelName,
	}
	return ws.conn.WriteJSON(m)
}
