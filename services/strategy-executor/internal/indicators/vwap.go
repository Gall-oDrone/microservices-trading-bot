package indicators

import (
	"errors"
)

// VWAP implements Volume Weighted Average Price indicator
type VWAP struct {
	period int
}

// NewVWAP creates a new VWAP indicator
// period: lookback period (0 = use all available data, typical for intraday)
func NewVWAP(period int) *VWAP {
	return &VWAP{period: period}
}

func (v *VWAP) Name() string {
	return "vwap"
}

func (v *VWAP) Period() int {
	return v.period
}

// Compute is not applicable for VWAP (requires volume data)
func (v *VWAP) Compute(prices []float64) (float64, error) {
	return 0, errors.New("VWAP requires volume data, use ComputeFromTrades or ComputeFromBars")
}

// ComputeFromTrades calculates VWAP from trade data
// VWAP = Σ(Price × Volume) / Σ(Volume)
func (v *VWAP) ComputeFromTrades(trades []Trade) (float64, error) {
	if len(trades) == 0 {
		return 0, errors.New("no trades provided for VWAP calculation")
	}

	startIdx := 0
	if v.period > 0 && len(trades) > v.period {
		startIdx = len(trades) - v.period
	}

	sumPriceVolume := 0.0
	sumVolume := 0.0

	for i := startIdx; i < len(trades); i++ {
		priceVolume := trades[i].Price * trades[i].Amount
		sumPriceVolume += priceVolume
		sumVolume += trades[i].Amount
	}

	if sumVolume == 0 {
		return 0, errors.New("total volume is zero, cannot compute VWAP")
	}

	return sumPriceVolume / sumVolume, nil
}

// ComputeFromBars calculates VWAP from OHLCV bars
// Typical Price = (High + Low + Close) / 3
// VWAP = Σ(TypicalPrice × Volume) / Σ(Volume)
func (v *VWAP) ComputeFromBars(bars []OHLCV) (float64, error) {
	if len(bars) == 0 {
		return 0, errors.New("no bars provided for VWAP calculation")
	}

	startIdx := 0
	if v.period > 0 && len(bars) > v.period {
		startIdx = len(bars) - v.period
	}

	sumTPVolume := 0.0
	sumVolume := 0.0

	for i := startIdx; i < len(bars); i++ {
		typicalPrice := (bars[i].High + bars[i].Low + bars[i].Close) / 3
		tpVolume := typicalPrice * bars[i].Volume
		sumTPVolume += tpVolume
		sumVolume += bars[i].Volume
	}

	if sumVolume == 0 {
		return 0, errors.New("total volume is zero, cannot compute VWAP")
	}

	return sumTPVolume / sumVolume, nil
}

// ComputeSeries returns VWAP values for each bar (cumulative from start)
func (v *VWAP) ComputeSeries(bars []OHLCV) ([]float64, error) {
	if len(bars) == 0 {
		return nil, errors.New("no bars provided for VWAP series calculation")
	}

	result := make([]float64, len(bars))
	sumTPVolume := 0.0
	sumVolume := 0.0

	for i, bar := range bars {
		typicalPrice := (bar.High + bar.Low + bar.Close) / 3
		sumTPVolume += typicalPrice * bar.Volume
		sumVolume += bar.Volume

		if sumVolume == 0 {
			result[i] = typicalPrice
		} else {
			result[i] = sumTPVolume / sumVolume
		}
	}

	return result, nil
}

// ComputeSeriesFromTrades returns VWAP values for each trade (cumulative)
func (v *VWAP) ComputeSeriesFromTrades(trades []Trade) ([]float64, error) {
	if len(trades) == 0 {
		return nil, errors.New("no trades provided for VWAP series calculation")
	}

	result := make([]float64, len(trades))
	sumPriceVolume := 0.0
	sumVolume := 0.0

	for i, trade := range trades {
		sumPriceVolume += trade.Price * trade.Amount
		sumVolume += trade.Amount

		if sumVolume == 0 {
			result[i] = trade.Price
		} else {
			result[i] = sumPriceVolume / sumVolume
		}
	}

	return result, nil
}

// VWAPBands represents VWAP with standard deviation bands
type VWAPBands struct {
	VWAP      float64
	Upper1Std float64
	Lower1Std float64
	Upper2Std float64
	Lower2Std float64
}

// ComputeWithBands calculates VWAP with standard deviation bands
func (v *VWAP) ComputeWithBands(bars []OHLCV) (*VWAPBands, error) {
	if len(bars) == 0 {
		return nil, errors.New("no bars provided for VWAP bands calculation")
	}

	startIdx := 0
	if v.period > 0 && len(bars) > v.period {
		startIdx = len(bars) - v.period
	}

	sumTPVolume := 0.0
	sumVolume := 0.0
	typicalPrices := make([]float64, 0, len(bars)-startIdx)

	for i := startIdx; i < len(bars); i++ {
		typicalPrice := (bars[i].High + bars[i].Low + bars[i].Close) / 3
		tpVolume := typicalPrice * bars[i].Volume
		sumTPVolume += tpVolume
		sumVolume += bars[i].Volume
		typicalPrices = append(typicalPrices, typicalPrice)
	}

	if sumVolume == 0 {
		return nil, errors.New("total volume is zero")
	}

	vwap := sumTPVolume / sumVolume

	sumSquaredDiff := 0.0
	for _, tp := range typicalPrices {
		diff := tp - vwap
		sumSquaredDiff += diff * diff
	}

	stdDev := 0.0
	if len(typicalPrices) > 0 {
		variance := sumSquaredDiff / float64(len(typicalPrices))
		if variance > 0 {
			stdDev = variance
			for i := 0; i < 10; i++ {
				stdDev = (stdDev + variance/stdDev) / 2
			}
		}
	}

	return &VWAPBands{
		VWAP:      vwap,
		Upper1Std: vwap + stdDev,
		Lower1Std: vwap - stdDev,
		Upper2Std: vwap + 2*stdDev,
		Lower2Std: vwap - 2*stdDev,
	}, nil
}

// IsPriceAboveVWAP returns true if the price is above VWAP
func (v *VWAP) IsPriceAboveVWAP(price, vwap float64) bool {
	return price > vwap
}

// IsPriceBelowVWAP returns true if the price is below VWAP
func (v *VWAP) IsPriceBelowVWAP(price, vwap float64) bool {
	return price < vwap
}
