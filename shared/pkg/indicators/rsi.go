package indicators

// RSI computes the Relative Strength Index (0-100) using simple average of gains/losses.
// prices: closing prices, newest last. period: typically 14.
// Returns 50.0 (neutral) if not enough data.
func RSI(prices []float64, period int) float64 {
	if period <= 0 || len(prices) < period+1 {
		return 50.0
	}
	n := period + 1
	start := len(prices) - n
	if start < 0 {
		start = 0
	}
	p := prices[start:]
	if len(p) < n {
		return 50.0
	}
	gains, losses := 0.0, 0.0
	for i := 1; i < len(p); i++ {
		ch := p[i] - p[i-1]
		if ch > 0 {
			gains += ch
		} else {
			losses += -ch
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)
	if avgLoss == 0 {
		return 100.0
	}
	rs := avgGain / avgLoss
	return 100.0 - (100.0 / (1.0 + rs))
}

// RSIState indicates overbought/oversold/neutral for a given RSI value.
type RSIState int

const (
	RSINeutral RSIState = iota
	RSIOversold
	RSIOverbought
)

// RSIStateFromLevels returns the signal state given RSI and thresholds.
func RSIStateFromLevels(rsi, oversold, overbought float64) RSIState {
	if rsi < oversold {
		return RSIOversold
	}
	if rsi > overbought {
		return RSIOverbought
	}
	return RSINeutral
}

// DefaultRSIOversold is the typical oversold threshold (e.g. 30).
const DefaultRSIOversold = 30.0

// DefaultRSIOverbought is the typical overbought threshold (e.g. 70).
const DefaultRSIOverbought = 70.0
