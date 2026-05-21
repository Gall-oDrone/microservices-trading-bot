package sync

import (
	"testing"

	"bitso-trading-platform/shared/pkg/bitso"
)

// TestBuildFillObservation_buyTakerFeeInBase verifies the BUY taker case that mirrors the
// organic_lp_pnl_1779300471 fill on Stage (docs/strategy-fee-accuracy/):
//
//	BUY 0.001 BTC @ 1,340,720; fee 0.00000741 BTC; maker_side=sell → we were taker; rate ≈ 0.741%.
func TestBuildFillObservation_buyTakerFeeInBase(t *testing.T) {
	book := bitso.NewBook(bitso.Currency("btc"), bitso.Currency("mxn"))
	tr := &bitso.UserTrade{
		Book:          *book,
		Major:         bitso.ToMonetary(0.001),
		MajorCurrency: bitso.Currency("btc"),
		Minor:         bitso.ToMonetary(-1340.72),
		MinorCurrency: bitso.Currency("mxn"),
		FeesAmount:    bitso.ToMonetaryWithP(0.00000741),
		FeesCurrency:  bitso.Currency("btc"),
		Price:         bitso.ToMonetaryWithP(1340720),
		Side:          bitso.OrderSideBuy,
		MakerSide:     bitso.OrderSideSell, // counterparty was maker → we were taker
	}
	obs := buildFillObservation(tr, 0.001)
	if obs.Liquidity != bitso.LiquidityTaker {
		t.Fatalf("liquidity: got %q want %q", obs.Liquidity, bitso.LiquidityTaker)
	}
	if got, want := obs.FeeRate, 0.00741; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("fee_rate: got %v want ~%v", got, want)
	}
	if obs.FeeCurrency != "btc" {
		t.Fatalf("fee_currency: got %q want btc", obs.FeeCurrency)
	}
	if got, want := obs.FeeAmount, 0.00000741; got < want-1e-12 || got > want+1e-12 {
		t.Fatalf("fee_amount: got %v want %v", got, want)
	}
}

// TestBuildFillObservation_sellMakerFeeInQuote mirrors the SELL leg of the same Stage fill:
//
//	SELL 0.001 BTC @ 1,341,020; fee 7.643814 MXN; maker_side=sell → we were maker; rate ≈ 0.570%.
func TestBuildFillObservation_sellMakerFeeInQuote(t *testing.T) {
	book := bitso.NewBook(bitso.Currency("btc"), bitso.Currency("mxn"))
	tr := &bitso.UserTrade{
		Book:          *book,
		Major:         bitso.ToMonetary(-0.001),
		MajorCurrency: bitso.Currency("btc"),
		Minor:         bitso.ToMonetary(1341.02),
		MinorCurrency: bitso.Currency("mxn"),
		FeesAmount:    bitso.ToMonetary(7.643814),
		FeesCurrency:  bitso.Currency("mxn"),
		Price:         bitso.ToMonetary(1341020),
		Side:          bitso.OrderSideSell,
		MakerSide:     bitso.OrderSideSell, // we rested → we were maker
	}
	obs := buildFillObservation(tr, 0.001)
	if obs.Liquidity != bitso.LiquidityMaker {
		t.Fatalf("liquidity: got %q want %q", obs.Liquidity, bitso.LiquidityMaker)
	}
	if got, want := obs.FeeRate, 0.0057; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("fee_rate: got %v want ~%v", got, want)
	}
	if obs.FeeCurrency != "mxn" {
		t.Fatalf("fee_currency: got %q want mxn", obs.FeeCurrency)
	}
}

// TestBuildFillObservation_missingMakerSide ensures absent MakerSide doesn't crash; liquidity
// stays empty so the caller falls back to the strategy's configured assumption.
func TestBuildFillObservation_missingMakerSide(t *testing.T) {
	book := bitso.NewBook(bitso.Currency("btc"), bitso.Currency("mxn"))
	tr := &bitso.UserTrade{
		Book:         *book,
		Major:        bitso.ToMonetary(0.001),
		Minor:        bitso.ToMonetary(-1340.72),
		FeesAmount:   bitso.ToMonetaryWithP(0.00000741),
		FeesCurrency: bitso.Currency("btc"),
		Price:        bitso.ToMonetaryWithP(1340720),
		Side:         bitso.OrderSideBuy,
		// MakerSide intentionally unset (OrderSideNone)
	}
	obs := buildFillObservation(tr, 0.001)
	if obs.Liquidity != "" {
		t.Fatalf("liquidity should be empty when maker_side missing, got %q", obs.Liquidity)
	}
	if obs.FeeRate <= 0 {
		t.Fatalf("fee_rate should still be derivable from fee amount, got %v", obs.FeeRate)
	}
}
