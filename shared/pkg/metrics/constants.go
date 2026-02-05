package metrics

// Prometheus metric names and label names for trading/financial metrics.
// Single source of truth for production dashboards and alerting.
const (
	// Metric names (Prometheus)
	NameDailyRealizedPnL   = "trading_daily_realized_pnl_currency"
	NameDailyUnrealizedPnL = "trading_daily_unrealized_pnl_currency"
	NameDrawdownPercent    = "trading_drawdown_percent"
	NameDrawdownAbsolute   = "trading_drawdown_absolute_currency"
	NamePeakEquity         = "trading_peak_equity_currency"
	NameCurrentEquity      = "trading_current_equity_currency"
	NameTradesToday        = "trading_trades_today_total"
	NameWinsToday          = "trading_wins_today_total"
	NameLossesToday        = "trading_losses_today_total"

	// Label names
	LabelCurrency = "currency"
	LabelBook     = "book"
	LabelStrategy = "strategy"
)
