package etoro

import (
	"context"
	"sync"
	"time"
)

// limiter is a token bucket refilled continuously at perMinute/60 tokens a
// second with a burst of perMinute. Tokens may go negative: a caller that
// finds the bucket empty reserves the next token and sleeps until it is due,
// so concurrent callers queue in order instead of stampeding.
type limiter struct {
	mu     sync.Mutex
	rate   float64 // tokens per second
	burst  float64
	tokens float64
	last   time.Time
	now    func() time.Time
}

func newLimiter(perMinute int, now func() time.Time) *limiter {
	b := float64(perMinute)
	return &limiter{rate: b / 60, burst: b, tokens: b, last: now(), now: now}
}

// reserve takes one token and returns how long to wait before using it.
func (l *limiter) reserve() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	if el := t.Sub(l.last).Seconds(); el > 0 {
		l.tokens += el * l.rate
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
	}
	l.last = t
	l.tokens--
	if l.tokens >= 0 {
		return 0
	}
	return time.Duration(-l.tokens / l.rate * float64(time.Second))
}

func (l *limiter) wait(ctx context.Context, sleep func(context.Context, time.Duration) error) error {
	if d := l.reserve(); d > 0 {
		return sleep(ctx, d)
	}
	return ctx.Err()
}
