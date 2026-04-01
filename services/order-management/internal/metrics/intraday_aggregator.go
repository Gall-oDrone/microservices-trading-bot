package metrics

import (
	"sync"
	"time"

	"github.com/shopspring/decimal"

	sharedMetrics "bitso-trading-platform/shared/pkg/metrics"
)

// IntradayAggregator maintains session-scoped P&L and trade counts with decimal precision,
// and writes to Prometheus via IntradayMetricsWriter. Thread-safe and production-ready.
// Implements sharedMetrics.PnLRecorder and sharedMetrics.IntradayRecorder.
type IntradayAggregator struct {
	writer IntradayMetricsWriter
	session sharedMetrics.SessionProvider

	mu sync.RWMutex

	// Session boundary: reset when date changes
	lastSessionDate time.Time

	// Per-currency state (decimal for accurate accumulation)
	dailyRealizedPnL   map[string]decimal.Decimal
	dailyUnrealizedPnL map[string]decimal.Decimal
	peakEquity         map[string]decimal.Decimal
	currentEquity      map[string]decimal.Decimal

	// Per book+strategy counts for current session
	tradesToday   map[string]int64 // key: book|strategy
	winsToday     map[string]int64
	lossesToday   map[string]int64
	breakevensToday map[string]int64 // realized P&L == 0 (still a closed trade)
}

// NewIntradayAggregator creates an aggregator that writes to the given writer
// and uses the given session provider for date boundaries. Both must be non-nil.
func NewIntradayAggregator(writer IntradayMetricsWriter, session sharedMetrics.SessionProvider) *IntradayAggregator {
	if writer == nil {
		panic("metrics: IntradayMetricsWriter is required")
	}
	if session == nil {
		session = sharedMetrics.UTCSessionProvider{}
	}
	return &IntradayAggregator{
		writer:             writer,
		session:            session,
		lastSessionDate:     time.Time{},
		dailyRealizedPnL:    make(map[string]decimal.Decimal),
		dailyUnrealizedPnL:  make(map[string]decimal.Decimal),
		peakEquity:          make(map[string]decimal.Decimal),
		currentEquity:       make(map[string]decimal.Decimal),
		tradesToday:       make(map[string]int64),
		winsToday:         make(map[string]int64),
		lossesToday:       make(map[string]int64),
		breakevensToday:   make(map[string]int64),
	}
}

// Ensure IntradayAggregator implements sharedMetrics.PnLRecorder and sharedMetrics.IntradayRecorder.
var (
	_ sharedMetrics.PnLRecorder     = (*IntradayAggregator)(nil)
	_ sharedMetrics.IntradayRecorder = (*IntradayAggregator)(nil)
)

func (a *IntradayAggregator) maybeResetSession() {
	now := a.session.SessionDate()
	date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastSessionDate.IsZero() || a.lastSessionDate.Before(date) {
		a.lastSessionDate = date
		a.dailyRealizedPnL = make(map[string]decimal.Decimal)
		a.dailyUnrealizedPnL = make(map[string]decimal.Decimal)
		a.peakEquity = make(map[string]decimal.Decimal)
		a.currentEquity = make(map[string]decimal.Decimal)
		a.tradesToday = make(map[string]int64)
		a.winsToday = make(map[string]int64)
		a.lossesToday = make(map[string]int64)
		a.breakevensToday = make(map[string]int64)
	}
}

func key(book, strategy string) string {
	return book + "|" + strategy
}

// RecordDailyRealizedPnL adds to the current session's realized P&L for the currency.
func (a *IntradayAggregator) RecordDailyRealizedPnL(currency string, amount decimal.Decimal) {
	a.maybeResetSession()
	a.mu.Lock()
	prev := a.dailyRealizedPnL[currency]
	a.dailyRealizedPnL[currency] = prev.Add(amount)
	cur := a.dailyRealizedPnL[currency]
	a.mu.Unlock()
	f, _ := cur.Float64()
	a.writer.SetDailyRealizedPnL(currency, f)
}

// RecordDailyUnrealizedPnL sets the current session's unrealized P&L (replaces).
func (a *IntradayAggregator) RecordDailyUnrealizedPnL(currency string, amount decimal.Decimal) {
	a.maybeResetSession()
	a.mu.Lock()
	a.dailyUnrealizedPnL[currency] = amount
	a.mu.Unlock()
	f, _ := amount.Float64()
	a.writer.SetDailyUnrealizedPnL(currency, f)
}

// RecordTradeClosed records a closed trade and updates counts and realized P&L.
func (a *IntradayAggregator) RecordTradeClosed(outcome sharedMetrics.TradeOutcome) {
	a.maybeResetSession()
	a.mu.Lock()
	k := key(outcome.Book, outcome.Strategy)
	a.tradesToday[k]++
	switch {
	case outcome.IsBreakeven:
		a.breakevensToday[k]++
	case outcome.IsWin:
		a.winsToday[k]++
	default:
		a.lossesToday[k]++
	}
	prev := a.dailyRealizedPnL[outcome.Currency]
	a.dailyRealizedPnL[outcome.Currency] = prev.Add(outcome.RealizedPnL.Amount())
	curRealized := a.dailyRealizedPnL[outcome.Currency]
	trades := a.tradesToday[k]
	wins := a.winsToday[k]
	losses := a.lossesToday[k]
	a.mu.Unlock()

	fRealized, _ := curRealized.Float64()
	a.writer.SetDailyRealizedPnL(outcome.Currency, fRealized)
	a.writer.SetTradesToday(outcome.Book, outcome.Strategy, float64(trades))
	a.writer.SetWinsToday(outcome.Book, outcome.Strategy, float64(wins))
	a.writer.SetLossesToday(outcome.Book, outcome.Strategy, float64(losses))
}

// RecordEquityUpdate updates current and peak equity and recomputes drawdown.
func (a *IntradayAggregator) RecordEquityUpdate(currency string, currentEquity decimal.Decimal) {
	a.maybeResetSession()
	a.mu.Lock()
	a.currentEquity[currency] = currentEquity
	peak := a.peakEquity[currency]
	if peak.IsZero() || currentEquity.GreaterThan(peak) {
		a.peakEquity[currency] = currentEquity
		peak = currentEquity
	}
	a.mu.Unlock()

	// Drawdown: (peak - current) / peak * 100 when peak > 0
	var percent float64
	var absolute decimal.Decimal
	if peak.IsZero() || !peak.GreaterThan(decimal.Zero) {
		percent = 0
		absolute = decimal.Zero
	} else {
		diff := peak.Sub(currentEquity)
		absolute = diff
		if diff.GreaterThan(decimal.Zero) {
			pct := diff.Div(peak).Mul(decimal.NewFromInt(100))
			percent, _ = pct.Float64()
		}
	}
	absF, _ := absolute.Float64()
	a.writer.SetCurrentEquity(currency, mustFloat64(currentEquity))
	a.writer.SetPeakEquity(currency, mustFloat64(peak))
	a.writer.SetDrawdownPercent(currency, percent)
	a.writer.SetDrawdownAbsolute(currency, absF)
}

func mustFloat64(d decimal.Decimal) float64 {
	f, _ := d.Float64()
	return f
}

// SessionDate returns the date for which metrics are currently accumulated.
func (a *IntradayAggregator) SessionDate() time.Time {
	a.maybeResetSession()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastSessionDate
}

// TradesToday returns the number of trades closed in the current session for book/strategy.
func (a *IntradayAggregator) TradesToday(book, strategy string) int64 {
	a.maybeResetSession()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.tradesToday[key(book, strategy)]
}

// WinsToday returns the number of winning trades in the current session.
func (a *IntradayAggregator) WinsToday(book, strategy string) int64 {
	a.maybeResetSession()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.winsToday[key(book, strategy)]
}

// LossesToday returns the number of losing trades in the current session.
func (a *IntradayAggregator) LossesToday(book, strategy string) int64 {
	a.maybeResetSession()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lossesToday[key(book, strategy)]
}

// SessionSnapshot returns current session risk metrics for use by trading-engine (daily loss / drawdown limits).
// Sums realized P&L across currencies; returns max drawdown % across currencies.
func (a *IntradayAggregator) SessionSnapshot() (dailyRealizedPnL, drawdownPct float64) {
	a.maybeResetSession()
	a.mu.RLock()
	defer a.mu.RUnlock()
	var totalRealized decimal.Decimal
	for _, v := range a.dailyRealizedPnL {
		totalRealized = totalRealized.Add(v)
	}
	maxDrawdownPct := float64(0)
	for c := range a.currentEquity {
		peak := a.peakEquity[c]
		cur := a.currentEquity[c]
		if peak.IsZero() || !peak.GreaterThan(decimal.Zero) {
			continue
		}
		diff := peak.Sub(cur)
		if diff.GreaterThan(decimal.Zero) {
			pct := diff.Div(peak).Mul(decimal.NewFromInt(100))
			p, _ := pct.Float64()
			if p > maxDrawdownPct {
				maxDrawdownPct = p
			}
		}
	}
	r, _ := totalRealized.Float64()
	return r, maxDrawdownPct
}
