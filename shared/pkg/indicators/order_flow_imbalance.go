package indicators

// OrderFlowImbalance returns (buyVolume - sellVolume) / (buyVolume + sellVolume) in [-1, 1].
// pvs should have Side set to "buy" or "sell" per trade.
func OrderFlowImbalance(pvs []PriceVolume) float64 {
	if len(pvs) == 0 {
		return 0
	}
	var buyVol, sellVol float64
	for _, pv := range pvs {
		switch pv.Side {
		case "buy":
			buyVol += pv.Volume
		case "sell":
			sellVol += pv.Volume
		}
	}
	total := buyVol + sellVol
	if total == 0 {
		return 0
	}
	return (buyVol - sellVol) / total
}
