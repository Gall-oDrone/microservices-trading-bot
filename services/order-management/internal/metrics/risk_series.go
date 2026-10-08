package metrics

import (
	"math"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Execution-quality and portfolio-risk series (plan §6.4.7): what a trading
// desk's risk function reports beyond limits — realized slippage of every
// executed order against the price it was decided at (implementation
// shortfall), exposure per book marked to market, and a 1-day 99 % parametric
// VaR per quote currency against its limit.
//
// RiskSeries registers on the Registerer it is given (prometheus.Default-
// Registerer in main, a fresh registry in tests), so it is independent of
// MetricsCollector's promauto series. All labels are bounded: book, side,
// direction, currency, asset.

// Slippage directions for order_slippage_cost_quote_total.
const (
	SlippageAdverse     = "adverse"     // paid more (buy) / received less (sell) than the decision price
	SlippageImprovement = "improvement" // the opposite
)

// VaRConfidence and VaRHorizon describe portfolio_var_quote; fixed so the
// series never changes meaning silently.
const (
	VaRConfidence = 0.99
	VaRZ          = 2.3263478740408408 // one-sided normal quantile at 99 %
	VaRHorizon    = "1d"
)

// RiskSeries holds the execution-quality and portfolio-risk series.
type RiskSeries struct {
	slippageBps    *prometheus.HistogramVec
	slippageCost   *prometheus.CounterVec
	filledNotional *prometheus.CounterVec
	exposureBase   *prometheus.GaugeVec
	exposureQuote  *prometheus.GaugeVec
	markPrice      *prometheus.GaugeVec
	markFallback   *prometheus.GaugeVec
	dailyVol       *prometheus.GaugeVec
	netBase        *prometheus.GaugeVec
	grossQuote     *prometheus.GaugeVec
	netQuote       *prometheus.GaugeVec
	varQuote       *prometheus.GaugeVec
	varLimit       *prometheus.GaugeVec
	lastRun        prometheus.Gauge
	runErrors      prometheus.Counter
}

// NewRiskSeries creates and registers the series on reg.
func NewRiskSeries(reg prometheus.Registerer) *RiskSeries {
	m := &RiskSeries{
		slippageBps: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "order_arrival_slippage_bps",
			Help:    "Per executed order: average fill price vs the order's decision price in bps, signed so positive is a cost (buy above / sell below). Observed once, when the order closes with fills",
			Buckets: []float64{-200, -100, -50, -25, -10, -5, 0, 5, 10, 25, 50, 100, 200, 500},
		}, []string{"book", "side"}),
		slippageCost: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "order_slippage_cost_quote_total",
			Help: "Slippage vs decision price in quote currency, split by direction so both stay monotonic; net cost = adverse - improvement",
		}, []string{"book", "side", "direction"}),
		filledNotional: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "order_filled_notional_quote_total",
			Help: "Filled notional (quantity x average price) in quote currency of the orders observed in order_arrival_slippage_bps; denominator for notional-weighted slippage",
		}, []string{"book", "side"}),
		exposureBase: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "position_exposure_base",
			Help: "Signed position per book in base currency (long positive)",
		}, []string{"book"}),
		exposureQuote: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "position_exposure_quote",
			Help: "Signed position per book marked to market, in the book's quote currency",
		}, []string{"book", "currency"}),
		markPrice: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "position_mark_price",
			Help: "Price used to mark the book (market-data mid, else last trade, else the position's average entry price)",
		}, []string{"book"}),
		markFallback: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "position_mark_fallback",
			Help: "1 when an open book has no market price and is marked at its average entry price (or at 0 if that is unknown too); its exposure and VaR are then stale",
		}, []string{"book"}),
		dailyVol: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "risk_var_daily_vol_ratio",
			Help: "Daily return volatility assumed for the book in the VaR (RISK_VAR_DAILY_VOL*; a model parameter, not an estimate)",
		}, []string{"book"}),
		netBase: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "portfolio_net_exposure_base",
			Help: "Net position across books per base asset (e.g. btc over btc_mxn and btc_usd)",
		}, []string{"asset"}),
		grossQuote: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "portfolio_gross_exposure_quote",
			Help: "Sum of absolute marked exposures per quote currency",
		}, []string{"currency"}),
		netQuote: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "portfolio_net_exposure_quote",
			Help: "Sum of signed marked exposures per quote currency",
		}, []string{"currency"}),
		varQuote: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "portfolio_var_quote",
			Help: "1-day 99 % parametric VaR per quote currency: z x |sum(exposure x daily vol)| (books in one currency share the base asset, correlation 1)",
		}, []string{"currency"}),
		varLimit: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "portfolio_var_limit_quote",
			Help: "VaR limit per quote currency (RISK_VAR_LIMITS); absent when not set",
		}, []string{"currency"}),
		lastRun: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "portfolio_risk_last_run_timestamp_seconds",
			Help: "Unix time the portfolio exposure and VaR were last computed",
		}),
		runErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "portfolio_risk_run_errors_total",
			Help: "Portfolio risk runs that could not read positions",
		}),
	}
	reg.MustRegister(m.slippageBps, m.slippageCost, m.filledNotional, m.exposureBase, m.exposureQuote,
		m.markPrice, m.markFallback, m.dailyVol, m.netBase, m.grossQuote, m.netQuote, m.varQuote,
		m.varLimit, m.lastRun, m.runErrors)
	return m
}

// SlippageBps is the signed slippage of avgPrice against refPrice, positive
// when it is a cost: a buy above or a sell below the reference. ok is false
// when either price is unusable or the side is unknown.
func SlippageBps(side string, refPrice, avgPrice float64) (bps float64, ok bool) {
	if !(refPrice > 0) || !(avgPrice > 0) || math.IsInf(refPrice, 0) || math.IsInf(avgPrice, 0) {
		return 0, false
	}
	switch side {
	case "buy":
		return (avgPrice - refPrice) / refPrice * 1e4, true
	case "sell":
		return (refPrice - avgPrice) / refPrice * 1e4, true
	}
	return 0, false
}

// ObserveExecution records one closed order's slippage. qty is the filled
// base quantity. It is a no-op (false) when the prices are unusable.
func (m *RiskSeries) ObserveExecution(book, side string, qty, refPrice, avgPrice float64) bool {
	if m == nil || !(qty > 0) {
		return false
	}
	bps, ok := SlippageBps(side, refPrice, avgPrice)
	if !ok {
		return false
	}
	notional := qty * avgPrice
	cost := bps / 1e4 * notional
	m.slippageBps.WithLabelValues(book, side).Observe(bps)
	m.filledNotional.WithLabelValues(book, side).Add(notional)
	// Touch both directions so a rate over either never reads "no data".
	adv := m.slippageCost.WithLabelValues(book, side, SlippageAdverse)
	imp := m.slippageCost.WithLabelValues(book, side, SlippageImprovement)
	if cost >= 0 {
		adv.Add(cost)
	} else {
		imp.Add(-cost)
	}
	return true
}

// BookExposure is one book's marked position.
type BookExposure struct {
	Book, Asset, Currency string
	Base, Mark, Quote     float64
	DailyVol              float64
	Fallback              bool
}

// CurrencyRisk is the aggregate for one quote currency.
type CurrencyRisk struct {
	Gross, Net, VaR float64
	Limit           float64 // 0: none
}

// SetPortfolio publishes one portfolio run.
func (m *RiskSeries) SetPortfolio(books []BookExposure, netBase map[string]float64, ccy map[string]CurrencyRisk, at time.Time) {
	if m == nil {
		return
	}
	for _, b := range books {
		m.exposureBase.WithLabelValues(b.Book).Set(b.Base)
		m.exposureQuote.WithLabelValues(b.Book, b.Currency).Set(b.Quote)
		m.markPrice.WithLabelValues(b.Book).Set(b.Mark)
		m.dailyVol.WithLabelValues(b.Book).Set(b.DailyVol)
		fb := 0.0
		if b.Fallback {
			fb = 1
		}
		m.markFallback.WithLabelValues(b.Book).Set(fb)
	}
	for a, v := range netBase {
		m.netBase.WithLabelValues(a).Set(v)
	}
	for c, r := range ccy {
		m.grossQuote.WithLabelValues(c).Set(r.Gross)
		m.netQuote.WithLabelValues(c).Set(r.Net)
		m.varQuote.WithLabelValues(c).Set(r.VaR)
		if r.Limit > 0 {
			m.varLimit.WithLabelValues(c).Set(r.Limit)
		}
	}
	m.lastRun.Set(float64(at.Unix()))
}

// RecordRunError counts a run that could not read positions.
func (m *RiskSeries) RecordRunError() {
	if m != nil {
		m.runErrors.Inc()
	}
}
