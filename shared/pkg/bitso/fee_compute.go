package bitso

import (
	"math"
	"strings"
)

// MinExitPriceAfterRoundTrip is the minimum exit last price for break-even given buy- and sell-leg
// fee rates as decimal fractions of notional (e.g. from maker_fee_decimal / taker_fee_decimal).
func MinExitPriceAfterRoundTrip(entryPrice, buyFeeRate, sellFeeRate float64) float64 {
	if sellFeeRate >= 1 {
		return math.Inf(1)
	}
	return entryPrice * (1 + buyFeeRate) / (1 - sellFeeRate)
}

// MinExitPriceAfterFees is equivalent to MinExitPriceAfterRoundTrip(entry, makerRate, takerRate).
// Deprecated: prefer MinExitPriceAfterRoundTrip with explicit leg roles via FeeDecimalForLiquidity.
func MinExitPriceAfterFees(entryPrice, makerRate, takerRate float64) float64 {
	return MinExitPriceAfterRoundTrip(entryPrice, makerRate, takerRate)
}

// NetQuotePnLPerBase is quote-currency P&L per one unit of major after buyFeeRate on the entry leg
// and sellFeeRate on the exit leg.
func NetQuotePnLPerBase(entryPrice, exitPrice, buyFeeRate, sellFeeRate float64) float64 {
	return exitPrice*(1-sellFeeRate) - entryPrice*(1+buyFeeRate)
}

// LookupFeeByBook returns the fee row for a book string such as "btc_mxn", or nil.
func LookupFeeByBook(cf *CustomerFees, book string) *Fee {
	if cf == nil {
		return nil
	}
	for i := range cf.Fees {
		if cf.Fees[i].Book.String() == book {
			return &cf.Fees[i]
		}
	}
	return nil
}

// MakerTakerDecimalRates returns fee rates as decimal fractions of notional.
// It prefers maker_fee_decimal / taker_fee_decimal from the API; if those are zero it uses
// maker_fee_percent / taker_fee_percent divided by 100 (Bitso returns percent as e.g. "0.5000" for 0.5%).
func (f *Fee) MakerTakerDecimalRates() (maker, taker float64) {
	m := f.MakerFeeDecimal.Float64()
	t := f.TakerFeeDecimal.Float64()
	if m == 0 && f.MakerFeePercent != "" {
		m = f.MakerFeePercent.Float64() / 100.0
	}
	if t == 0 && f.TakerFeePercent != "" {
		t = f.TakerFeePercent.Float64() / 100.0
	}
	return m, t
}

// FeeDecimalForLiquidity returns the fee decimal for "maker" or "taker" (case-insensitive).
// Unknown values default to taker (conservative for costs).
func FeeDecimalForLiquidity(f *Fee, liquidity string) float64 {
	if f == nil {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(liquidity)) {
	case "maker":
		m := f.MakerFeeDecimal.Float64()
		if m == 0 && f.MakerFeePercent != "" {
			m = f.MakerFeePercent.Float64() / 100.0
		}
		return m
	case "taker", "":
		t := f.TakerFeeDecimal.Float64()
		if t == 0 && f.TakerFeePercent != "" {
			t = f.TakerFeePercent.Float64() / 100.0
		}
		return t
	default:
		return FeeDecimalForLiquidity(f, "taker")
	}
}
