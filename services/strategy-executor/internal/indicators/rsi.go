package indicators

import (
	"errors"
)

// RSI implements Relative Strength Index indicator
type RSI struct {
	period int
}

// NewRSI creates a new RSI indicator with the given period (typically 14)
func NewRSI(period int) *RSI {
	return &RSI{period: period}
}

func (r *RSI) Name() string {
	return "rsi"
}

func (r *RSI) Period() int {
	return r.period
}

// Compute calculates RSI from a slice of prices (most recent last)
// RSI = 100 - (100 / (1 + RS))
// where RS = Average Gain / Average Loss over the period
func (r *RSI) Compute(prices []float64) (float64, error) {
	if len(prices) < r.period+1 {
		return 0, errors.New("insufficient data for RSI calculation (need period + 1 prices)")
	}

	avgGain, avgLoss := r.computeAvgGainLoss(prices)
	
	if avgLoss == 0 {
		if avgGain == 0 {
			return 50, nil
		}
		return 100, nil
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi, nil
}

// computeAvgGainLoss calculates average gain and loss using Wilder's smoothing method
func (r *RSI) computeAvgGainLoss(prices []float64) (avgGain, avgLoss float64) {
	var gains, losses []float64
	for i := 1; i < len(prices); i++ {
		change := prices[i] - prices[i-1]
		if change > 0 {
			gains = append(gains, change)
			losses = append(losses, 0)
		} else {
			gains = append(gains, 0)
			losses = append(losses, -change)
		}
	}

	if len(gains) < r.period {
		return 0, 0
	}

	sumGain := 0.0
	sumLoss := 0.0
	for i := 0; i < r.period; i++ {
		sumGain += gains[i]
		sumLoss += losses[i]
	}

	avgGain = sumGain / float64(r.period)
	avgLoss = sumLoss / float64(r.period)

	for i := r.period; i < len(gains); i++ {
		avgGain = (avgGain*float64(r.period-1) + gains[i]) / float64(r.period)
		avgLoss = (avgLoss*float64(r.period-1) + losses[i]) / float64(r.period)
	}

	return avgGain, avgLoss
}

// ComputeFromTrades calculates RSI from trade data
func (r *RSI) ComputeFromTrades(trades []Trade) (float64, error) {
	if len(trades) < r.period+1 {
		return 0, errors.New("insufficient trades for RSI calculation")
	}

	prices := make([]float64, len(trades))
	for i, t := range trades {
		prices[i] = t.Price
	}

	return r.Compute(prices)
}

// ComputeFromBars calculates RSI from OHLCV bars using close prices
func (r *RSI) ComputeFromBars(bars []OHLCV) (float64, error) {
	if len(bars) < r.period+1 {
		return 0, errors.New("insufficient bars for RSI calculation")
	}

	prices := make([]float64, len(bars))
	for i, bar := range bars {
		prices[i] = bar.Close
	}

	return r.Compute(prices)
}

// ComputeSeries returns RSI values for all possible windows
func (r *RSI) ComputeSeries(prices []float64) ([]float64, error) {
	if len(prices) < r.period+1 {
		return nil, errors.New("insufficient data for RSI series calculation")
	}

	changes := make([]float64, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		changes[i-1] = prices[i] - prices[i-1]
	}

	result := make([]float64, len(prices)-r.period)

	sumGain := 0.0
	sumLoss := 0.0
	for i := 0; i < r.period; i++ {
		if changes[i] > 0 {
			sumGain += changes[i]
		} else {
			sumLoss -= changes[i]
		}
	}

	avgGain := sumGain / float64(r.period)
	avgLoss := sumLoss / float64(r.period)

	if avgLoss == 0 {
		if avgGain == 0 {
			result[0] = 50
		} else {
			result[0] = 100
		}
	} else {
		rs := avgGain / avgLoss
		result[0] = 100 - (100 / (1 + rs))
	}

	for i := r.period; i < len(changes); i++ {
		if changes[i] > 0 {
			avgGain = (avgGain*float64(r.period-1) + changes[i]) / float64(r.period)
			avgLoss = (avgLoss * float64(r.period-1)) / float64(r.period)
		} else {
			avgGain = (avgGain * float64(r.period-1)) / float64(r.period)
			avgLoss = (avgLoss*float64(r.period-1) - changes[i]) / float64(r.period)
		}

		if avgLoss == 0 {
			if avgGain == 0 {
				result[i-r.period+1] = 50
			} else {
				result[i-r.period+1] = 100
			}
		} else {
			rs := avgGain / avgLoss
			result[i-r.period+1] = 100 - (100 / (1 + rs))
		}
	}

	return result, nil
}

// IsOverbought returns true if RSI is above the threshold (typically 70)
func (r *RSI) IsOverbought(rsi, threshold float64) bool {
	return rsi > threshold
}

// IsOversold returns true if RSI is below the threshold (typically 30)
func (r *RSI) IsOversold(rsi, threshold float64) bool {
	return rsi < threshold
}
