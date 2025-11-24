package database

import (
	"time"
)

// BidTradeTrendConsumer represents a batch of trades for trend analysis
type BidTradeTrendConsumer struct {
	BatchId     int       `json:"batch_id"`
	Index       int       `json:"index"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	// Add other fields as needed
}
