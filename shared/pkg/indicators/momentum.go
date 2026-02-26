package indicators

// Momentum returns price momentum: (current - past) / past, or 0 if past is 0.
// prices: newest last. lookback = number of periods back for "past".
func Momentum(prices []float64, lookback int) float64 {
	if lookback <= 0 || len(prices) <= lookback {
		return 0
	}
	current := prices[len(prices)-1]
	past := prices[len(prices)-1-lookback]
	if past == 0 {
		return 0
	}
	return (current - past) / past
}

// Momentum1m expects prices sampled at ~1min; lookback 1 = 1min momentum.
// Caller should pass the appropriate slice (e.g. last 2 points for 1min if 1min bars).
const Momentum1mLookback = 1

// Momentum5mLookback: 5 periods for 5min (if 1min bars).
const Momentum5mLookback = 5

// Momentum15mLookback: 15 periods for 15min (if 1min bars).
const Momentum15mLookback = 15

// MomentumMulti returns momentum for 1, 5, 15 period lookbacks (e.g. 1m, 5m, 15m if bars are 1min).
func MomentumMulti(prices []float64) (m1, m5, m15 float64) {
	m1 = Momentum(prices, Momentum1mLookback)
	m5 = Momentum(prices, Momentum5mLookback)
	m15 = Momentum(prices, Momentum15mLookback)
	return m1, m5, m15
}
