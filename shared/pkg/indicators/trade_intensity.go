package indicators

import "time"

// TradeIntensityPerSecond returns number of trades per second in the given slice.
// Assumes pvs are ordered by time; window is the time span of the slice.
func TradeIntensityPerSecond(count int, window time.Duration) float64 {
	if window <= 0 {
		return 0
	}
	secs := window.Seconds()
	if secs <= 0 {
		return 0
	}
	return float64(count) / secs
}

// TradeIntensityPerMinute returns number of trades per minute.
func TradeIntensityPerMinute(count int, window time.Duration) float64 {
	if window <= 0 {
		return 0
	}
	mins := window.Minutes()
	if mins <= 0 {
		return 0
	}
	return float64(count) / mins
}

// TradeCountAndWindow from a slice of PriceVolume returns count and time span (last - first timestamp).
func TradeCountAndWindow(pvs []PriceVolume) (count int, window time.Duration) {
	if len(pvs) == 0 {
		return 0, 0
	}
	first := pvs[0].Timestamp
	last := pvs[len(pvs)-1].Timestamp
	if last.Before(first) {
		first, last = last, first
	}
	return len(pvs), last.Sub(first)
}
