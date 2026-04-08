package bitso

import "math"

// MinExitPriceAfterFees returns the minimum last-trade price (minor per major, e.g. MXN per BTC)
// at which selling the position is break-even on round-trip fees, assuming the buy filled as maker
// and the sell executes as taker. Rates are decimal fractions of notional (e.g. maker_fee_decimal
// from GET /api/v3/fees).
func MinExitPriceAfterFees(entryPrice, makerRate, takerRate float64) float64 {
	if takerRate >= 1 {
		return math.Inf(1)
	}
	return entryPrice * (1 + makerRate) / (1 - takerRate)
}

// NetQuotePnLPerBase returns quote-currency P&L for one unit of major (e.g. one BTC) after maker
// fee on the buy and taker fee on the sell, using the same rate convention as MinExitPriceAfterFees.
func NetQuotePnLPerBase(entryPrice, exitPrice, makerRate, takerRate float64) float64 {
	return exitPrice*(1-takerRate) - entryPrice*(1+makerRate)
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
