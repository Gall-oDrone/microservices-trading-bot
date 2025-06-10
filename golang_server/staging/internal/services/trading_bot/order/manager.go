package order

import (
	"fmt"
	"log"
	"net/url"
	"time"

	"bitso_trading_bot/pkg/bitso"
)

// Manager handles order operations
type Manager struct {
	bitsoClient *bitso.Client
	dbClient    interface{} // TODO: Replace with proper DB client interface
	book        *bitso.Book
}

// NewManager creates a new order manager instance
func NewManager(bitsoClient *bitso.Client, dbClient interface{}, book *bitso.Book) *Manager {
	return &Manager{
		bitsoClient: bitsoClient,
		dbClient:    dbClient,
		book:        book,
	}
}

// PostOrderID saves an order ID to the database
func (m *Manager) PostOrderID(orderID string, side bitso.OrderSide, orderStatus bitso.OrderStatus) error {
	// TODO: Replace with proper DB client method
	err := m.dbClient.(interface {
		PostOrder(string, string, string, string) error
	}).PostOrder(
		m.book.String(),
		orderID,
		side.String(),
		orderStatus.String(),
	)
	if err != nil {
		log.Panicln("error posting order id: ", err)
		return err
	}
	return nil
}

// GetOrderIDsByKey retrieves order IDs by status or side
func (m *Manager) GetOrderIDsByKey(key, value string) (map[string]string, error) {
	var oids map[string]string
	var err error

	switch key {
	case "status":
		// TODO: Replace with proper DB client method
		oids, err = m.dbClient.(interface {
			GetOrdersByStatus(string) (map[string]string, error)
		}).GetOrdersByStatus(value)
	default:
		// TODO: Replace with proper DB client method
		oids, err = m.dbClient.(interface {
			GetOrdersBySide(string) (map[string]string, error)
		}).GetOrdersBySide(value)
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
	// TODO: Replace with proper DB client method
	err := m.dbClient.(interface {
		SetOrderWithTTL(string, time.Duration) error
	}).SetOrderWithTTL(oid, orderTimeout)
	if err != nil {
		return fmt.Errorf("failed to set order TTL: %v", err)
	}
	return nil
}
