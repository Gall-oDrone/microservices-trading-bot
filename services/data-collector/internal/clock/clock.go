package clock

import "time"

// Clock abstracts time for testability.
type Clock interface {
	Now() time.Time
}

// RealClock uses the system clock.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// FakeClock is a controllable clock for tests.
type FakeClock struct {
	T time.Time
}

func (c *FakeClock) Now() time.Time { return c.T.UTC() }

func (c *FakeClock) Advance(d time.Duration) {
	c.T = c.T.Add(d)
}
