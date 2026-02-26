package indicators

import "math"

// BollingerResult holds upper band, middle (SMA), lower band, and width.
type BollingerResult struct {
	Upper  float64
	Middle float64
	Lower  float64
	Width  float64 // (Upper - Lower) / Middle, or 0 if Middle is 0
}

// BollingerBands computes Bollinger Bands (SMA middle, ± numStdDev standard deviations).
func BollingerBands(prices []float64, period int, numStdDev float64) BollingerResult {
	if period <= 0 || len(prices) < period || numStdDev <= 0 {
		return BollingerResult{}
	}
	middle := SMA(prices, period)
	start := len(prices) - period
	variance := 0.0
	for i := start; i < len(prices); i++ {
		variance += (prices[i] - middle) * (prices[i] - middle)
	}
	std := math.Sqrt(variance / float64(period))
	upper := middle + numStdDev*std
	lower := middle - numStdDev*std
	width := 0.0
	if middle != 0 {
		width = (upper - lower) / middle
	}
	return BollingerResult{
		Upper:  upper,
		Middle: middle,
		Lower:  lower,
		Width:  width,
	}
}

// DefaultBollingerStdDev is the typical number of standard deviations (2.0).
const DefaultBollingerStdDev = 2.0
