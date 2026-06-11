package bitso

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketConnCloseUnblocksReceive(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, err := NewWebSocketConnWithURL(wsURL)
	if err != nil {
		t.Fatalf("NewWebSocketConnWithURL: %v", err)
	}

	recv := ws.Receive()
	if err := ws.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case _, ok := <-recv:
		if ok {
			t.Fatal("expected receive channel closed after connection close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("receive channel not closed within timeout")
	}
}
