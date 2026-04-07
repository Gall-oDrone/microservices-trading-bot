package indicators

import (
	"errors"
	"math"
)

// ATR implements Average True Range indicator
type ATR struct {
	period int
}

// NewATR creates a new ATR indicator with the given period (typically 14)
func NewATR(period int) *ATR {
	return &ATR{period: period}
}

func (a *ATR) Name() string {
	return "atr"
}

func (a *ATR) Period() int {
	return a.period
}

// Compute is not applicable for ATR (requires OHLCV data)
func (a *ATR) Compute(prices []float64) (float64, error) {
	return 0, errors.New("ATR requires OHLCV data, use ComputeFromBars instead")
}

// ComputeFromTrades is not applicable for ATR (requires OHLCV data)
func (a *ATR) ComputeFromTrades(trades []Trade) (float64, error) {
	return 0, errors.New("ATR requires OHLCV data, use ComputeFromBars instead")
}

// ComputeFromBars calculates ATR from OHLCV bars
// ATR = SMA of True Range
// True Range = max(High-Low, abs(High-PrevClose), abs(Low-PrevClose))
func (a *ATR) ComputeFromBars(bars []OHLCV) (float64, error) {
	if len(bars) < a.period+1 {
		return 0, errors.New("insufficient bars for ATR calculation (need period + 1 bars)")
	}

	trueRanges := a.computeTrueRanges(bars)

	if len(trueRanges) < a.period {
		return 0, errors.New("insufficient true ranges for ATR calculation")
	}

	sumTR := 0.0
	for i := 0; i < a.period; i++ {
		sumTR += trueRanges[i]
	}
	atr := sumTR / float64(a.period)

	for i := a.period; i < len(trueRanges); i++ {
		atr = (atr*float64(a.period-1) + trueRanges[i]) / float64(a.period)
	}

	return atr, nil
}

// computeTrueRanges calculates True Range for each bar
func (a *ATR) computeTrueRanges(bars []OHLCV) []float64 {
	if len(bars) < 2 {
		return nil
	}

	trueRanges := make([]float64, len(bars)-1)

	for i := 1; i < len(bars); i++ {
		high := bars[i].High
		low := bars[i].Low
		prevClose := bars[i-1].Close

		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)

		trueRanges[i-1] = math.Max(tr1, math.Max(tr2, tr3))
	}

	return trueRanges
}

// ComputeSeries returns ATR values for all possible windows
func (a *ATR) ComputeSeries(bars []OHLCV) ([]float64, error) {
	if len(bars) < a.period+1 {
		return nil, errors.New("insufficient bars for ATR series calculation")
	}

	trueRanges := a.computeTrueRanges(bars)
	result := make([]float64, len(trueRanges)-a.period+1)

	sumTR := 0.0
	for i := 0; i < a.period; i++ {
		sumTR += trueRanges[i]
	}
	atr := sumTR / float64(a.period)
	result[0] = atr

	for i := a.period; i < len(trueRanges); i++ {
		atr = (atr*float64(a.period-1) + trueRanges[i]) / float64(a.period)
		result[i-a.period+1] = atr
	}

	return result, nil
}

// ComputeTrueRange calculates a single True Range value
func (a *ATR) ComputeTrueRange(high, low, prevClose float64) float64 {
	tr1 := high - low
	tr2 := math.Abs(high - prevClose)
	tr3 := math.Abs(low - prevClose)
	return math.Max(tr1, math.Max(tr2, tr3))
}

// GetVolatilityLevel categorizes ATR as Low, Medium, or High volatility
// based on typical price movement
func (a *ATR) GetVolatilityLevel(atr, price float64) string {
	if price == 0 {
		return "unknown"
	}

	atrPercent := (atr / price) * 100

	if atrPercent < 1.0 {
		return "low"
	} else if atrPercent < 3.0 {
		return "medium"
	}
	return "high"
}

// GetStopDistance calculates stop distance based on ATR multiplier
func (a *ATR) GetStopDistance(atr, multiplier float64) float64 {
	return atr * multiplier
}
