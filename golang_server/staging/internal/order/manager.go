package order

import (
	"fmt"
	"log"
	"net/url"
	"time"

	"bitso_trading_bot/internal/database"
	"bitso_trading_bot/pkg/bitso"
)

// Manager handles order operations
type Manager struct {
	bitsoClient *bitso.Client
	dbClient    database.RedisClient
	book        *bitso.Book
	orderQueue  []string // Queue of order IDs
}

// NewManager creates a new order manager instance
func NewManager(bitsoClient *bitso.Client, dbClient database.RedisClient, book *bitso.Book) *Manager {
	return &Manager{
		bitsoClient: bitsoClient,
		dbClient:    dbClient,
		book:        book,
		orderQueue:  make([]string, 0),
	}
}

// GetBitsoClient returns the bitso client
func (m *Manager) GetBitsoClient() *bitso.Client {
	return m.bitsoClient
}

// GetBitsoClientBook returns the bitso book
func (m *Manager) GetBitsoBook() *bitso.Book {
	return m.book
}

// GetDBClient returns the db client
func (m *Manager) GetDBClient() *database.RedisClient {
	return &m.dbClient
}

// SaveOrder saves an order ID to the database
func (m *Manager) SaveOrder(orderID string, side bitso.OrderSide, orderStatus bitso.OrderStatus) error {
	err := m.dbClient.SaveUserOrder(bitso.UserOrder{
		OID:    orderID,
		Side:   side,
		Status: orderStatus,
		Book:   *m.book,
	})
	if err != nil {
		log.Panicln("error posting order id: ", err)
		return err
	}
	return nil
}

// GetOrdersByFilter retrieves order IDs by status or side
func (m *Manager) GetOrdersByFilter(key, value string) (map[string]string, error) {
	var oids map[string]string
	var err error

	switch key {
	case "status":
		oids, err = m.dbClient.GetOrdersByStatus(value)
	default:
		oids, err = m.dbClient.GetOrdersBySide(value)
	}

	if err != nil {
		log.Panicln("error getting order ids: ", err)
	}
	return oids, err
}

// GetOpenOrders retrieves open orders from Bitso
func (m *Manager) GetOpenOrders() ([]bitso.UserOrder, error) {
	params := url.Values{}
	params.Add("book", m.book.String())
	return m.bitsoClient.MyOpenOrders(params)
}

// LookupOrder retrieves a specific order from Bitso
func (m *Manager) LookupOrder(oid string) (*bitso.UserOrder, error) {
	return m.bitsoClient.LookupOrder(oid)
}

// CancelOrder cancels an order on Bitso
func (m *Manager) CancelOrder(oid string) ([]string, error) {
	return m.bitsoClient.CancelOrder(oid)
}

// SetOrderWithTTL sets an order with a time-to-live in the database
func (m *Manager) SetOrderWithTTL(oid string) error {
	orderTimeout := 5 * time.Minute
	err := m.dbClient.SetOrderWithTTL(oid, orderTimeout)
	if err != nil {
		return fmt.Errorf("failed to set order TTL: %v", err)
	}
	return nil
}

// Queue Operations

// AppendToQueue adds an order ID to the queue
func (m *Manager) AppendToQueue(oid string) error {
	if oid == "" {
		return fmt.Errorf("order ID cannot be empty")
	}
	m.orderQueue = append(m.orderQueue, oid)
	return nil
}

// RemoveFromQueue removes an order ID from the queue
func (m *Manager) RemoveFromQueue(oid string) error {
	if oid == "" {
		return fmt.Errorf("order ID cannot be empty")
	}
	for i, orderID := range m.orderQueue {
		if orderID == oid {
			m.orderQueue = append(m.orderQueue[:i], m.orderQueue[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("order ID %s not found in queue", oid)
}

// GetQueueOrders returns the list of orders in the queue
func (m *Manager) GetQueueOrders() []string {
	return m.orderQueue
}

// ClearQueue clears all orders from the queue
func (m *Manager) ClearQueue() {
	m.orderQueue = make([]string, 0)
}
