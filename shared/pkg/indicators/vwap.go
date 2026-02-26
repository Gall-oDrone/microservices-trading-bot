package indicators

// VWAP computes Volume Weighted Average Price from price-volume pairs.
// VWAP = sum(price * volume) / sum(volume). Typically used for a session or rolling window.
func VWAP(pvs []PriceVolume) float64 {
	if len(pvs) == 0 {
		return 0
	}
	var sumPV, sumV float64
	for _, pv := range pvs {
		sumPV += pv.Price * pv.Volume
		sumV += pv.Volume
	}
	if sumV == 0 {
		return 0
	}
	return sumPV / sumV
}

// VWAPFromSlices computes VWAP from parallel price and volume slices (same length).
func VWAPFromSlices(prices, volumes []float64) float64 {
	if len(prices) == 0 || len(prices) != len(volumes) {
		return 0
	}
	var sumPV, sumV float64
	for i := range prices {
		sumPV += prices[i] * volumes[i]
		sumV += volumes[i]
	}
	if sumV == 0 {
		return 0
	}
	return sumPV / sumV
}
