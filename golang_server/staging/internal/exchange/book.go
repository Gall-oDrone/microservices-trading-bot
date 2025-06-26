package exchange

import (
	"log"

	"bitso_trading_bot/pkg/bitso"
)

// BookManager handles exchange book operations
type BookManager struct {
	bitsoClient  *bitso.Client
	book         *bitso.Book
	exchangeBook bitso.ExchangeOrderBook
}

// NewBookManager creates a new book manager instance
func NewBookManager(bitsoClient *bitso.Client, book *bitso.Book) *BookManager {
	return &BookManager{
		bitsoClient: bitsoClient,
		book:        book,
	}
}

// GetTicker retrieves the current ticker for the book
func (m *BookManager) GetTicker() (*bitso.Ticker, error) {
	return m.bitsoClient.Ticker(m.book)
}

// GetMinMinorValue returns the minimum value in minor currency
func (m *BookManager) GetMinMinorValue() float64 {
	return m.exchangeBook.MinimumValue.Float64()
}

// CheckMinMinorValue verifies if a value meets the minimum minor currency requirement
func (m *BookManager) CheckMinMinorValue(value float64) bool {
	if value < m.exchangeBook.MinimumValue.Float64() {
		log.Panicf("minor value to buy is less than minimum value to trade.")
		return false
	}
	return true
}

// CheckMaxMinorValue verifies if a value meets the maximum minor currency requirement
func (m *BookManager) CheckMaxMinorValue(value float64) bool {
	if value > m.exchangeBook.MaximumValue.Float64() {
		log.Panicf("minor value to buy is more than maximum value to trade.")
		return false
	}
	return true
}

// GetMinMajorAmount returns the minimum amount in major currency
func (m *BookManager) GetMinMajorAmount() float64 {
	return m.exchangeBook.MinimumAmount.Float64()
}

// GetSpread calculates the current spread between ask and bid
func (m *BookManager) GetSpread() (float64, error) {
	ticker, err := m.GetTicker()
	if err != nil {
		return 0, err
	}
	return ticker.Ask.Float64() - ticker.Bid.Float64(), nil
}

// SetExchangeBook updates the exchange book information
func (m *BookManager) SetExchangeBook(book bitso.ExchangeOrderBook) {
	m.exchangeBook = book
}
