package simulator

import (
	"fmt"
	"sync"
	"time"
)

// OrderBook represents a simulated order book
type OrderBook struct {
	Book      string
	Timestamp time.Time
	Bids      []PriceLevel
	Asks      []PriceLevel
	mu        sync.RWMutex
}

// PriceLevel represents a price level in the order book
type PriceLevel struct {
	Price  float64 `json:"price"`
	Amount float64 `json:"amount"`
}

// NewOrderBook creates a new order book
func NewOrderBook(book string) *OrderBook {
	return &OrderBook{
		Book:      book,
		Timestamp: time.Now(),
		Bids:      make([]PriceLevel, 0),
		Asks:      make([]PriceLevel, 0),
	}
}

// Update updates the order book with new data
func (ob *OrderBook) Update(bids, asks []PriceLevel, timestamp time.Time) {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	
	ob.Bids = bids
	ob.Asks = asks
	ob.Timestamp = timestamp
}

// GetBestBid returns the highest bid price and amount
func (ob *OrderBook) GetBestBid() (float64, float64, error) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	
	if len(ob.Bids) == 0 {
		return 0, 0, fmt.Errorf("no bids available")
	}
	
	bestBid := ob.Bids[0]
	return bestBid.Price, bestBid.Amount, nil
}

// GetBestAsk returns the lowest ask price and amount
func (ob *OrderBook) GetBestAsk() (float64, float64, error) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	
	if len(ob.Asks) == 0 {
		return 0, 0, fmt.Errorf("no asks available")
	}
	
	bestAsk := ob.Asks[0]
	return bestAsk.Price, bestAsk.Amount, nil
}

// GetMidPrice returns the mid price (average of best bid and ask)
func (ob *OrderBook) GetMidPrice() (float64, error) {
	bidPrice, _, err := ob.GetBestBid()
	if err != nil {
		return 0, err
	}
	
	askPrice, _, err := ob.GetBestAsk()
	if err != nil {
		return 0, err
	}
	
	return (bidPrice + askPrice) / 2, nil
}

// GetSpread returns the bid-ask spread
func (ob *OrderBook) GetSpread() float64 {
	bidPrice, _, err := ob.GetBestBid()
	if err != nil {
		return 0
	}
	
	askPrice, _, err := ob.GetBestAsk()
	if err != nil {
		return 0
	}
	
	return askPrice - bidPrice
}

// CanFillOrder checks if an order can be filled with available liquidity
func (ob *OrderBook) CanFillOrder(side string, amount float64) bool {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	
	if side == "buy" {
		// Check ask side liquidity
		totalAvailable := 0.0
		for _, ask := range ob.Asks {
			totalAvailable += ask.Amount
			if totalAvailable >= amount {
				return true
			}
		}
		return false
	} else {
		// Check bid side liquidity
		totalAvailable := 0.0
		for _, bid := range ob.Bids {
			totalAvailable += bid.Amount
			if totalAvailable >= amount {
				return true
			}
		}
		return false
	}
}

// GetDepth returns the total depth (amount) on each side
func (ob *OrderBook) GetDepth() (bidDepth, askDepth float64) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	
	for _, bid := range ob.Bids {
		bidDepth += bid.Amount
	}
	
	for _, ask := range ob.Asks {
		askDepth += ask.Amount
	}
	
	return bidDepth, askDepth
}

// GetVWAP calculates the volume-weighted average price for an amount
func (ob *OrderBook) GetVWAP(side string, amount float64) (float64, error) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	
	var levels []PriceLevel
	if side == "buy" {
		levels = ob.Asks
	} else {
		levels = ob.Bids
	}
	
	if len(levels) == 0 {
		return 0, fmt.Errorf("no liquidity available")
	}
	
	totalCost := 0.0
	totalAmount := 0.0
	
	for _, level := range levels {
		takeAmount := level.Amount
		if totalAmount+takeAmount > amount {
			takeAmount = amount - totalAmount
		}
		
		totalCost += level.Price * takeAmount
		totalAmount += takeAmount
		
		if totalAmount >= amount {
			break
		}
	}
	
	if totalAmount < amount {
		return 0, fmt.Errorf("insufficient liquidity: need %.8f, available %.8f", amount, totalAmount)
	}
	
	return totalCost / totalAmount, nil
}

