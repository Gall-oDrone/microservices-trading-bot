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

// Manager wraps shared/pkg/bitso.WebSocketConn with reconnect/backoff
// equivalent to services/market-data/internal/websocket.Manager, plus
// disconnect/reconnect hooks for gap recording.
type Manager struct {
	wsURL  string
	wsConn *bitso.WebSocketConn
	logger *log.Logger

	reconnectAttempts int
	reconnectInterval time.Duration
	reconnectMaxDelay time.Duration

	tradesStream chan *bitso.WebSocketTrade

	connected   bool
	connectedMu sync.RWMutex
	stopChan    chan struct{}
	wg          sync.WaitGroup

	books    []*bitso.Book
	channels []string

	onDisconnect func()
	onReconnect  func()
}

// ManagerConfig holds configuration for the WebSocket manager.
type ManagerConfig struct {
	WSURL             string
	ReconnectAttempts int
	ReconnectInterval time.Duration
	ReconnectMaxDelay time.Duration
	Logger            *log.Logger
	OnDisconnect      func()
	OnReconnect       func()
}

// NewManager creates a new WebSocket stream manager.
func NewManager(config *ManagerConfig) *Manager {
	logger := config.Logger
	if logger == nil {
		logger = log.New(log.Writer(), "[WS-MANAGER] ", log.LstdFlags|log.Lshortfile)
	}
	attempts := config.ReconnectAttempts
	if attempts <= 0 {
		attempts = 10
	}
	interval := config.ReconnectInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	maxDelay := config.ReconnectMaxDelay
	if maxDelay <= 0 {
		maxDelay = 30 * time.Second
	}

	return &Manager{
		wsURL:             config.WSURL,
		logger:            logger,
		reconnectAttempts: attempts,
		reconnectInterval: interval,
		reconnectMaxDelay: maxDelay,
		tradesStream:      make(chan *bitso.WebSocketTrade, 100),
		stopChan:          make(chan struct{}),
		onDisconnect:      config.OnDisconnect,
		onReconnect:       config.OnReconnect,
	}
}

// Connect establishes a WebSocket connection.
func (m *Manager) Connect(ctx context.Context) error {
	m.logger.Println("Connecting to Bitso WebSocket...")
	conn, err := bitso.NewWebSocketConnWithURL(m.wsURL)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	m.wsConn = conn
	m.setConnected(true)
	m.logger.Println("Connected to Bitso WebSocket")
	return nil
}

// Subscribe subscribes to specified channels for given books.
func (m *Manager) Subscribe(books []*bitso.Book, channels []string) error {
	if m.wsConn == nil {
		return fmt.Errorf("not connected to WebSocket")
	}
	m.books = books
	m.channels = channels
	for _, book := range books {
		for _, channel := range channels {
			if err := m.wsConn.Subscribe(book, channel); err != nil {
				return fmt.Errorf("failed to subscribe to %s for %s: %w", channel, book.String(), err)
			}
			m.logger.Printf("Subscribed to %s channel for %s", channel, book.String())
		}
	}
	return nil
}

// Start begins processing messages from the WebSocket.
func (m *Manager) Start(ctx context.Context) error {
	if m.wsConn == nil {
		return fmt.Errorf("not connected to WebSocket")
	}
	m.wg.Add(1)
	go m.messageLoop(ctx)
	return nil
}

// Stop gracefully stops the WebSocket manager.
func (m *Manager) Stop() error {
	select {
	case <-m.stopChan:
	default:
		close(m.stopChan)
	}
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		m.logger.Println("Warning: Some goroutines did not stop within timeout")
	}
	if m.wsConn != nil {
		_ = m.wsConn.Close()
	}
	close(m.tradesStream)
	m.setConnected(false)
	return nil
}

func (m *Manager) messageLoop(ctx context.Context) {
	defer m.wg.Done()
	receiveChan := m.wsConn.Receive()
	for {
		select {
		case <-m.stopChan:
			return
		case <-ctx.Done():
			return
		case msg, ok := <-receiveChan:
			if !ok {
				m.logger.Println("WebSocket channel closed, attempting reconnect...")
				m.setConnected(false)
				if m.onDisconnect != nil {
					m.onDisconnect()
				}
				if err := m.reconnect(ctx); err != nil {
					m.logger.Printf("Reconnection failed: %v", err)
					return
				}
				if m.onReconnect != nil {
					m.onReconnect()
				}
				receiveChan = m.wsConn.Receive()
				continue
			}
			m.routeMessage(msg)
		}
	}
}

func (m *Manager) routeMessage(msg interface{}) {
	switch v := msg.(type) {
	case bitso.WebSocketTrade:
		select {
		case m.tradesStream <- &v:
		case <-time.After(1 * time.Second):
			m.logger.Println("Warning: trades stream full, dropping message")
		}
	case bitso.WebSocketReply:
		if v.Type == "ka" {
			return
		}
		m.logger.Printf("Received control message: %+v", v)
	default:
		// Ignore non-trade channels for this collector.
	}
}

func (m *Manager) reconnect(ctx context.Context) error {
	for attempt := 1; attempt <= m.reconnectAttempts; attempt++ {
		backoff := m.calculateBackoff(attempt)
		m.logger.Printf("Reconnection attempt %d/%d in %v...", attempt, m.reconnectAttempts, backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during reconnection")
		case <-m.stopChan:
			return fmt.Errorf("stop signal received during reconnection")
		}
		if err := m.Connect(ctx); err != nil {
			m.logger.Printf("Reconnection attempt %d failed: %v", attempt, err)
			continue
		}
		if err := m.Subscribe(m.books, m.channels); err != nil {
			m.logger.Printf("Resubscription failed: %v", err)
			_ = m.wsConn.Close()
			m.wsConn = nil
			continue
		}
		m.logger.Printf("Successfully reconnected after %d attempts", attempt)
		return nil
	}
	return fmt.Errorf("failed to reconnect after %d attempts", m.reconnectAttempts)
}

func (m *Manager) calculateBackoff(attempt int) time.Duration {
	backoff := time.Duration(math.Pow(2, float64(attempt-1))) * m.reconnectInterval
	if backoff > m.reconnectMaxDelay {
		backoff = m.reconnectMaxDelay
	}
	jitter := time.Duration(float64(backoff) * 0.2 * (rand.Float64()*2 - 1))
	return backoff + jitter
}

// GetTradesStream returns the trades stream channel.
func (m *Manager) GetTradesStream() <-chan *bitso.WebSocketTrade {
	return m.tradesStream
}

// IsConnected returns the current connection status.
func (m *Manager) IsConnected() bool {
	m.connectedMu.RLock()
	defer m.connectedMu.RUnlock()
	return m.connected
}

func (m *Manager) setConnected(status bool) {
	m.connectedMu.Lock()
	defer m.connectedMu.Unlock()
	m.connected = status
}
