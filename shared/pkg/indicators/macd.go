package indicators

// MACDResult holds MACD line, signal line, and histogram.
type MACDResult struct {
	MACD    float64
	Signal  float64
	Histogram float64
}

// MACD computes MACD(fast, slow, signal) for the last value.
// fast, slow, signal are periods (e.g. 12, 26, 9).
func MACD(prices []float64, fast, slow, signal int) MACDResult {
	if fast <= 0 || slow <= 0 || signal <= 0 || len(prices) < slow {
		return MACDResult{}
	}
	macdSeries := MACDSeries(prices, fast, slow)
	if len(macdSeries) == 0 {
		return MACDResult{}
	}
	macdLine := macdSeries[len(macdSeries)-1]
	signalLine := EMA(macdSeries, signal)
	return MACDResult{
		MACD:      macdLine,
		Signal:    signalLine,
		Histogram: macdLine - signalLine,
	}
}

// MACDSeries returns the MACD line (fast EMA - slow EMA) for each point.
func MACDSeries(prices []float64, fast, slow int) []float64 {
	if fast <= 0 || slow <= 0 || len(prices) < slow {
		return nil
	}
	fastEMAs := EMASeries(prices, fast)
	slowEMAs := EMASeries(prices, slow)
	out := make([]float64, len(prices))
	for i := range prices {
		out[i] = fastEMAs[i] - slowEMAs[i]
	}
	return out
}
