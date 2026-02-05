package metrics

import (
	"time"

	"github.com/shopspring/decimal"
)

// SessionProvider provides the current session date for intraday metrics.
// Allows tests and different timezones to control "today" boundaries.
// Implementations must be safe for concurrent use.
type SessionProvider interface {
	// SessionDate returns the current session date (typically UTC calendar date).
	SessionDate() time.Time
}

// UTCSessionProvider uses UTC for session date (production default).
type UTCSessionProvider struct{}

// SessionDate returns today's date in UTC.
func (UTCSessionProvider) SessionDate() time.Time {
	return time.Now().UTC()
}

// PnLRecorder records profit/loss and related financial metrics for intraday and risk.
// Implementations must be thread-safe and suitable for production.
// All monetary values use decimal internally; exporters may convert to float for Prometheus.
type PnLRecorder interface {
	// RecordDailyRealizedPnL adds to the current session's realized P&L.
	RecordDailyRealizedPnL(currency string, amount decimal.Decimal)

	// RecordDailyUnrealizedPnL sets the current session's unrealized P&L (replaces, not accumulates).
	RecordDailyUnrealizedPnL(currency string, amount decimal.Decimal)

	// RecordTradeClosed records a closed trade for daily counts and win/loss.
	RecordTradeClosed(outcome TradeOutcome)

	// RecordEquityUpdate updates current and peak equity for drawdown calculation.
	// Current equity = start-of-day equity + daily realized P&L + unrealized P&L.
	// Implementations compute drawdown from peak vs current and expose it (e.g. Prometheus).
	RecordEquityUpdate(currency string, currentEquity decimal.Decimal)
}

// IntradayRecorder extends PnLRecorder with session reset and query support.
// Aggregators implement this; callers record via PnLRecorder.
type IntradayRecorder interface {
	PnLRecorder

	// SessionDate returns the date for which metrics are currently accumulated.
	SessionDate() time.Time

	// TradesToday returns the number of trades closed in the current session.
	TradesToday(book, strategy string) int64

	// WinsToday returns the number of winning trades in the current session.
	WinsToday(book, strategy string) int64

	// LossesToday returns the number of losing trades in the current session.
	LossesToday(book, strategy string) int64
}
