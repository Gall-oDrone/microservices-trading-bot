package data

import (
	"context"
	"fmt"
	"time"

	"bitso-trading-platform/backtesting/internal/models"
)

// DataProvider defines the interface for loading historical market data
type DataProvider interface {
	// LoadHistoricalData loads historical data for the given request
	LoadHistoricalData(ctx context.Context, req *DataRequest) ([]models.MarketEvent, error)
	
	// StreamData streams historical data through a channel
	StreamData(ctx context.Context, req *DataRequest) (<-chan models.MarketEvent, error)
	
	// GetDataRange returns the available date range for a book
	GetDataRange(ctx context.Context, book string) (*DateRange, error)
	
	// Close closes the provider and releases resources
	Close() error
}

// DataRequest represents a request for historical data
type DataRequest struct {
	Book        string                    `json:"book"`
	StartDate   time.Time                 `json:"start_date"`
	EndDate     time.Time                 `json:"end_date"`
	EventTypes  []models.MarketEventType  `json:"event_types"` // trades, tickers, orderbooks
	Granularity string                    `json:"granularity"`  // "tick", "1m", "5m", etc.
	Limit       int                       `json:"limit"`        // Max events to fetch (0 = no limit)
}

// DateRange represents an available date range
type DateRange struct {
	FirstDate time.Time `json:"first_date"`
	LastDate  time.Time `json:"last_date"`
}

// NewDataRequest creates a new data request
func NewDataRequest(book string, startDate, endDate time.Time) *DataRequest {
	return &DataRequest{
		Book:        book,
		StartDate:   startDate,
		EndDate:     endDate,
		EventTypes:  []models.MarketEventType{models.EventTypeTrade}, // Default to trades
		Granularity: "tick",                                           // Default to tick data
		Limit:       0,                                                // No limit by default
	}
}

// Validate validates the data request
func (r *DataRequest) Validate() error {
	if r.Book == "" {
		return fmt.Errorf("book is required")
	}
	
	if r.StartDate.IsZero() {
		return fmt.Errorf("start_date is required")
	}
	
	if r.EndDate.IsZero() {
		return fmt.Errorf("end_date is required")
	}
	
	if r.EndDate.Before(r.StartDate) {
		return fmt.Errorf("end_date must be after start_date")
	}
	
	if len(r.EventTypes) == 0 {
		return fmt.Errorf("at least one event type is required")
	}
	
	// Validate event types
	validTypes := map[models.MarketEventType]bool{
		models.EventTypeTrade:     true,
		models.EventTypeTicker:    true,
		models.EventTypeOrderBook: true,
	}
	for _, eventType := range r.EventTypes {
		if !validTypes[eventType] {
			return fmt.Errorf("invalid event type: %s", eventType)
		}
	}
	
	if r.Limit < 0 {
		return fmt.Errorf("limit cannot be negative")
	}
	
	return nil
}

// GetDuration returns the duration of the requested time range
func (r *DataRequest) GetDuration() time.Duration {
	return r.EndDate.Sub(r.StartDate)
}

// GetDays returns the number of days in the requested range
func (r *DataRequest) GetDays() int {
	return int(r.GetDuration().Hours() / 24)
}

// WithEventTypes sets the event types to fetch
func (r *DataRequest) WithEventTypes(types ...models.MarketEventType) *DataRequest {
	r.EventTypes = types
	return r
}

// WithGranularity sets the data granularity
func (r *DataRequest) WithGranularity(granularity string) *DataRequest {
	r.Granularity = granularity
	return r
}

// WithLimit sets the maximum number of events to fetch
func (r *DataRequest) WithLimit(limit int) *DataRequest {
	r.Limit = limit
	return r
}

// String returns a string representation of the request
func (r *DataRequest) String() string {
	return fmt.Sprintf("DataRequest{book=%s, range=%s to %s, types=%v}",
		r.Book,
		r.StartDate.Format("2006-01-02"),
		r.EndDate.Format("2006-01-02"),
		r.EventTypes)
}

