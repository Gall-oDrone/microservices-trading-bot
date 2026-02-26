package indicators

// SMA computes the Simple Moving Average over the last 'period' values.
func SMA(prices []float64, period int) float64 {
	if period <= 0 || len(prices) < period {
		return 0
	}
	start := len(prices) - period
	sum := 0.0
	for i := start; i < len(prices); i++ {
		sum += prices[i]
	}
	return sum / float64(period)
}

// SMASeries returns SMA for each position; first (period-1) values are zero.
func SMASeries(prices []float64, period int) []float64 {
	if period <= 0 || len(prices) < period {
		return nil
	}
	out := make([]float64, len(prices))
	for i := period - 1; i < len(prices); i++ {
		sum := 0.0
		for j := i - period + 1; j <= i; j++ {
			sum += prices[j]
		}
		out[i] = sum / float64(period)
	}
	return out
}
