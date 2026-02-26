package indicators

// OrderBookImbalance returns (bidVolume - askVolume) / (bidVolume + askVolume) in [-1, 1].
// Positive = more bid volume (bullish), negative = more ask volume (bearish).
// Levels: typically top N levels each side; volumes are summed from the snapshot.
func OrderBookImbalance(snapshot *OrderBookSnapshot) float64 {
	if snapshot == nil {
		return 0
	}
	bidVol := 0.0
	for _, l := range snapshot.Bids {
		bidVol += l.Amount
	}
	askVol := 0.0
	for _, l := range snapshot.Asks {
		askVol += l.Amount
	}
	total := bidVol + askVol
	if total == 0 {
		return 0
	}
	return (bidVol - askVol) / total
}

// OrderBookImbalanceFromLevels computes imbalance from bid/ask level slices (price, amount pairs).
func OrderBookImbalanceFromLevels(bids, asks []OrderBookLevel) float64 {
	bidVol := 0.0
	for _, l := range bids {
		bidVol += l.Amount
	}
	askVol := 0.0
	for _, l := range asks {
		askVol += l.Amount
	}
	total := bidVol + askVol
	if total == 0 {
		return 0
	}
	return (bidVol - askVol) / total
}
