package strategies

import "time"

// Clock supplies "now" to a strategy.
//
// Strategies throttle signals, age out pending orders, and time-box positions
// by comparing timestamps against the current time. In production that is the
// wall clock. In a backtest it must be the *simulated* market time of the bar
// being replayed, because a 34-day replay finishes in a few minutes of real
// time: with a wall clock, a 60-second signal throttle silently collapses the
// entire run to a handful of signals, and the result becomes a measurement of
// how fast the machine is rather than of how the strategy behaves.
//
// The zero value is not usable; call SetClock or rely on the wall-clock
// default provided by BaseEnhancedStrategy.now.
type Clock func() time.Time

// ClockAware is implemented by strategies whose notion of time can be
// redirected. The backtest runner uses this to advance strategies along
// simulated market time.
type ClockAware interface {
	SetClock(Clock)
}

// SetClock redirects this strategy's notion of "now".
//
// Passing nil restores the wall clock, so a caller can safely reset without
// needing to know the previous value.
func (s *BaseEnhancedStrategy) SetClock(c Clock) {
	s.clock = c
}

// now returns the current time according to this strategy's clock.
//
// It defaults to time.Now so that every existing production code path and test
// keeps its exact previous behaviour without being touched.
func (s *BaseEnhancedStrategy) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

// Now is the exported form of now, for strategies defined outside this package
// (for example the backtest-only baselines) that need the same notion of time
// as the built-in strategies.
func (s *BaseEnhancedStrategy) Now() time.Time {
	return s.now()
}
