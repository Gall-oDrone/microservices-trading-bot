package websocket

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"

	gws "github.com/gorilla/websocket"
)

// wsTestServer is a minimal Bitso-like WebSocket endpoint whose per-connection
// behavior the test controls, so we can reproduce the 2026-08-19 outage: an
// abnormal closure (1006) on a live connection.
type wsTestServer struct {
	url      string
	ln       net.Listener
	upgrader gws.Upgrader
	onConn   func(conn *gws.Conn, n int32)
	count    int32
}

func newWSTestServer(t *testing.T, onConn func(conn *gws.Conn, n int32)) *wsTestServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &wsTestServer{
		url:    "ws://" + ln.Addr().String(),
		ln:     ln,
		onConn: onConn,
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := s.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		n := atomic.AddInt32(&s.count, 1)
		s.onConn(conn, n)
	})
	go func() { _ = http.Serve(ln, handler) }()
	return s
}

func (s *wsTestServer) closeListener() { _ = s.ln.Close() }

func testLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// TestManager_ReconnectsAfterAbnormalClosure verifies the primary fix: when the
// server drops a live connection without a close handshake (1006), the shared
// reader closes its inbox, the manager observes the closed channel, and it
// reconnects instead of blocking forever.
func TestManager_ReconnectsAfterAbnormalClosure(t *testing.T) {
	drained := func(conn *gws.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
	firstDropped := make(chan struct{})
	srv := newWSTestServer(t, func(conn *gws.Conn, n int32) {
		if n == 1 {
			_, _, _ = conn.ReadMessage() // consume subscribe
			_ = conn.Close()             // abrupt drop -> client sees 1006
			close(firstDropped)
			return
		}
		drained(conn) // keep the reconnected socket alive
	})
	defer srv.closeListener()

	reconnected := make(chan struct{}, 1)
	m := NewManager(&ManagerConfig{
		WSURL:             srv.url,
		ReconnectAttempts: 5,
		ReconnectInterval: 10 * time.Millisecond,
		ReconnectMaxDelay: 40 * time.Millisecond,
		Logger:            testLogger(),
		OnReconnect: func() {
			select {
			case reconnected <- struct{}{}:
			default:
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	book := bitso.NewBook(bitso.ToCurrency("btc"), bitso.ToCurrency("mxn"))
	if err := m.Subscribe([]*bitso.Book{book}, []string{"trades"}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = m.Stop() }()

	select {
	case <-firstDropped:
	case <-time.After(2 * time.Second):
		t.Fatal("server never accepted the first connection")
	}

	select {
	case <-reconnected:
	case err := <-m.Fatal():
		t.Fatalf("manager gave up instead of reconnecting: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("manager did not reconnect after abnormal closure")
	}

	if !m.IsConnected() {
		t.Fatal("manager reports disconnected after successful reconnect")
	}
}

// TestManager_FatalAfterReconnectExhausted verifies the second fix: once the
// endpoint is gone and every reconnect attempt fails, the manager signals a
// terminal error via Fatal() instead of silently returning and leaving a
// zombie behind a passing /healthz.
func TestManager_FatalAfterReconnectExhausted(t *testing.T) {
	firstConn := make(chan struct{})
	proceed := make(chan struct{})
	srv := newWSTestServer(t, func(conn *gws.Conn, n int32) {
		if n == 1 {
			_, _, _ = conn.ReadMessage()
			close(firstConn)
			<-proceed // hold the drop until the listener is closed
			_ = conn.Close()
			return
		}
		_ = conn.Close()
	})

	m := NewManager(&ManagerConfig{
		WSURL:             srv.url,
		ReconnectAttempts: 3,
		ReconnectInterval: 10 * time.Millisecond,
		ReconnectMaxDelay: 30 * time.Millisecond,
		Logger:            testLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	book := bitso.NewBook(bitso.ToCurrency("btc"), bitso.ToCurrency("mxn"))
	if err := m.Subscribe([]*bitso.Book{book}, []string{"trades"}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = m.Stop() }()

	select {
	case <-firstConn:
	case <-time.After(2 * time.Second):
		t.Fatal("server never accepted the first connection")
	}

	srv.closeListener() // subsequent dials are refused
	close(proceed)      // now drop the live connection

	select {
	case err := <-m.Fatal():
		if err == nil {
			t.Fatal("expected non-nil fatal error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("manager did not signal Fatal after exhausting reconnects")
	}
}
