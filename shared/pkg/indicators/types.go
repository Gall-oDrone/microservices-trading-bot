package indicators

import "time"

// PriceVolume represents a single price and volume point (e.g. from a trade).
type PriceVolume struct {
	Price     float64
	Volume    float64
	Timestamp time.Time
	Side      string // "buy" or "sell" for order flow
}

// OHLCV represents a candle/bar for a period.
type OHLCV struct {
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp time.Time
}

// BidAsk represents best bid and ask for spread calculations.
type BidAsk struct {
	Bid       float64
	Ask       float64
	Timestamp time.Time
}

// OrderBookLevel represents one side of the book for imbalance.
type OrderBookLevel struct {
	Price  float64
	Amount float64
}

// OrderBookSnapshot holds bid/ask levels for imbalance calculation.
type OrderBookSnapshot struct {
	Bids    []OrderBookLevel
	Asks    []OrderBookLevel
	Spread  float64 // Ask - Bid
	Mid     float64 // (Bid + Ask) / 2
	Timestamp time.Time
}
