package websocket

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// MockWebSocketConn is a mock implementation of bitso.WebSocketConn for testing
type MockWebSocketConn struct {
	receiveChan chan interface{}
	connected   bool
	subscribed  map[string][]string // book -> channels
}

func NewMockWebSocketConn() *MockWebSocketConn {
	return &MockWebSocketConn{
		receiveChan: make(chan interface{}, 100),
		connected:   true,
		subscribed:  make(map[string][]string),
	}
}

func (m *MockWebSocketConn) Subscribe(book *bitso.Book, channel string) error {
	bookStr := book.String()
	if m.subscribed[bookStr] == nil {
		m.subscribed[bookStr] = make([]string, 0)
	}
	m.subscribed[bookStr] = append(m.subscribed[bookStr], channel)
	return nil
}

func (m *MockWebSocketConn) Receive() <-chan interface{} {
	return m.receiveChan
}

func (m *MockWebSocketConn) Close() error {
	m.connected = false
	close(m.receiveChan)
	return nil
}

func (m *MockWebSocketConn) SendTrade(trade *bitso.WebSocketTrade) {
	if m.connected {
		select {
		case m.receiveChan <- *trade:
		default:
			// Channel full, drop message
		}
	}
}

func (m *MockWebSocketConn) SendOrder(order *bitso.WebSocketOrder) {
	if m.connected {
		select {
		case m.receiveChan <- *order:
		default:
			// Channel full, drop message
		}
	}
}

func (m *MockWebSocketConn) SendDiffOrder(diffOrder *bitso.WebSocketDiffOrder) {
	if m.connected {
		select {
		case m.receiveChan <- *diffOrder:
		default:
			// Channel full, drop message
		}
	}
}

func (m *MockWebSocketConn) SendReply(reply *bitso.WebSocketReply) {
	if m.connected {
		select {
		case m.receiveChan <- *reply:
		default:
			// Channel full, drop message
		}
	}
}

func (m *MockWebSocketConn) Disconnect() {
	m.connected = false
	close(m.receiveChan)
}

// TestManager tests the WebSocket manager functionality
func TestManager(t *testing.T) {
	// Create test logger
	logger := log.New(os.Stdout, "[TEST-MANAGER] ", log.LstdFlags)

	// Create manager
	config := &ManagerConfig{
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

	// Mock the WebSocket connection
	mockConn := NewMockWebSocketConn()
	manager.wsConn = mockConn
	manager.setConnected(true)

	// Test subscription
	books := []*bitso.Book{bitso.ToBook("btc_mxn")}
	channels := []string{"trades", "orders"}

	if err := manager.Subscribe(books, channels); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
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
	testTrade := createTestWebSocketTrade()
	mockConn.SendTrade(testTrade)

	// Wait for message processing
	time.Sleep(100 * time.Millisecond)

	// Check if trade was received
	select {
	case receivedTrade := <-manager.GetTradesStream():
		if receivedTrade == nil {
			t.Fatal("Received nil trade")
		}
		if receivedTrade.Book.String() != "btc_mxn" {
			t.Errorf("Expected book btc_mxn, got %s", receivedTrade.Book.String())
		}
		if receivedTrade.Payload[0].TID != 12345 {
			t.Errorf("Expected TID 12345, got %d", receivedTrade.Payload[0].TID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for trade message")
	}

	// Test sending an order message
	testOrder := createTestWebSocketOrder()
	mockConn.SendOrder(testOrder)

	// Wait for message processing
	time.Sleep(100 * time.Millisecond)

	// Check if order was received
	select {
	case receivedOrder := <-manager.GetOrdersStream():
		if receivedOrder == nil {
			t.Fatal("Received nil order")
		}
		if receivedOrder.Book.String() != "btc_mxn" {
			t.Errorf("Expected book btc_mxn, got %s", receivedOrder.Book.String())
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for order message")
	}

	// Test sending a diff-order message
	testDiffOrder := createTestWebSocketDiffOrder()
	mockConn.SendDiffOrder(testDiffOrder)

	// Wait for message processing
	time.Sleep(100 * time.Millisecond)

	// Check if diff-order was received
	select {
	case receivedDiffOrder := <-manager.GetDiffOrdersStream():
		if receivedDiffOrder == nil {
			t.Fatal("Received nil diff-order")
		}
		if receivedDiffOrder.Book.String() != "btc_mxn" {
			t.Errorf("Expected book btc_mxn, got %s", receivedDiffOrder.Book.String())
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for diff-order message")
	}

	// Test sending a keep-alive message
	keepAlive := &bitso.WebSocketReply{
		Type: "ka",
	}
	mockConn.SendReply(keepAlive)

	// Wait for message processing
	time.Sleep(100 * time.Millisecond)

	// Keep-alive should be ignored (no error expected)

	// Stop manager
	if err := manager.Stop(); err != nil {
		t.Errorf("Failed to stop manager: %v", err)
	}
}

// TestManagerReconnection tests reconnection functionality
func TestManagerReconnection(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-RECONNECT] ", log.LstdFlags)

	config := &ManagerConfig{
		ReconnectAttempts: 2,
		ReconnectInterval: 100 * time.Millisecond,
		ReconnectMaxDelay: 1 * time.Second,
		Logger:            logger,
	}
	manager := NewManager(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Mock the WebSocket connection
	mockConn := NewMockWebSocketConn()
	manager.wsConn = mockConn
	manager.setConnected(true)

	// Subscribe to channels
	books := []*bitso.Book{bitso.ToBook("btc_mxn")}
	channels := []string{"trades"}

	if err := manager.Subscribe(books, channels); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Start manager
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Simulate connection loss
	mockConn.Disconnect()
	manager.setConnected(false)

	// Wait for reconnection attempt
	time.Sleep(200 * time.Millisecond)

	// The manager should attempt to reconnect
	// Note: In a real test, we would need to mock the reconnection logic
	// For now, we just verify the manager handles disconnection gracefully

	manager.Stop()
}

// TestManagerMessageRouting tests message routing functionality
func TestManagerMessageRouting(t *testing.T) {
	logger := log.New(os.Stdout, "[TEST-ROUTING] ", log.LstdFlags)

	config := &ManagerConfig{
		ReconnectAttempts: 3,
		ReconnectInterval: 1 * time.Second,
		ReconnectMaxDelay: 5 * time.Second,
		Logger:            logger,
	}
	manager := NewManager(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Mock the WebSocket connection
	mockConn := NewMockWebSocketConn()
	manager.wsConn = mockConn
	manager.setConnected(true)

	// Start manager
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Test routing different message types
	tests := []struct {
		name        string
		sendFunc    func()
		receiveFunc func() interface{}
	}{
		{
			name: "Trade message",
			sendFunc: func() {
				trade := createTestWebSocketTrade()
				mockConn.SendTrade(trade)
			},
			receiveFunc: func() interface{} {
				select {
				case msg := <-manager.GetTradesStream():
					return msg
				case <-time.After(1 * time.Second):
					return nil
				}
			},
		},
		{
			name: "Order message",
			sendFunc: func() {
				order := createTestWebSocketOrder()
				mockConn.SendOrder(order)
			},
			receiveFunc: func() interface{} {
				select {
				case msg := <-manager.GetOrdersStream():
					return msg
				case <-time.After(1 * time.Second):
					return nil
				}
			},
		},
		{
			name: "Diff-order message",
			sendFunc: func() {
				diffOrder := createTestWebSocketDiffOrder()
				mockConn.SendDiffOrder(diffOrder)
			},
			receiveFunc: func() interface{} {
				select {
				case msg := <-manager.GetDiffOrdersStream():
					return msg
				case <-time.After(1 * time.Second):
					return nil
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.sendFunc()
			time.Sleep(50 * time.Millisecond)

			msg := tt.receiveFunc()
			if msg == nil {
				t.Errorf("Failed to receive %s", tt.name)
			}
		})
	}

	manager.Stop()
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Mock the WebSocket connection
	mockConn := NewMockWebSocketConn()
	manager.wsConn = mockConn
	manager.setConnected(true)

	// Start manager
	manager.Start(ctx)

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

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			trade := createTestWebSocketTrade()
			trade.Payload[0].TID = uint64(time.Now().UnixNano())
			mockConn.SendTrade(trade)
		}
	})

	manager.Stop()
}

// Helper functions to create test messages

func createTestWebSocketTrade() *bitso.WebSocketTrade {
	return &bitso.WebSocketTrade{
		Book: bitso.ToBook("btc_mxn"),
		Payload: []bitso.WebSocketTradePayload{
			{
				TID:               12345,
				Price:             bitso.Monetary{Value: 50000.0},
				Amount:            bitso.Monetary{Value: 0.001},
				Value:             bitso.Monetary{Value: 50.0},
				MakerOrderID:      "maker-order-123",
				TakerOrderID:      "taker-order-456",
				MakerSide:         "0",
				CreationTimestamp: uint64(time.Now().UnixMilli()),
			},
		},
		Sent: uint64(time.Now().UnixMilli()),
	}
}

func createTestWebSocketOrder() *bitso.WebSocketOrder {
	return &bitso.WebSocketOrder{
		Book: bitso.ToBook("btc_mxn"),
		Payload: []bitso.WebSocketOrderPayload{
			{
				OID:    "order-123",
				Side:   "0",
				Price:  bitso.Monetary{Value: 50000.0},
				Amount: bitso.Monetary{Value: 0.001},
				Status: "open",
			},
		},
		Sent: uint64(time.Now().UnixMilli()),
	}
}

func createTestWebSocketDiffOrder() *bitso.WebSocketDiffOrder {
	return &bitso.WebSocketDiffOrder{
		Book: bitso.ToBook("btc_mxn"),
		Payload: []bitso.WebSocketDiffOrderPayload{
			{
				OID:    "order-123",
				Side:   "0",
				Price:  bitso.Monetary{Value: 50000.0},
				Amount: bitso.Monetary{Value: 0.001},
				Status: "open",
			},
		},
		Sent: uint64(time.Now().UnixMilli()),
	}
}
