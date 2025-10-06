package mocks

import (
	"context"
	"time"

	"github.com/stretchr/testify/mock"
)

// MockBitsoClient is a mock implementation of the Bitso client
type MockBitsoClient struct {
	mock.Mock
}

// GetBalance mocks the GetBalance method
func (m *MockBitsoClient) GetBalance(ctx context.Context, currency string) (float64, error) {
	args := m.Called(ctx, currency)
	return args.Get(0).(float64), args.Error(1)
}

// PlaceOrder mocks the PlaceOrder method
func (m *MockBitsoClient) PlaceOrder(ctx context.Context, orderType, side, book string, amount, price float64) (string, error) {
	args := m.Called(ctx, orderType, side, book, amount, price)
	return args.String(0), args.Error(1)
}

// CancelOrder mocks the CancelOrder method
func (m *MockBitsoClient) CancelOrder(ctx context.Context, orderID string) error {
	args := m.Called(ctx, orderID)
	return args.Error(0)
}

// GetOrderBook mocks the GetOrderBook method
func (m *MockBitsoClient) GetOrderBook(ctx context.Context, book string) (interface{}, error) {
	args := m.Called(ctx, book)
	return args.Get(0), args.Error(1)
}

// GetTrades mocks the GetTrades method
func (m *MockBitsoClient) GetTrades(ctx context.Context, book string, limit int) ([]interface{}, error) {
	args := m.Called(ctx, book, limit)
	return args.Get(0).([]interface{}), args.Error(1)
}

// SetRateLimit mocks rate limiting behavior
func (m *MockBitsoClient) SetRateLimit(requestsPerMinute int) {
	m.Called(requestsPerMinute)
}

// GetLastRequestTime mocks getting the last request time
func (m *MockBitsoClient) GetLastRequestTime() time.Time {
	args := m.Called()
	return args.Get(0).(time.Time)
}
