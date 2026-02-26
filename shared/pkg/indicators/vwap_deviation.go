package indicators

// VWAPDeviation returns (price - vwap) / vwap, i.e. relative deviation from VWAP.
// Used as institutional indicator: price above VWAP = bullish deviation, below = bearish.
func VWAPDeviation(price, vwap float64) float64 {
	if vwap <= 0 {
		return 0
	}
	return (price - vwap) / vwap
}

// VWAPDeviationBps returns deviation in basis points.
func VWAPDeviationBps(price, vwap float64) float64 {
	return VWAPDeviation(price, vwap) * 10000
}
