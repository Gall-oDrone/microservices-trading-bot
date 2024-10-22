package timeseries

import (
	"errors"
	"math"
)

// MovingAverage implements a circular buffer for calculating moving averages
type MovingAverage struct {
	Window          int
	values          []float64
	valPos          int
	slotsFilled     bool
	ignoreNanValues bool
	ignoreInfValues bool
}

var errNoValues = errors.New("no values")

// SetIgnoreInfValues sets whether to ignore infinite values
func (ma *MovingAverage) SetIgnoreInfValues(ignoreInfValues bool) {
	ma.ignoreInfValues = ignoreInfValues
}

// SetIgnoreNanValues sets whether to ignore NaN values
func (ma *MovingAverage) SetIgnoreNanValues(ignoreNanValues bool) {
	ma.ignoreNanValues = ignoreNanValues
}

// Avg returns the average of the filled values
func (ma *MovingAverage) Avg() float64 {
	values := ma.filledValues()
	if values == nil {
		return 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

// filledValues returns the slice of filled values
func (ma *MovingAverage) filledValues() []float64 {
	if !ma.slotsFilled {
		if ma.valPos == 0 {
			return nil
		}
		return ma.values[:ma.valPos]
	}
	return ma.values
}

// Add adds new values to the moving average
func (ma *MovingAverage) Add(values ...float64) {
	for _, val := range values {
		if (ma.ignoreNanValues && math.IsNaN(val)) || (ma.ignoreInfValues && math.IsInf(val, 0)) {
			continue
		}
		ma.values[ma.valPos] = val
		ma.valPos = (ma.valPos + 1) % ma.Window
		if !ma.slotsFilled && ma.valPos == 0 {
			ma.slotsFilled = true
		}
	}
}

// SlotsFilled returns whether all slots are filled
func (ma *MovingAverage) SlotsFilled() bool {
	return ma.slotsFilled
}

// Values returns the current filled values
func (ma *MovingAverage) Values() []float64 {
	return ma.filledValues()
}

// Count returns the number of filled values
func (ma *MovingAverage) Count() int {
	return len(ma.filledValues())
}

// Max returns the maximum value of the filled values
func (ma *MovingAverage) Max() (float64, error) {
	values := ma.filledValues()
	if values == nil {
		return 0, errNoValues
	}
	maxVal := values[0]
	for _, value := range values {
		if value > maxVal {
			maxVal = value
		}
	}
	return maxVal, nil
}

// Min returns the minimum value of the filled values
func (ma *MovingAverage) Min() (float64, error) {
	values := ma.filledValues()
	if values == nil {
		return 0, errNoValues
	}
	minVal := values[0]
	for _, value := range values {
		if value < minVal {
			minVal = value
		}
	}
	return minVal, nil
}

// New creates a new MovingAverage with a specified window size
func New(window int) *MovingAverage {
	if window <= 0 {
		panic("window size must be greater than 0")
	}
	return &MovingAverage{
		Window:      window,
		values:      make([]float64, window),
		valPos:      0,
		slotsFilled: false,
	}
}
