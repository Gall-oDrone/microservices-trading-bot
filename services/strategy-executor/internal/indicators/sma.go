package indicators

import (
	"errors"
)

// SMA implements Simple Moving Average indicator
type SMA struct {
	period int
}

// NewSMA creates a new SMA indicator with the given period
func NewSMA(period int) *SMA {
	return &SMA{period: period}
}

func (s *SMA) Name() string {
	return "sma"
}

func (s *SMA) Period() int {
	return s.period
}

// Compute calculates SMA from a slice of prices (most recent last)
func (s *SMA) Compute(prices []float64) (float64, error) {
	if len(prices) < s.period {
		return 0, errors.New("insufficient data for SMA calculation")
	}

	sum := 0.0
	start := len(prices) - s.period
	for i := start; i < len(prices); i++ {
		sum += prices[i]
	}

	return sum / float64(s.period), nil
}

// ComputeFromTrades calculates SMA from trade data
func (s *SMA) ComputeFromTrades(trades []Trade) (float64, error) {
	if len(trades) < s.period {
		return 0, errors.New("insufficient trades for SMA calculation")
	}

	prices := make([]float64, len(trades))
	for i, t := range trades {
		prices[i] = t.Price
	}

	return s.Compute(prices)
}

// ComputeFromBars calculates SMA from OHLCV bars using close prices
func (s *SMA) ComputeFromBars(bars []OHLCV) (float64, error) {
	if len(bars) < s.period {
		return 0, errors.New("insufficient bars for SMA calculation")
	}

	prices := make([]float64, len(bars))
	for i, bar := range bars {
		prices[i] = bar.Close
	}

	return s.Compute(prices)
}

// ComputeSeries returns SMA values for all possible windows in the price series
func (s *SMA) ComputeSeries(prices []float64) ([]float64, error) {
	if len(prices) < s.period {
		return nil, errors.New("insufficient data for SMA series calculation")
	}

	result := make([]float64, len(prices)-s.period+1)
	
	windowSum := 0.0
	for i := 0; i < s.period; i++ {
		windowSum += prices[i]
	}
	result[0] = windowSum / float64(s.period)

	for i := s.period; i < len(prices); i++ {
		windowSum = windowSum - prices[i-s.period] + prices[i]
		result[i-s.period+1] = windowSum / float64(s.period)
	}

	return result, nil
}
