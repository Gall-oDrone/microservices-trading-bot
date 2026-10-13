// Package cfdrisk is the risk and analytics layer of the eToro index-CFD
// executor (porting plan phase P4, 2026-10-13): VaR, expected shortfall and
// stress on NYSE trading days, a CFD-priced Monte Carlo, P&L attribution
// with financing, and fill quality. Reporting only: nothing here blocks an
// order or feeds the executor, and nothing here calls eToro.
//
// It reuses the frozen Bitso models where their arithmetic carries over
// (shared/pkg/varmodel for the EWMA/long vol, the historical quantiles, the
// Basel backtest and the episode replay; shared/pkg/cfdsim for pricing) and
// changes only what an exchange-traded index breaks:
//
//   - returns chain across NYSE trading days (Friday -> Monday is one
//     return, a holiday is skipped, a missing trading day breaks the chain),
//     not calendar days;
//   - the long window is 252 returns (one trading year), not 365;
//   - two books do not share one base asset, so the portfolio VaR needs a
//     correlation (EWMA, on aligned trading days) instead of assuming 1;
//   - the stress episodes are equity-index crises on ^NDX / ^GSPC dates;
//   - simulated paths advance by trading days and pay overnight financing
//     per calendar night, with the margin close-out flag.
//
// All numbers are decimals (fractions of notional, USD): percentages and
// basis points only appear at the report boundary (cmd/etoro-risk-report).
package cfdrisk

import (
	"time"

	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/shared/pkg/varmodel"
)

// DefaultParams is the index-CFD model: the Bitso recursion with a one
// trading-year long window. Lambda, the EWMA seed, MinReturns and the Basel
// window are the same; only LongWindow changes (365 calendar days of a
// 24/7 market -> 252 trading days).
func DefaultParams() varmodel.Params {
	return varmodel.Params{Lambda: 0.94, SeedReturns: varmodel.SeedReturns, LongWindow: 252,
		MinReturns: 60, BacktestWindow: 250, Confidence: 0.99}
}

// MinScenarios is the fewest aligned historical scenarios accepted for the
// historical VaR and ES, as in the Bitso monitor (plan §6.4.9): with the
// 252-day window it demands a nearly complete year.
const MinScenarios = 250

// NextTradingDay is the chain of the model: the next NYSE trading day.
func NextTradingDay(d time.Time) time.Time { return mktcal.NextTradingDay(d) }

// TradingDayCloses keeps the closes dated on NYSE trading days, sorted as
// given. eToro quotes the index CFDs on holidays and (since ~September 2026)
// at weekends; such bars are dropped before the chain is formed because a
// Saturday bar would otherwise pair with Monday's.
func TradingDayCloses(closes []varmodel.Close) []varmodel.Close {
	out := make([]varmodel.Close, 0, len(closes))
	for _, c := range closes {
		if mktcal.IsTradingDay(c.Date) {
			out = append(out, c)
		}
	}
	return out
}

// TradingLogReturns forms a return only when b's date is the trading day
// after a's: Friday -> Monday and Wednesday -> Friday over a Thursday
// holiday are single returns; a missing trading day breaks the chain, as a
// missing calendar day does in varmodel.LogReturns.
func TradingLogReturns(closes []varmodel.Close) []varmodel.Return {
	return varmodel.LogReturnsOn(TradingDayCloses(closes), NextTradingDay)
}

// Estimate is varmodel.EstimateWith on the trading-day chain with
// DefaultParams: the next-day vol, the 250-trading-day backtest and the last
// 252 returns for historical simulation.
func Estimate(closes []varmodel.Close) (varmodel.Estimate, error) {
	return varmodel.EstimateWith(TradingDayCloses(closes), NextTradingDay, DefaultParams())
}
