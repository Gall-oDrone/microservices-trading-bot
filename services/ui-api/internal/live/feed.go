// Package live keeps one connection to Bitso's public WebSocket and turns it
// into display-only market data for the UI: last trade, best bid/ask, today's
// forming daily candle and the provisional SMA50 flip level, plus (for the
// Market page) the top order-book levels and a tape of recent trades.
//
// Nothing here feeds a trading decision. The daily-executor decides on closed
// production candles only; everything computed from the forming candle is
// labelled provisional ("if today closed now").
// Design: docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §8.3.
package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// DefaultURL is Bitso's production public WebSocket (no keys needed).
const DefaultURL = "wss://ws.bitso.com"

// Trade is one execution from the trades channel.
type Trade struct {
	Book   string
	ID     int64
	Price  float64
	Amount float64 // base currency
	// Side is the taker's side: "buy" or "sell". The channel's "t" is 0 for a
	// taker buy and 1 for a taker sell (checked 2026-10-08 against REST
	// /v3/trades, whose maker_side is always the opposite).
	Side string
	At   time.Time
}

// MaxDepth is how many levels per side the orders channel sends (Bitso's top 20).
const MaxDepth = 20

// Level is one aggregated price level of the order book.
type Level struct {
	Price  float64 `json:"price"`
	Amount float64 `json:"amount"` // base currency
}

// Top is the top of the book from the orders channel: the best bid and ask,
// and up to MaxDepth levels per side, best first.
type Top struct {
	Book       string
	Bid, Ask   float64
	Bids, Asks []Level
	At         time.Time
}

// Handler receives upstream events. Calls come from the feed's goroutine.
type Handler interface {
	OnConnect()
	OnMessage(at time.Time) // any message, keep-alives included
	OnTrade(Trade)
	OnTop(Top)
	OnDisconnect(err error)
}

// Feed is a reconnecting client for the trades and orders channels.
type Feed struct {
	URL     string
	Books   []string
	Handler Handler
	Log     *log.Logger
	// ReadTimeout: with no message at all (Bitso sends {"type":"ka"}
	// keep-alives) for this long, the connection is treated as dead.
	ReadTimeout time.Duration
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
	Dialer      *websocket.Dialer
}

func (f *Feed) defaults() {
	if f.URL == "" {
		f.URL = DefaultURL
	}
	if f.ReadTimeout == 0 {
		f.ReadTimeout = 30 * time.Second
	}
	if f.MinBackoff == 0 {
		f.MinBackoff = time.Second
	}
	if f.MaxBackoff == 0 {
		f.MaxBackoff = 30 * time.Second
	}
	if f.Dialer == nil {
		f.Dialer = &websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	}
	if f.Log == nil {
		f.Log = log.Default()
	}
}

// Run connects and reconnects until ctx is done. Backoff doubles from
// MinBackoff to MaxBackoff with ±20% jitter, and resets after a session
// that stayed up for a minute.
func (f *Feed) Run(ctx context.Context) {
	f.defaults()
	backoff := f.MinBackoff
	for {
		start := time.Now()
		err := f.session(ctx)
		if ctx.Err() != nil {
			return
		}
		f.Handler.OnDisconnect(err)
		if time.Since(start) > time.Minute {
			backoff = f.MinBackoff
		}
		wait := time.Duration(float64(backoff) * (0.8 + 0.4*rand.Float64()))
		f.Log.Printf("live: upstream %s: %v; reconnecting in %s", f.URL, err, wait.Round(100*time.Millisecond))
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if backoff *= 2; backoff > f.MaxBackoff {
			backoff = f.MaxBackoff
		}
	}
}

type wsMessage struct {
	Action   string          `json:"action"`
	Response string          `json:"response"`
	Type     string          `json:"type"`
	Book     string          `json:"book"`
	Payload  json.RawMessage `json:"payload"`
	Sent     int64           `json:"sent"`
}

type wsTrade struct {
	I int64  `json:"i"`
	A string `json:"a"`
	R string `json:"r"`
	T int    `json:"t"`
	X int64  `json:"x"`
}

type wsLevel struct {
	R string `json:"r"`
	A string `json:"a"`
}

type wsOrders struct {
	Bids []wsLevel `json:"bids"`
	Asks []wsLevel `json:"asks"`
}

func (f *Feed) session(ctx context.Context) error {
	conn, _, err := f.Dialer.DialContext(ctx, f.URL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	var once sync.Once
	closeConn := func() { once.Do(func() { conn.Close() }) }
	defer closeConn()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			closeConn()
		case <-done:
		}
	}()

	for _, b := range f.Books {
		for _, typ := range []string{"trades", "orders"} {
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(map[string]string{"action": "subscribe", "book": b, "type": typ}); err != nil {
				return fmt.Errorf("subscribe %s %s: %w", b, typ, err)
			}
		}
	}
	f.Handler.OnConnect()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(f.ReadTimeout))
		_, data, err := conn.ReadMessage()
		if err != nil {
			var ne interface{ Timeout() bool }
			if errors.As(err, &ne) && ne.Timeout() {
				return fmt.Errorf("no message for %s", f.ReadTimeout)
			}
			return err
		}
		now := time.Now()
		f.Handler.OnMessage(now)
		var m wsMessage
		if err := json.Unmarshal(data, &m); err != nil {
			f.Log.Printf("live: unparsable message: %v", err)
			continue
		}
		switch {
		case m.Action == "subscribe":
			if m.Response != "ok" {
				return fmt.Errorf("subscribe %s rejected: %s", m.Type, data)
			}
		case m.Type == "ka":
		case m.Type == "trades" && len(m.Payload) > 0:
			var ts []wsTrade
			if err := json.Unmarshal(m.Payload, &ts); err != nil {
				f.Log.Printf("live: trades payload: %v", err)
				continue
			}
			for _, t := range ts {
				tr, ok := toTrade(m.Book, t)
				if ok {
					f.Handler.OnTrade(tr)
				}
			}
		case m.Type == "orders" && len(m.Payload) > 0:
			var o wsOrders
			if err := json.Unmarshal(m.Payload, &o); err != nil {
				f.Log.Printf("live: orders payload: %v", err)
				continue
			}
			top := Top{Book: m.Book, Bids: levels(o.Bids, true), Asks: levels(o.Asks, false), At: now}
			if len(top.Bids) > 0 {
				top.Bid = top.Bids[0].Price
			}
			if len(top.Asks) > 0 {
				top.Ask = top.Asks[0].Price
			}
			if m.Sent > 0 {
				top.At = time.UnixMilli(m.Sent)
			}
			f.Handler.OnTop(top)
		}
	}
}

func toTrade(book string, t wsTrade) (Trade, bool) {
	p, err1 := strconv.ParseFloat(t.R, 64)
	a, err2 := strconv.ParseFloat(t.A, 64)
	if err1 != nil || err2 != nil || p <= 0 || t.X <= 0 {
		return Trade{}, false
	}
	side := "buy"
	if t.T == 1 {
		side = "sell"
	}
	return Trade{Book: book, ID: t.I, Price: p, Amount: a, Side: side, At: time.UnixMilli(t.X)}, true
}

// levels parses one side, best first (highest bid / lowest ask; the
// channel's order is not relied on), merging equal prices, capped at MaxDepth.
func levels(in []wsLevel, bids bool) []Level {
	by := map[float64]float64{}
	for _, l := range in {
		r, err1 := strconv.ParseFloat(l.R, 64)
		a, err2 := strconv.ParseFloat(l.A, 64)
		if err1 != nil || err2 != nil || r <= 0 || a <= 0 {
			continue
		}
		by[r] += a
	}
	out := make([]Level, 0, len(by))
	for p, a := range by {
		out = append(out, Level{Price: p, Amount: a})
	}
	sort.Slice(out, func(i, j int) bool {
		if bids {
			return out[i].Price > out[j].Price
		}
		return out[i].Price < out[j].Price
	})
	if len(out) > MaxDepth {
		out = out[:MaxDepth]
	}
	return out
}
