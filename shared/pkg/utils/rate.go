package utils

import (
	"fmt"
)

// MarketSide represents the side of a trade (buy or sell)
type MarketSide string

const (
	MarketSideBuy  MarketSide = "buy"
	MarketSideSell MarketSide = "sell"
)

// Rate represents the current market rates and amount for a trade
type Rate struct {
	BidRate float64 // Current bid rate
	AskRate float64 // Current ask rate
	Amount  float64 // Trade amount
}

// NewRate creates a new Rate instance with validation
func NewRate(bidRate, askRate, amount float64) (*Rate, error) {
	if bidRate <= 0 {
		return nil, fmt.Errorf("invalid bid rate: must be greater than 0, got %.8f", bidRate)
	}
	if askRate <= 0 {
		return nil, fmt.Errorf("invalid ask rate: must be greater than 0, got %.8f", askRate)
	}
	if amount <= 0 {
		return nil, fmt.Errorf("invalid amount: must be greater than 0, got %.8f", amount)
	}
	if bidRate >= askRate {
		return nil, fmt.Errorf("invalid rates: bid rate (%.8f) must be less than ask rate (%.8f)", bidRate, askRate)
	}

	return &Rate{
		BidRate: bidRate,
		AskRate: askRate,
		Amount:  amount,
	}, nil
}

// CalculateTotal calculates the total value of a trade including fees and limits
func (r *Rate) CalculateTotal(value, fee, limit float64, side MarketSide) float64 {
	switch side {
	case MarketSideSell:
		// For sell orders, we use the bid rate
		if r.BidRate < (value - fee - limit) {
			// If bid rate is lower than our target, we adjust down
			return (value - limit) / r.Amount
		}
		// Otherwise, we include fees and limits
		return (value + fee + limit) / r.Amount

	case MarketSideBuy:
		// For buy orders, we use the ask rate
		if r.AskRate < (value - fee - limit) {
			// If ask rate is lower than our target, we adjust down
			return (value - limit)
		}
		// Otherwise, we include fees and limits
		return r.Amount / (value + fee + limit)

	default:
		// This should never happen due to type safety, but just in case
		return 0
	}
}

// CalculateValue calculates the total value of a trade based on the market side
func (r *Rate) CalculateValue(side MarketSide) float64 {
	switch side {
	case MarketSideSell:
		// For sell orders, value = amount * bid rate
		return r.Amount * r.BidRate

	case MarketSideBuy:
		// For buy orders, value = amount / ask rate
		return r.Amount / r.AskRate

	default:
		// This should never happen due to type safety, but just in case
		return 0
	}
}

// GetSpread returns the current spread between bid and ask rates
func (r *Rate) GetSpread() float64 {
	return r.AskRate - r.BidRate
}

// GetSpreadPercentage returns the spread as a percentage of the bid rate
func (r *Rate) GetSpreadPercentage() float64 {
	return (r.GetSpread() / r.BidRate) * 100
}

// IsProfitable checks if a trade would be profitable given the current rates
func (r *Rate) IsProfitable(side MarketSide, minProfitPercentage float64) bool {
	switch side {
	case MarketSideSell:
		// For sell orders, check if bid rate is profitable
		return r.BidRate >= r.AskRate*(1+minProfitPercentage/100)

	case MarketSideBuy:
		// For buy orders, check if ask rate is profitable
		return r.AskRate <= r.BidRate*(1-minProfitPercentage/100)

	default:
		return false
	}
}
