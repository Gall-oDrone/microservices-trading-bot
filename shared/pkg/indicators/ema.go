package indicators

// EMA computes the Exponential Moving Average for the last value in the series.
// multiplier = 2 / (period + 1). Uses the first value as seed for the first EMA.
func EMA(prices []float64, period int) float64 {
	if period <= 0 || len(prices) == 0 {
		return 0
	}
	if len(prices) < period {
		period = len(prices)
	}
	mult := 2.0 / float64(period+1)
	ema := prices[0]
	for i := 1; i < len(prices); i++ {
		ema = (prices[i]-ema)*mult + ema
	}
	return ema
}

// EMASeries returns EMA for each point (first period-1 values use partial warm-up).
func EMASeries(prices []float64, period int) []float64 {
	if period <= 0 || len(prices) == 0 {
		return nil
	}
	mult := 2.0 / float64(period+1)
	out := make([]float64, len(prices))
	out[0] = prices[0]
	for i := 1; i < len(prices); i++ {
		out[i] = (prices[i]-out[i-1])*mult + out[i-1]
	}
	return out
}
