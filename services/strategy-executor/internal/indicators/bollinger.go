package indicators

import (
	"errors"
	"math"
)

// Bollinger implements Bollinger Bands indicator
type Bollinger struct {
	period int
	stdDev float64
}

// NewBollinger creates a new Bollinger Bands indicator
// period: lookback period (typically 20)
// stdDev: number of standard deviations (typically 2.0)
func NewBollinger(period int, stdDev float64) *Bollinger {
	return &Bollinger{period: period, stdDev: stdDev}
}

func (b *Bollinger) Name() string {
	return "bollinger"
}

func (b *Bollinger) Period() int {
	return b.period
}

// Compute calculates Bollinger Bands middle value (SMA) from prices
func (b *Bollinger) Compute(prices []float64) (float64, error) {
	bb, err := b.ComputeBands(prices)
	if err != nil {
		return 0, err
	}
	return bb.Middle, nil
}

// ComputeBands calculates full Bollinger Bands (upper, middle, lower)
func (b *Bollinger) ComputeBands(prices []float64) (*BollingerBands, error) {
	if len(prices) < b.period {
		return nil, errors.New("insufficient data for Bollinger Bands calculation")
	}

	sma := NewSMA(b.period)
	middle, err := sma.Compute(prices)
	if err != nil {
		return nil, err
	}

	start := len(prices) - b.period
	sumSquares := 0.0
	for i := start; i < len(prices); i++ {
		diff := prices[i] - middle
		sumSquares += diff * diff
	}

	stdDevValue := math.Sqrt(sumSquares / float64(b.period))

	return &BollingerBands{
		Upper:  middle + b.stdDev*stdDevValue,
		Middle: middle,
		Lower:  middle - b.stdDev*stdDevValue,
		StdDev: stdDevValue,
	}, nil
}

// ComputeFromTrades calculates Bollinger Bands from trade data
func (b *Bollinger) ComputeFromTrades(trades []Trade) (float64, error) {
	prices := make([]float64, len(trades))
	for i, t := range trades {
		prices[i] = t.Price
	}
	return b.Compute(prices)
}

// ComputeBandsFromTrades calculates full Bollinger Bands from trade data
func (b *Bollinger) ComputeBandsFromTrades(trades []Trade) (*BollingerBands, error) {
	prices := make([]float64, len(trades))
	for i, t := range trades {
		prices[i] = t.Price
	}
	return b.ComputeBands(prices)
}

// ComputeFromBars calculates Bollinger Bands from OHLCV bars using close prices
func (b *Bollinger) ComputeFromBars(bars []OHLCV) (float64, error) {
	prices := make([]float64, len(bars))
	for i, bar := range bars {
		prices[i] = bar.Close
	}
	return b.Compute(prices)
}

// ComputeBandsFromBars calculates full Bollinger Bands from OHLCV bars
func (b *Bollinger) ComputeBandsFromBars(bars []OHLCV) (*BollingerBands, error) {
	prices := make([]float64, len(bars))
	for i, bar := range bars {
		prices[i] = bar.Close
	}
	return b.ComputeBands(prices)
}

// ComputeBandsSeries returns Bollinger Bands for all possible windows
func (b *Bollinger) ComputeBandsSeries(prices []float64) ([]*BollingerBands, error) {
	if len(prices) < b.period {
		return nil, errors.New("insufficient data for Bollinger Bands series calculation")
	}

	result := make([]*BollingerBands, len(prices)-b.period+1)

	for i := 0; i <= len(prices)-b.period; i++ {
		window := prices[i : i+b.period]
		bb, err := b.ComputeBands(window)
		if err != nil {
			return nil, err
		}
		result[i] = bb
	}

	return result, nil
}

// GetBandWidth returns the Bollinger Band Width indicator
// BandWidth = (Upper - Lower) / Middle * 100
func (b *Bollinger) GetBandWidth(bb *BollingerBands) float64 {
	if bb.Middle == 0 {
		return 0
	}
	return (bb.Upper - bb.Lower) / bb.Middle * 100
}

// GetPercentB returns the %B indicator
// %B = (Price - Lower) / (Upper - Lower)
// Values > 1 indicate price is above upper band
// Values < 0 indicate price is below lower band
func (b *Bollinger) GetPercentB(price float64, bb *BollingerBands) float64 {
	bandwidth := bb.Upper - bb.Lower
	if bandwidth == 0 {
		return 0.5
	}
	return (price - bb.Lower) / bandwidth
}

// IsPriceAboveUpper returns true if price is above the upper band
func (b *Bollinger) IsPriceAboveUpper(price float64, bb *BollingerBands) bool {
	return price > bb.Upper
}

// IsPriceBelowLower returns true if price is below the lower band
func (b *Bollinger) IsPriceBelowLower(price float64, bb *BollingerBands) bool {
	return price < bb.Lower
}
