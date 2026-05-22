package strategies

import (
	"bitso-trading-platform/shared/pkg/bitso"
)

// PositionFeeRates stores realized per-leg fee rates from Bitso fills (POINT-9).
type PositionFeeRates struct {
	BuyFeeRate  float64
	SellFeeRate float64
}

// ApplyBuyFillFee stores the realized BUY-leg fee rate when present on the fill.
func ApplyBuyFillFee(fill OrderFill, rates *PositionFeeRates) {
	if rates == nil {
		return
	}
	if fill.FeeRate > 0 {
		rates.BuyFeeRate = fill.FeeRate
	} else if fill.BuyFeeRate != nil && *fill.BuyFeeRate > 0 {
		rates.BuyFeeRate = *fill.BuyFeeRate
	}
}

// ApplySellFillFee stores the realized SELL-leg fee rate when present on the fill.
func ApplySellFillFee(fill OrderFill, rates *PositionFeeRates) {
	if rates == nil {
		return
	}
	if fill.FeeRate > 0 {
		rates.SellFeeRate = fill.FeeRate
	}
}

// RealizedQuotePnL returns gross and net quote P&L for a closed round-trip.
// When buyFeeRate or sellFeeRate > 0, net uses bitso.NetQuotePnLPerBase.
func RealizedQuotePnL(entry, exit, positionSize, buyFeeRate, sellFeeRate float64) (gross, net float64, feeModel string) {
	gross = (exit - entry) * positionSize
	net = gross
	feeModel = "gross_only"
	if buyFeeRate > 0 && sellFeeRate > 0 {
		net = bitso.NetQuotePnLPerBase(entry, exit, buyFeeRate, sellFeeRate) * positionSize
		feeModel = "bitso_realized"
	}
	return gross, net, feeModel
}
