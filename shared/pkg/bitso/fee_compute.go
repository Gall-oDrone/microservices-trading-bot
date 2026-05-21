package bitso

import (
	"math"
	"strings"
)

// LiquidityMaker / LiquidityTaker label the role of a fill on the venue.
const (
	LiquidityMaker = "maker"
	LiquidityTaker = "taker"
)

// DeriveFillLiquidity returns "maker" when our `side` matches Bitso's `makerSide` for the same trade,
// otherwise "taker". On Bitso, `MakerSide` indicates which side of the trade rested on the book;
// if that matches our order's side, we were the maker.
//
// Returns "" when either side is OrderSideNone (caller should fall back to configured assumption).
func DeriveFillLiquidity(side, makerSide OrderSide) string {
	if side == OrderSideNone || makerSide == OrderSideNone {
		return ""
	}
	if side == makerSide {
		return LiquidityMaker
	}
	return LiquidityTaker
}

// DeriveFillFeeRate computes the actual decimal fee rate (fraction of notional) for a single fill,
// given the absolute base amount (`majorAbs`), the absolute quote amount (`minorAbs`), the fee
// charged (`feesAmount`) and whether Bitso billed the fee in the base currency. Returns 0 when
// inputs are unusable.
//
// Bitso bills BUY fees in the base currency (e.g. BTC for btc_mxn) and SELL fees in the quote
// currency (e.g. MXN for btc_mxn). Both representations collapse to the same decimal:
//
//	feeIsBase  → rate = feesAmount / majorAbs (both base units → unitless ratio)
//	!feeIsBase → rate = feesAmount / minorAbs (both quote units → unitless ratio)
func DeriveFillFeeRate(feesAmount, majorAbs, minorAbs float64, feeIsBase bool) float64 {
	if feesAmount <= 0 {
		return 0
	}
	if feeIsBase {
		if majorAbs <= 0 {
			return 0
		}
		return feesAmount / majorAbs
	}
	if minorAbs <= 0 {
		return 0
	}
	return feesAmount / minorAbs
}

// IsBaseCurrencyForBook returns true when `feeCurrency` matches the base (major) currency of a
// book formatted as "<base>_<quote>" (e.g. "btc_mxn" → base "btc"). Returns false on unknown
// formats; callers should default to false (quote).
func IsBaseCurrencyForBook(feeCurrency Currency, book string) bool {
	if feeCurrency == CurrencyNone || book == "" {
		return false
	}
	parts := strings.SplitN(strings.ToLower(book), "_", 2)
	if len(parts) != 2 || parts[0] == "" {
		return false
	}
	return strings.EqualFold(string(feeCurrency), parts[0])
}

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
