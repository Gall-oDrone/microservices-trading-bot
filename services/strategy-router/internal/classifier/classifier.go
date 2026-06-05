// Package classifier maps an indicator snapshot to a market-regime label.
//
// Inputs come from strategy-executor's GET /api/v1/indicators/{book}/snapshot
// response. Outputs are one of:
//   - "low_vol_range"
//   - "trending_up"
//   - "trending_down"
//   - "high_vol"
//   - "neutral"
//
// The thresholds are passed in by the caller so production and tests share
// the same code path. Defaults are populated by services/strategy-router
// /internal/config.Load() (which mirrors scripts/strategy-regime-router.sh).
package classifier

// Snapshot mirrors the relevant subset of the indicator-service Snapshot
// JSON. Each pointer field may be nil when the indicator hasn't warmed
// up yet; the classifier treats missing inputs as "neutral".
type Snapshot struct {
	Book        string
	Price       float64
	ATR         float64
	EMA         float64
	RSI         float64
	BBUpper     float64
	BBMiddle    float64
	BBLower     float64
	DataHealthy bool
	StaleReason string
}

// Thresholds bundles the tunable knobs for the classifier so callers can
// inject the values from config.Config (or a test fixture) without depending
// on the config package.
type Thresholds struct {
	ATRHighVolPct float64
	ATRLowVolPct  float64
	RSIOverbought float64
	RSIOversold   float64
	BBUpper       float64
	BBLower       float64
	EMADistEntry  float64 // percent EMA distance considered "trending"
}

// Decision is the structured output of a classification run.
type Decision struct {
	Regime      string  // one of the regime labels above
	ATRPct      float64 // ATR as percent of price
	EMADistPct  float64 // (price - ema) / price * 100
	BollingerPB float64 // %B
	RSI         float64
	Price       float64
	Reason      string // human-readable explanation
}

// Classify returns a Decision for the given snapshot + thresholds.
//
// The order of checks intentionally matches the bash router:
//  1. high_vol — pause if ATR% is excessive
//  2. low_vol_range — calm market with price in the middle of the band
//  3. trending_up / trending_down — EMA distance + RSI gating
//  4. neutral — default
func Classify(s Snapshot, th Thresholds) Decision {
	d := Decision{
		Price: s.Price,
		RSI:   s.RSI,
	}

	if s.Price <= 0 {
		d.Regime = "neutral"
		d.Reason = "missing price"
		return d
	}

	// ATR as percent of price.
	d.ATRPct = (s.ATR / s.Price) * 100

	// Bollinger %B = (price - lower) / (upper - lower).
	if s.BBUpper > s.BBLower {
		d.BollingerPB = (s.Price - s.BBLower) / (s.BBUpper - s.BBLower)
	} else {
		d.BollingerPB = 0.5
	}

	// EMA distance: price vs EMA as percent of price.
	if s.EMA > 0 {
		d.EMADistPct = ((s.Price - s.EMA) / s.Price) * 100
	}

	// 1) High volatility — short-circuit: never trade here.
	if d.ATRPct > th.ATRHighVolPct {
		d.Regime = "high_vol"
		d.Reason = "ATR% above high-vol threshold"
		return d
	}

	// 2) Low-volatility range — tight ATR + price inside the band.
	if d.ATRPct < th.ATRLowVolPct &&
		d.BollingerPB > th.BBLower &&
		d.BollingerPB < th.BBUpper {
		d.Regime = "low_vol_range"
		d.Reason = "ATR% below low-vol threshold and %B within band"
		return d
	}

	// 3) Trending up — price above EMA, RSI not exhausted.
	if d.EMADistPct > th.EMADistEntry && s.RSI < th.RSIOverbought && s.RSI > 0 {
		d.Regime = "trending_up"
		d.Reason = "EMA distance positive and RSI below overbought"
		return d
	}

	// 4) Trending down — price below EMA, RSI not exhausted.
	if d.EMADistPct < -th.EMADistEntry && s.RSI > th.RSIOversold {
		d.Regime = "trending_down"
		d.Reason = "EMA distance negative and RSI above oversold"
		return d
	}

	d.Regime = "neutral"
	d.Reason = "no regime predicate matched"
	return d
}
