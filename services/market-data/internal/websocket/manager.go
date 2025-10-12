package websocket

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// StreamManager manages WebSocket connections and streams
type StreamManager interface {
	Connect(ctx context.Context) error
	Subscribe(books []*bitso.Book, channels []string) error
	Start(ctx context.Context) error
	Stop() error
	GetTradesStream() <-chan *bitso.WebSocketTrade
	GetOrdersStream() <-chan *bitso.WebSocketOrder
	GetDiffOrdersStream() <-chan *bitso.WebSocketDiffOrder
	IsConnected() bool
}

// Manager implements StreamManager
type Manager struct {
	wsConn *bitso.WebSocketConn
	logger *log.Logger

	// Reconnection strategy
	reconnectAttempts int
	reconnectInterval time.Duration
	reconnectMaxDelay time.Duration
	currentAttempt    int

	// Channels for different message types
	tradesStream     chan *bitso.WebSocketTrade
	ordersStream     chan *bitso.WebSocketOrder
	diffOrdersStream chan *bitso.WebSocketDiffOrder

	// State management
	connected   bool
	connectedMu sync.RWMutex
	stopChan    chan struct{}
	wg          sync.WaitGroup

	// Subscriptions
	books    []*bitso.Book
	channels []string
}

// ManagerConfig holds configuration for the WebSocket manager
type ManagerConfig struct {
	ReconnectAttempts int
	ReconnectInterval time.Duration
	ReconnectMaxDelay time.Duration
	Logger            *log.Logger
}

// NewManager creates a new WebSocket stream manager
func NewManager(config *ManagerConfig) *Manager {
	logger := config.Logger
	if logger == nil {
		logger = log.New(log.Writer(), "[WS-MANAGER] ", log.LstdFlags|log.Lshortfile)
	}

	return &Manager{
		logger:            logger,
		reconnectAttempts: config.ReconnectAttempts,
		reconnectInterval: config.ReconnectInterval,
		reconnectMaxDelay: config.ReconnectMaxDelay,
		tradesStream:      make(chan *bitso.WebSocketTrade, 100),
		ordersStream:      make(chan *bitso.WebSocketOrder, 100),
		diffOrdersStream:  make(chan *bitso.WebSocketDiffOrder, 100),
		stopChan:          make(chan struct{}),
	}
}

// Connect establishes a WebSocket connection
func (m *Manager) Connect(ctx context.Context) error {
	m.logger.Println("Connecting to Bitso WebSocket...")

	conn, err := bitso.NewWebSocketConn()
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	m.wsConn = conn
	m.setConnected(true)
	m.currentAttempt = 0

	m.logger.Println("✓ Connected to Bitso WebSocket")
	return nil
}

// Subscribe subscribes to specified channels for given books
func (m *Manager) Subscribe(books []*bitso.Book, channels []string) error {
	if m.wsConn == nil {
		return fmt.Errorf("not connected to WebSocket")
	}

	m.books = books
	m.channels = channels

	m.logger.Printf("Subscribing to channels: %v for books: %v", channels, books)

	for _, book := range books {
		for _, channel := range channels {
			if err := m.wsConn.Subscribe(book, channel); err != nil {
				return fmt.Errorf("failed to subscribe to %s for %s: %w", channel, book.String(), err)
			}
			m.logger.Printf("✓ Subscribed to %s channel for %s", channel, book.String())
		}
	}

	return nil
}

// Start begins processing messages from the WebSocket
func (m *Manager) Start(ctx context.Context) error {
	if m.wsConn == nil {
		return fmt.Errorf("not connected to WebSocket")
	}

	m.logger.Println("Starting WebSocket message processor...")

	m.wg.Add(1)
	go m.messageLoop(ctx)

	// Start health monitor
	m.wg.Add(1)
	go m.healthMonitor(ctx)

	m.logger.Println("✓ WebSocket manager started")
	return nil
}

// Stop gracefully stops the WebSocket manager
func (m *Manager) Stop() error {
	m.logger.Println("Stopping WebSocket manager...")

	// Signal stop
	close(m.stopChan)

	// Wait for goroutines
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.logger.Println("All goroutines stopped")
	case <-time.After(10 * time.Second):
		m.logger.Println("Warning: Some goroutines did not stop within timeout")
	}

	// Close WebSocket connection
	if m.wsConn != nil {
		if err := m.wsConn.Close(); err != nil {
			m.logger.Printf("Error closing WebSocket: %v", err)
		}
	}

	// Close channels
	close(m.tradesStream)
	close(m.ordersStream)
	close(m.diffOrdersStream)

	m.setConnected(false)
	m.logger.Println("✓ WebSocket manager stopped")
	return nil
}

// messageLoop continuously processes messages from WebSocket
func (m *Manager) messageLoop(ctx context.Context) {
	defer m.wg.Done()
	m.logger.Println("Message loop started")

	receiveChan := m.wsConn.Receive()

	for {
		select {
		case <-m.stopChan:
			m.logger.Println("Message loop stopping")
			return

		case <-ctx.Done():
			m.logger.Println("Context cancelled, message loop stopping")
			return

		case msg, ok := <-receiveChan:
			if !ok {
				m.logger.Println("WebSocket channel closed, attempting reconnect...")
				m.setConnected(false)

				// Attempt to reconnect
				if err := m.reconnect(ctx); err != nil {
					m.logger.Printf("Reconnection failed: %v", err)
					return
				}

				// Get new receive channel
				receiveChan = m.wsConn.Receive()
				continue
			}

			// Route message to appropriate channel
			m.routeMessage(msg)
		}
	}
}

// routeMessage routes incoming messages to appropriate streams
func (m *Manager) routeMessage(msg interface{}) {
	switch v := msg.(type) {
	case bitso.WebSocketTrade:
		select {
		case m.tradesStream <- &v:
		case <-time.After(1 * time.Second):
			m.logger.Println("Warning: trades stream full, dropping message")
		}

	case bitso.WebSocketOrder:
		select {
		case m.ordersStream <- &v:
		case <-time.After(1 * time.Second):
			m.logger.Println("Warning: orders stream full, dropping message")
		}

	case bitso.WebSocketDiffOrder:
		select {
		case m.diffOrdersStream <- &v:
		case <-time.After(1 * time.Second):
			m.logger.Println("Warning: diff-orders stream full, dropping message")
		}

	case bitso.WebSocketReply:
		// Handle control messages (keep-alive, subscription confirmations)
		if v.Type == "ka" {
			// Keep-alive, ignore
			return
		}
		m.logger.Printf("Received control message: %+v", v)

	default:
		m.logger.Printf("Unknown message type: %T", msg)
	}
}

// reconnect attempts to reconnect with exponential backoff
func (m *Manager) reconnect(ctx context.Context) error {
	m.logger.Println("Starting reconnection procedure...")

	for attempt := 1; attempt <= m.reconnectAttempts; attempt++ {
		m.currentAttempt = attempt

		// Calculate backoff with jitter
		backoff := m.calculateBackoff(attempt)
		m.logger.Printf("Reconnection attempt %d/%d in %v...",
			attempt, m.reconnectAttempts, backoff)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during reconnection")
		case <-m.stopChan:
			return fmt.Errorf("stop signal received during reconnection")
		}

		// Attempt connection
		if err := m.Connect(ctx); err != nil {
			m.logger.Printf("Reconnection attempt %d failed: %v", attempt, err)
			continue
		}

		// Resubscribe to channels
		if err := m.Subscribe(m.books, m.channels); err != nil {
			m.logger.Printf("Resubscription failed: %v", err)
			m.wsConn.Close()
			m.wsConn = nil
			continue
		}

		m.logger.Printf("✓ Successfully reconnected after %d attempts", attempt)
		return nil
	}

	return fmt.Errorf("failed to reconnect after %d attempts", m.reconnectAttempts)
}

// calculateBackoff calculates exponential backoff with jitter
func (m *Manager) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: 2^attempt * base interval
	backoff := time.Duration(math.Pow(2, float64(attempt-1))) * m.reconnectInterval

	// Cap at max delay
	if backoff > m.reconnectMaxDelay {
		backoff = m.reconnectMaxDelay
	}

	// Add jitter (±20%)
	jitter := time.Duration(float64(backoff) * 0.2 * (rand.Float64()*2 - 1))
	backoff += jitter

	return backoff
}

// healthMonitor periodically checks connection health
func (m *Manager) healthMonitor(ctx context.Context) {
	defer m.wg.Done()
	m.logger.Println("Health monitor started")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			m.logger.Println("Health monitor stopping")
			return

		case <-ctx.Done():
			m.logger.Println("Health monitor cancelled")
			return

		case <-ticker.C:
			if !m.IsConnected() {
				m.logger.Println("Warning: WebSocket connection lost")
			}
		}
	}
}

// GetTradesStream returns the trades stream channel
func (m *Manager) GetTradesStream() <-chan *bitso.WebSocketTrade {
	return m.tradesStream
}

// GetOrdersStream returns the orders stream channel
func (m *Manager) GetOrdersStream() <-chan *bitso.WebSocketOrder {
	return m.ordersStream
}

// GetDiffOrdersStream returns the diff-orders stream channel
func (m *Manager) GetDiffOrdersStream() <-chan *bitso.WebSocketDiffOrder {
	return m.diffOrdersStream
}

// IsConnected returns the current connection status
func (m *Manager) IsConnected() bool {
	m.connectedMu.RLock()
	defer m.connectedMu.RUnlock()
	return m.connected
}

// setConnected sets the connection status (thread-safe)
func (m *Manager) setConnected(status bool) {
	m.connectedMu.Lock()
	defer m.connectedMu.Unlock()
	m.connected = status
}
