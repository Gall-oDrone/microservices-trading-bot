package indicators

// Spread returns bid-ask spread (ask - bid) and mid price.
func Spread(bid, ask float64) (spread, mid float64) {
	spread = ask - bid
	mid = (bid + ask) / 2
	return spread, mid
}

// SpreadBps returns spread in basis points relative to mid: (ask-bid)/mid * 10000.
func SpreadBps(bid, ask float64) float64 {
	spread, mid := Spread(bid, ask)
	if mid <= 0 {
		return 0
	}
	return (spread / mid) * 10000
}

// SpreadState indicates tight or wide spread relative to thresholds (in bps).
type SpreadState int

const (
	SpreadUnknown SpreadState = iota
	SpreadTight
	SpreadWide
)

// SpreadStateFromBps returns Tight if bps <= tightBps, Wide if bps >= wideBps, else neutral (Tight/Wide by convention).
func SpreadStateFromBps(bps, tightBps, wideBps float64) SpreadState {
	if bps <= tightBps {
		return SpreadTight
	}
	if bps >= wideBps {
		return SpreadWide
	}
	return SpreadUnknown
}

// DefaultSpreadTightBps is a typical tight spread threshold (e.g. 5 bps).
const DefaultSpreadTightBps = 5.0

// DefaultSpreadWideBps is a typical wide spread threshold (e.g. 50 bps).
const DefaultSpreadWideBps = 50.0
