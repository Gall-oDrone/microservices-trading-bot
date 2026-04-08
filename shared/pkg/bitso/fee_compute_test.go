package bitso

import (
	"encoding/json"
	"testing"
)

func TestMinExitPriceAfterFees_docExample(t *testing.T) {
	// Example from Bitso docs: btc_mxn maker 0.5%, taker 0.65% as decimals 0.005 / 0.0065
	entry := 1_000_000.0
	maker := 0.005
	taker := 0.0065
	got := MinExitPriceAfterFees(entry, maker, taker)
	want := entry * (1 + maker) / (1 - taker)
	if got != want {
		t.Fatalf("MinExitPriceAfterFees: got %v want %v", got, want)
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
	exit := MinExitPriceAfterFees(entry, maker, taker)
	net := NetQuotePnLPerBase(entry, exit, maker, taker)
	if net < -1e-6 || net > 1e-6 {
		t.Fatalf("expected ~0 net at break-even exit, got %v", net)
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
