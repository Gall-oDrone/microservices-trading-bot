package indicators

import (
	"errors"
)

// EMA implements Exponential Moving Average indicator
type EMA struct {
	period int
	alpha  float64
}

// NewEMA creates a new EMA indicator with the given period
func NewEMA(period int) *EMA {
	alpha := 2.0 / float64(period+1)
	return &EMA{period: period, alpha: alpha}
}

func (e *EMA) Name() string {
	return "ema"
}

func (e *EMA) Period() int {
	return e.period
}

// Compute calculates EMA from a slice of prices (most recent last)
func (e *EMA) Compute(prices []float64) (float64, error) {
	if len(prices) < e.period {
		return 0, errors.New("insufficient data for EMA calculation")
	}

	sma := NewSMA(e.period)
	initialSMA, err := sma.Compute(prices[:e.period])
	if err != nil {
		return 0, err
	}

	ema := initialSMA
	for i := e.period; i < len(prices); i++ {
		ema = e.alpha*prices[i] + (1-e.alpha)*ema
	}

	return ema, nil
}

// ComputeFromTrades calculates EMA from trade data
func (e *EMA) ComputeFromTrades(trades []Trade) (float64, error) {
	if len(trades) < e.period {
		return 0, errors.New("insufficient trades for EMA calculation")
	}

	prices := make([]float64, len(trades))
	for i, t := range trades {
		prices[i] = t.Price
	}

	return e.Compute(prices)
}

// ComputeFromBars calculates EMA from OHLCV bars using close prices
func (e *EMA) ComputeFromBars(bars []OHLCV) (float64, error) {
	if len(bars) < e.period {
		return 0, errors.New("insufficient bars for EMA calculation")
	}

	prices := make([]float64, len(bars))
	for i, bar := range bars {
		prices[i] = bar.Close
	}

	return e.Compute(prices)
}

// ComputeSeries returns EMA values for all possible windows in the price series
func (e *EMA) ComputeSeries(prices []float64) ([]float64, error) {
	if len(prices) < e.period {
		return nil, errors.New("insufficient data for EMA series calculation")
	}

	result := make([]float64, len(prices)-e.period+1)
	
	sma := NewSMA(e.period)
	initialSMA, err := sma.Compute(prices[:e.period])
	if err != nil {
		return nil, err
	}

	ema := initialSMA
	result[0] = ema

	for i := e.period; i < len(prices); i++ {
		ema = e.alpha*prices[i] + (1-e.alpha)*ema
		result[i-e.period+1] = ema
	}

	return result, nil
}

// ComputeWithPrevious calculates EMA incrementally given previous EMA and new price
func (e *EMA) ComputeWithPrevious(prevEMA, newPrice float64) float64 {
	return e.alpha*newPrice + (1-e.alpha)*prevEMA
}
