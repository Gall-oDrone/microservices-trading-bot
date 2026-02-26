package indicators

import "math"

// Volatility returns the standard deviation of returns over the last 'period' prices.
// Returns 0 if insufficient data.
func Volatility(prices []float64, period int) float64 {
	if period < 2 || len(prices) < period+1 {
		return 0
	}
	start := len(prices) - period - 1
	if start < 0 {
		start = 0
	}
	p := prices[start:]
	if len(p) < 2 {
		return 0
	}
	returns := make([]float64, 0, len(p)-1)
	for i := 1; i < len(p); i++ {
		if p[i-1] != 0 {
			returns = append(returns, (p[i]-p[i-1])/p[i-1])
		}
	}
	if len(returns) == 0 {
		return 0
	}
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	variance := 0.0
	for _, r := range returns {
		variance += (r - mean) * (r - mean)
	}
	variance /= float64(len(returns))
	return math.Sqrt(variance)
}
