package bitso

import (
	"encoding/json"
	"testing"
)

func TestFeeDecimalForLiquidity(t *testing.T) {
	const payload = `{
		"maker_fee_decimal": "0.00500000",
		"taker_fee_decimal": "0.00650000"
	}`
	var f Fee
	if err := json.Unmarshal([]byte(payload), &f); err != nil {
		t.Fatal(err)
	}
	if FeeDecimalForLiquidity(&f, "maker") != 0.005 {
		t.Fatalf("maker decimal")
	}
	if FeeDecimalForLiquidity(&f, "taker") != 0.0065 {
		t.Fatalf("taker decimal")
	}
}

func TestMinExitPriceAfterFees_docExample(t *testing.T) {
	// Example from Bitso docs: btc_mxn maker 0.5%, taker 0.65% as decimals 0.005 / 0.0065
	entry := 1_000_000.0
	maker := 0.005
	taker := 0.0065
	got := MinExitPriceAfterRoundTrip(entry, maker, taker)
	want := entry * (1 + maker) / (1 - taker)
	if got != want {
		t.Fatalf("MinExitPriceAfterRoundTrip: got %v want %v", got, want)
	}
	// Sanity: break-even price above entry
	if got <= entry {
		t.Fatalf("expected break-even > entry, got %v vs entry %v", got, entry)
	}
}

func TestNetQuotePnLPerBase_breakEven(t *testing.T) {
	entry := 1_000_000.0
	maker := 0.005
	taker := 0.0065
	exit := MinExitPriceAfterRoundTrip(entry, maker, taker)
	net := NetQuotePnLPerBase(entry, exit, maker, taker)
	if net < -1e-6 || net > 1e-6 {
		t.Fatalf("expected ~0 net at break-even exit, got %v", net)
	}
}

func TestDeriveFillLiquidity(t *testing.T) {
	cases := []struct {
		name      string
		side      OrderSide
		makerSide OrderSide
		want      string
	}{
		{"buy_we_are_maker", OrderSideBuy, OrderSideBuy, LiquidityMaker},
		{"sell_we_are_maker", OrderSideSell, OrderSideSell, LiquidityMaker},
		{"buy_we_are_taker", OrderSideBuy, OrderSideSell, LiquidityTaker},
		{"sell_we_are_taker", OrderSideSell, OrderSideBuy, LiquidityTaker},
		{"unknown_side", OrderSideNone, OrderSideBuy, ""},
		{"unknown_maker_side", OrderSideBuy, OrderSideNone, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveFillLiquidity(tc.side, tc.makerSide); got != tc.want {
				t.Fatalf("DeriveFillLiquidity(%v, %v) = %q want %q", tc.side, tc.makerSide, got, tc.want)
			}
		})
	}
}

func TestDeriveFillFeeRate_baseAndQuote(t *testing.T) {
	// Real Bitso fills from organic_lp_pnl_1779300471 (Stage, May 2026):
	//   BUY  taker: 0.001 BTC @ 1,340,720; fee 0.00000741 BTC → 0.741% of notional
	//   SELL maker: 0.001 BTC @ 1,341,020; fee 7.643814 MXN → 0.570% of notional
	const (
		eps = 1e-9
	)
	// BUY taker fee in base currency (BTC)
	{
		got := DeriveFillFeeRate(0.00000741, 0.001, 0.001*1_340_720, true)
		want := 0.00741
		if got < want-eps || got > want+eps {
			t.Fatalf("buy taker fee rate: got %v want ~%v", got, want)
		}
	}
	// SELL maker fee in quote currency (MXN)
	{
		got := DeriveFillFeeRate(7.643814, 0.001, 0.001*1_341_020, false)
		want := 0.0057
		if got < want-eps || got > want+eps {
			t.Fatalf("sell maker fee rate: got %v want ~%v", got, want)
		}
	}
	// Zero / negative inputs → 0
	if r := DeriveFillFeeRate(0, 1, 1, true); r != 0 {
		t.Fatalf("zero fee → 0, got %v", r)
	}
	if r := DeriveFillFeeRate(1, 0, 1, true); r != 0 {
		t.Fatalf("zero major + base fee → 0, got %v", r)
	}
	if r := DeriveFillFeeRate(1, 1, 0, false); r != 0 {
		t.Fatalf("zero minor + quote fee → 0, got %v", r)
	}
}

func TestIsBaseCurrencyForBook(t *testing.T) {
	if !IsBaseCurrencyForBook("btc", "btc_mxn") {
		t.Fatalf("btc on btc_mxn should be base")
	}
	if IsBaseCurrencyForBook("mxn", "btc_mxn") {
		t.Fatalf("mxn on btc_mxn is quote, not base")
	}
	if IsBaseCurrencyForBook("", "btc_mxn") {
		t.Fatalf("empty currency should be false")
	}
	if IsBaseCurrencyForBook("btc", "malformed") {
		t.Fatalf("malformed book should be false")
	}
	// Case-insensitive
	if !IsBaseCurrencyForBook("BTC", "btc_mxn") {
		t.Fatalf("case-insensitive base check")
	}
}

func TestLookupFeeByBook_andDecimalRates(t *testing.T) {
	const payload = `{
		"fees": [{
			"book": "btc_mxn",
			"maker_fee_percent": "0.5000",
			"maker_fee_decimal": "0.00500000",
			"taker_fee_percent": "0.6500",
			"taker_fee_decimal": "0.00650000"
		}],
		"withdrawal_fees": {}
	}`
	var cf CustomerFees
	if err := json.Unmarshal([]byte(payload), &cf); err != nil {
		t.Fatal(err)
	}
	f := LookupFeeByBook(&cf, "btc_mxn")
	if f == nil {
		t.Fatal("expected fee row")
	}
	m, tk := f.MakerTakerDecimalRates()
	if m != 0.005 || tk != 0.0065 {
		t.Fatalf("rates: maker=%v taker=%v", m, tk)
	}
}
