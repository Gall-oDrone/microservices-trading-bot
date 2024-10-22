package timeseries

import "sync"

type ConcurrentMovingAverage struct {
	ma  *MovingAverage
	mux sync.RWMutex
}

// NewConcurrentMovingAverage creates a new ConcurrentMovingAverage.
func NewConcurrentMovingAverage(window int) *ConcurrentMovingAverage {
	return &ConcurrentMovingAverage{
		ma: New(window),
	}
}

// Add adds new values to the moving average.
func (c *ConcurrentMovingAverage) Add(values ...float64) {
	c.mux.Lock()
	defer c.mux.Unlock()
	c.ma.Add(values...)
}

// Avg calculates the average of the values.
func (c *ConcurrentMovingAverage) Avg() float64 {
	c.mux.RLock()
	defer c.mux.RUnlock()
	return c.ma.Avg()
}

// Min finds the minimum value in the moving average.
func (c *ConcurrentMovingAverage) Min() (float64, error) {
	c.mux.RLock()
	defer c.mux.RUnlock()
	return c.ma.Min()
}

// Max finds the maximum value in the moving average.
func (c *ConcurrentMovingAverage) Max() (float64, error) {
	c.mux.RLock()
	defer c.mux.RUnlock()
	return c.ma.Max()
}

// Count returns the number of values in the moving average.
func (c *ConcurrentMovingAverage) Count() int {
	c.mux.RLock()
	defer c.mux.RUnlock()
	return c.ma.Count()
}

// Values returns all the values in the moving average.
func (c *ConcurrentMovingAverage) Values() []float64 {
	c.mux.RLock()
	defer c.mux.RUnlock()
	return c.ma.Values()
}

// SlotsFilled checks if all slots in the moving average are filled.
func (c *ConcurrentMovingAverage) SlotsFilled() bool {
	c.mux.RLock()
	defer c.mux.RUnlock()
	return c.ma.SlotsFilled()
}
