package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	gorillaws "github.com/gorilla/websocket"

	"bitso-trading-platform/shared/pkg/bitso"
)

// fakeBitsoServer is an in-process WebSocket server that speaks enough of the
// Bitso protocol to exercise Manager through the real *bitso.WebSocketConn.
type fakeBitsoServer struct {
	t        *testing.T
	srv      *httptest.Server
	upgrader gorillaws.Upgrader

	mu            sync.Mutex
	conns         []*fakeServerConn
	subscriptions []subscribeMsg

	connected chan *fakeServerConn
}

// subscribeMsg mirrors bitso.WebSocketMessage as seen on the wire.
type subscribeMsg struct {
	Action string `json:"action"`
	Book   string `json:"book"`
	Type   string `json:"type"`
}

// fakeServerConn serialises writes: gorilla connections allow only one
// concurrent writer.
type fakeServerConn struct {
	conn *gorillaws.Conn
	mu   sync.Mutex
}

func (c *fakeServerConn) send(t *testing.T, msg string) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.WriteMessage(gorillaws.TextMessage, []byte(msg)); err != nil {
		t.Fatalf("fake server failed to send message: %v", err)
	}
}

func newFakeBitsoServer(t *testing.T) *fakeBitsoServer {
	t.Helper()
	s := &fakeBitsoServer{
		t:         t,
		connected: make(chan *fakeServerConn, 10),
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(func() {
		s.dropAll()
		s.srv.Close()
	})
	return s
}

func (s *fakeBitsoServer) handle(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	sc := &fakeServerConn{conn: conn}

	s.mu.Lock()
	s.conns = append(s.conns, sc)
	s.mu.Unlock()
	s.connected <- sc

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg subscribeMsg
		if err := json.Unmarshal(data, &msg); err != nil || msg.Action != "subscribe" {
			continue
		}
		s.mu.Lock()
		s.subscriptions = append(s.subscriptions, msg)
		s.mu.Unlock()

		// Bitso acknowledges each subscription with a control reply.
		reply := fmt.Sprintf(`{"action":"subscribe","response":"ok","time":%d,"type":%q}`,
			time.Now().UnixMilli(), msg.Type)
		sc.mu.Lock()
		_ = conn.WriteMessage(gorillaws.TextMessage, []byte(reply))
		sc.mu.Unlock()
	}
}

// URL returns the ws:// URL of the fake server.
func (s *fakeBitsoServer) URL() string {
	return "ws" + strings.TrimPrefix(s.srv.URL, "http")
}

// waitConn waits for the next client connection.
func (s *fakeBitsoServer) waitConn() *fakeServerConn {
	s.t.Helper()
	select {
	case c := <-s.connected:
		return c
	case <-time.After(5 * time.Second):
		s.t.Fatal("Timeout waiting for client to connect to fake Bitso server")
		return nil
	}
}

// dropAll closes every server-side connection, simulating a connection loss.
func (s *fakeBitsoServer) dropAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		_ = c.conn.Close()
	}
	s.conns = nil
}

func (s *fakeBitsoServer) getSubscriptions() []subscribeMsg {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]subscribeMsg, len(s.subscriptions))
	copy(out, s.subscriptions)
	return out
}

// metricCounts is a snapshot of fakeMetrics.
type metricCounts struct {
	connects    int
	disconnects int
	messages    int
	reconnects  int
	errors      int
}

// fakeMetrics records WebSocket lifecycle metrics.
type fakeMetrics struct {
	mu sync.Mutex
	metricCounts
}

func (f *fakeMetrics) RecordWebSocketConnection(connected bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if connected {
		f.connects++
	} else {
		f.disconnects++
	}
}

func (f *fakeMetrics) RecordWebSocketMessage() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages++
}

func (f *fakeMetrics) RecordWebSocketReconnect() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reconnects++
}

func (f *fakeMetrics) RecordWebSocketError() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errors++
}

func (f *fakeMetrics) snapshot() metricCounts {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.metricCounts
}

// TestManager tests the WebSocket manager functionality
func TestManager(t *testing.T) {
	server := newFakeBitsoServer(t)

	// Create test logger
	logger := log.New(os.Stdout, "[TEST-MANAGER] ", log.LstdFlags)

	// Create manager
	config := &ManagerConfig{
		WSURL:             server.URL(),
		ReconnectAttempts: 3,
		ReconnectInterval: 1 * time.Second,
		ReconnectMaxDelay: 5 * time.Second,
		Logger:            logger,
	}
	manager := NewManager(config)

	// Test manager creation
	if manager == nil {
		t.Fatal("Failed to create manager")
	}

	// Test initial connection status
	if manager.IsConnected() {
		t.Error("Expected manager to be disconnected initially")
	}

	// Test connection
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := manager.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	serverConn := server.waitConn()

	// Test subscription
	books := []*bitso.Book{bitso.NewBook(bitso.BTC, bitso.MXN)}
	channels := []string{"trades", "orders"}

	if err := manager.Subscribe(books, channels); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}
	eventually(t, 2*time.Second, func() bool { return len(server.getSubscriptions()) == 2 },
		"server to receive 2 subscriptions")
	for i, sub := range server.getSubscriptions() {
		if sub.Book != "btc_mxn" || sub.Type != channels[i] {
			t.Errorf("Unexpected subscription %d: %+v", i, sub)
		}
	}

	// Test connection status
	if !manager.IsConnected() {
		t.Error("Expected manager to be connected")
	}

	// Test starting the manager
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Test sending a trade message
	serverConn.send(t, tradeJSON(12345))

	// Check if trade was received
	receivedTrade := receive(t, manager.GetTradesStream(), "trade message")
	if receivedTrade == nil {
		t.Fatal("Received nil trade")
	}
	if receivedTrade.Book.String() != "btc_mxn" {
		t.Errorf("Expected book btc_mxn, got %s", receivedTrade.Book.String())
	}
	if len(receivedTrade.Payload) != 1 {
		t.Fatalf("Expected 1 trade payload, got %d", len(receivedTrade.Payload))
	}
	if receivedTrade.Payload[0].TID != 12345 {
		t.Errorf("Expected TID 12345, got %d", receivedTrade.Payload[0].TID)
	}
	if price := receivedTrade.Payload[0].Price.Float64(); price != 50000.0 {
		t.Errorf("Expected price 50000, got %f", price)
	}
	if receivedTrade.Payload[0].MakerSide != 0 {
		t.Errorf("Expected maker side 0 (buy), got %d", receivedTrade.Payload[0].MakerSide)
	}

	// Test sending an order message
	serverConn.send(t, ordersJSON())

	// Check if order was received
	receivedOrder := receive(t, manager.GetOrdersStream(), "order message")
	if receivedOrder == nil {
		t.Fatal("Received nil order")
	}
	if receivedOrder.Book.String() != "btc_mxn" {
		t.Errorf("Expected book btc_mxn, got %s", receivedOrder.Book.String())
	}
	if len(receivedOrder.Payload.Bids) != 1 || receivedOrder.Payload.Bids[0].OrderID != "order-123" {
		t.Errorf("Unexpected order bids: %+v", receivedOrder.Payload.Bids)
	}

	// Test sending a diff-order message
	serverConn.send(t, diffOrdersJSON())

	// Check if diff-order was received
	receivedDiffOrder := receive(t, manager.GetDiffOrdersStream(), "diff-order message")
	if receivedDiffOrder == nil {
		t.Fatal("Received nil diff-order")
	}
	if receivedDiffOrder.Book.String() != "btc_mxn" {
		t.Errorf("Expected book btc_mxn, got %s", receivedDiffOrder.Book.String())
	}
	if len(receivedDiffOrder.Payload) != 1 || receivedDiffOrder.Payload[0].OrderID != "order-123" {
		t.Errorf("Unexpected diff-order payload: %+v", receivedDiffOrder.Payload)
	}

	// Test sending a keep-alive message; it must not reach any stream, so the
	// next trade is the first thing on the trades stream.
	serverConn.send(t, `{"type":"ka"}`)
	serverConn.send(t, tradeJSON(2))
	if next := receive(t, manager.GetTradesStream(), "trade after keep-alive"); next.Payload[0].TID != 2 {
		t.Errorf("Expected trade TID 2 after keep-alive, got %d", next.Payload[0].TID)
	}
	if len(manager.GetOrdersStream()) != 0 || len(manager.GetDiffOrdersStream()) != 0 {
		t.Error("Keep-alive should not be routed to any stream")
	}

	// Stop manager
	if err := manager.Stop(); err != nil {
		t.Errorf("Failed to stop manager: %v", err)
	}
	if manager.IsConnected() {
		t.Error("Expected manager to be disconnected after Stop")
	}
}

// TestManagerNotConnected tests that Subscribe and Start require a connection
func TestManagerNotConnected(t *testing.T) {
	manager := NewManager(&ManagerConfig{
		ReconnectAttempts: 1,
		ReconnectInterval: 10 * time.Millisecond,
		ReconnectMaxDelay: 10 * time.Millisecond,
		Logger:            log.New(os.Stdout, "[TEST-NOT-CONNECTED] ", log.LstdFlags),
	})

	books := []*bitso.Book{bitso.NewBook(bitso.BTC, bitso.MXN)}
	if err := manager.Subscribe(books, []string{"trades"}); err == nil {
		t.Error("Expected Subscribe to fail when not connected")
	}
	if err := manager.Start(context.Background()); err == nil {
		t.Error("Expected Start to fail when not connected")
	}
}

// TestManagerReconnection tests reconnection functionality
func TestManagerReconnection(t *testing.T) {
	server := newFakeBitsoServer(t)
	logger := log.New(os.Stdout, "[TEST-RECONNECT] ", log.LstdFlags)
	metrics := &fakeMetrics{}

	config := &ManagerConfig{
		WSURL:             server.URL(),
		ReconnectAttempts: 3,
		ReconnectInterval: 50 * time.Millisecond,
		ReconnectMaxDelay: 200 * time.Millisecond,
		Logger:            logger,
		MetricsRecorder:   metrics,
	}
	manager := NewManager(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := manager.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	server.waitConn()

	// Subscribe to channels
	books := []*bitso.Book{bitso.NewBook(bitso.BTC, bitso.MXN)}
	channels := []string{"trades"}

	if err := manager.Subscribe(books, channels); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Start manager
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Simulate connection loss
	server.dropAll()

	// The manager should reconnect and resubscribe to the same channels
	newConn := server.waitConn()
	eventually(t, 5*time.Second, func() bool { return len(server.getSubscriptions()) == 2 },
		"manager to resubscribe after reconnect")
	if subs := server.getSubscriptions(); subs[1].Book != "btc_mxn" || subs[1].Type != "trades" {
		t.Errorf("Unexpected resubscription: %+v", subs[1])
	}
	eventually(t, 2*time.Second, manager.IsConnected, "manager to report connected")

	// Messages on the new connection must flow through the message loop
	newConn.send(t, tradeJSON(777))
	if trade := receive(t, manager.GetTradesStream(), "trade after reconnect"); trade.Payload[0].TID != 777 {
		t.Errorf("Expected TID 777 after reconnect, got %d", trade.Payload[0].TID)
	}

	m := metrics.snapshot()
	if m.reconnects != 1 {
		t.Errorf("Expected 1 reconnect recorded, got %d", m.reconnects)
	}
	if m.errors < 1 {
		t.Errorf("Expected connection loss to be recorded as an error, got %d", m.errors)
	}
	if m.connects != 2 || m.disconnects < 1 {
		t.Errorf("Expected 2 connects and at least 1 disconnect, got %+v", m)
	}

	if err := manager.Stop(); err != nil {
		t.Errorf("Failed to stop manager: %v", err)
	}
}

// TestManagerReconnectionGivesUp tests that the manager stops retrying after
// ReconnectAttempts when the server is gone.
func TestManagerReconnectionGivesUp(t *testing.T) {
	server := newFakeBitsoServer(t)

	manager := NewManager(&ManagerConfig{
		WSURL:             server.URL(),
		ReconnectAttempts: 2,
		ReconnectInterval: 10 * time.Millisecond,
		ReconnectMaxDelay: 20 * time.Millisecond,
		Logger:            log.New(os.Stdout, "[TEST-RECONNECT-FAIL] ", log.LstdFlags),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := manager.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	server.waitConn()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Take the server down entirely so every reconnect attempt fails
	server.dropAll()
	server.srv.Close()

	eventually(t, 2*time.Second, func() bool { return !manager.IsConnected() },
		"manager to report disconnected")

	// The message loop exits after exhausting its attempts; Stop must still
	// complete promptly (well under its 10s goroutine timeout).
	time.Sleep(200 * time.Millisecond)
	stopped := make(chan struct{})
	go func() {
		_ = manager.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for manager to stop")
	}
	if manager.IsConnected() {
		t.Error("Expected manager to remain disconnected")
	}
}

// TestManagerMessageRouting tests message routing functionality
func TestManagerMessageRouting(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-ROUTING] ", log.LstdFlags)
	metrics := &fakeMetrics{}

	config := &ManagerConfig{
		ReconnectAttempts: 3,
		ReconnectInterval: 1 * time.Second,
		ReconnectMaxDelay: 5 * time.Second,
		Logger:            logger,
		MetricsRecorder:   metrics,
	}
	manager := NewManager(config)

	// Test routing different message types
	tests := []struct {
		name        string
		msg         interface{}
		receiveFunc func() interface{}
	}{
		{
			name: "Trade message",
			msg:  *decode[bitso.WebSocketTrade](t, tradeJSON(1)),
			receiveFunc: func() interface{} {
				select {
				case msg := <-manager.GetTradesStream():
					return msg
				default:
					return nil
				}
			},
		},
		{
			name: "Order message",
			msg:  *decode[bitso.WebSocketOrder](t, ordersJSON()),
			receiveFunc: func() interface{} {
				select {
				case msg := <-manager.GetOrdersStream():
					return msg
				default:
					return nil
				}
			},
		},
		{
			name: "Diff-order message",
			msg:  *decode[bitso.WebSocketDiffOrder](t, diffOrdersJSON()),
			receiveFunc: func() interface{} {
				select {
				case msg := <-manager.GetDiffOrdersStream():
					return msg
				default:
					return nil
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager.routeMessage(tt.msg)

			msg := tt.receiveFunc()
			if msg == nil {
				t.Errorf("Failed to receive %s", tt.name)
			}
		})
	}

	// Control and unknown messages are not routed to any stream
	t.Run("Control and unknown messages", func(t *testing.T) {
		manager.routeMessage(bitso.WebSocketReply{Type: "ka"})
		manager.routeMessage(bitso.WebSocketReply{Action: "subscribe", Response: "ok", Type: "trades"})
		manager.routeMessage("unexpected")

		if n := len(manager.GetTradesStream()) + len(manager.GetOrdersStream()) + len(manager.GetDiffOrdersStream()); n != 0 {
			t.Errorf("Expected no routed messages, got %d", n)
		}
	})

	if got := metrics.snapshot().messages; got != 6 {
		t.Errorf("Expected 6 messages recorded, got %d", got)
	}
}

// TestManagerBackoffCalculation tests backoff calculation
func TestManagerBackoffCalculation(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-BACKOFF] ", log.LstdFlags)

	config := &ManagerConfig{
		ReconnectAttempts: 5,
		ReconnectInterval: 1 * time.Second,
		ReconnectMaxDelay: 10 * time.Second,
		Logger:            logger,
	}
	manager := NewManager(config)

	// Test backoff calculation
	backoff1 := manager.calculateBackoff(1)
	backoff2 := manager.calculateBackoff(2)
	backoff3 := manager.calculateBackoff(3)

	// Backoff should increase exponentially
	if backoff2 <= backoff1 {
		t.Errorf("Backoff should increase: %v <= %v", backoff2, backoff1)
	}
	if backoff3 <= backoff2 {
		t.Errorf("Backoff should increase: %v <= %v", backoff3, backoff2)
	}

	// Backoff should not exceed max delay
	if backoff1 > config.ReconnectMaxDelay {
		t.Errorf("Backoff exceeds max delay: %v > %v", backoff1, config.ReconnectMaxDelay)
	}
	if backoff2 > config.ReconnectMaxDelay {
		t.Errorf("Backoff exceeds max delay: %v > %v", backoff2, config.ReconnectMaxDelay)
	}
	if backoff3 > config.ReconnectMaxDelay {
		t.Errorf("Backoff exceeds max delay: %v > %v", backoff3, config.ReconnectMaxDelay)
	}
}

// BenchmarkManager benchmarks the manager performance
func BenchmarkManager(b *testing.B) {
	logger := log.New(os.Stdout, "[BENCH-MANAGER] ", log.LstdFlags)

	config := &ManagerConfig{
		ReconnectAttempts: 3,
		ReconnectInterval: 1 * time.Second,
		ReconnectMaxDelay: 5 * time.Second,
		Logger:            logger,
	}
	manager := NewManager(config)

	// Start goroutines to consume messages
	go func() {
		for range manager.GetTradesStream() {
			// Consume trades
		}
	}()
	go func() {
		for range manager.GetOrdersStream() {
			// Consume orders
		}
	}()
	go func() {
		for range manager.GetDiffOrdersStream() {
			// Consume diff-orders
		}
	}()

	var trade bitso.WebSocketTrade
	if err := json.Unmarshal([]byte(tradeJSON(1)), &trade); err != nil {
		b.Fatalf("failed to decode trade: %v", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			manager.routeMessage(trade)
		}
	})

	manager.Stop()
}

// Helper functions to create test messages (Bitso wire format)

func tradeJSON(tid uint64) string {
	now := time.Now().UnixMilli()
	// "t" is the maker side: 0 = buy, 1 = sell
	return fmt.Sprintf(`{"type":"trades","book":"btc_mxn","payload":[{"i":%d,"a":"0.001","r":"50000","v":"50","t":0,"x":%d,"mo":"maker-order-123","to":"taker-order-456"}],"sent":%d}`,
		tid, now, now)
}

func ordersJSON() string {
	return fmt.Sprintf(`{"type":"orders","book":"btc_mxn","payload":{"bids":[{"a":"0.001","o":"order-123","t":0,"r":"50000","s":"open","d":%d,"v":"50"}],"asks":[]}}`,
		time.Now().UnixMilli())
}

func diffOrdersJSON() string {
	return fmt.Sprintf(`{"type":"diff-orders","book":"btc_mxn","sequence":1,"payload":[{"d":%d,"r":"50000","s":"open","t":0,"a":"0.001","v":"50","o":"order-123"}]}`,
		time.Now().UnixMilli())
}

func decode[T any](t *testing.T, raw string) *T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("failed to decode %T: %v", v, err)
	}
	return &v
}

// receive waits for a value on ch or fails the test.
func receive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatalf("Timeout waiting for %s", what)
		var zero T
		return zero
	}
}

// eventually polls cond until it returns true or the timeout elapses.
func eventually(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Timeout waiting for %s", what)
}
