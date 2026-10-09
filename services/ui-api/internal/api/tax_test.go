package api

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/dailyrule"
)

func TestBuildTaxLots_FIFO(t *testing.T) {
	fills := []Fill{
		{FillDate: "2026-01-10", Side: "buy", NetBTC: 1, Notional: 100},
		{FillDate: "2026-03-01", Side: "buy", NetBTC: 1, Notional: 200},
		// Sells 1.5 BTC for 450 gross, 6 fee: 296 per BTC net.
		{FillDate: "2027-01-05", Side: "sell", NetBTC: -1.5, Notional: 450, FeeQuote: 6},
	}
	fx := func(d string) (float64, bool) {
		if d == "2026-03-01" {
			return 0, false
		}
		return 20, true
	}
	tl := buildTaxLots("usd", fills, 300, "2027-01-10", fx)
	if tl == nil || len(tl.Sales) != 2 || len(tl.Open) != 1 || tl.UnmatchedBTC != 0 {
		t.Fatalf("lots %+v", tl)
	}
	first, second := tl.Sales[0], tl.Sales[1]
	// The oldest lot is sold first: 1 BTC bought at 100, then 0.5 of the 200 lot.
	if first.BuyDate != "2026-01-10" || !near(first.QtyBTC, 1, 1e-12) || !near(first.Gain, 296-100, 1e-9) || first.HoldingDays != 360 {
		t.Fatalf("first %+v", first)
	}
	if first.GainMXN == nil || !near(*first.GainMXN, 296*20-100*20, 1e-9) {
		t.Fatalf("first mxn %v", first.GainMXN)
	}
	if second.BuyDate != "2026-03-01" || !near(second.QtyBTC, 0.5, 1e-12) || !near(second.Gain, 148-100, 1e-9) || second.GainMXN != nil {
		t.Fatalf("second %+v", second)
	}
	o := tl.Open[0]
	if o.BuyDate != "2026-03-01" || !near(o.RemainingBTC, 0.5, 1e-12) || !near(o.Unrealized, 0.5*(300-200), 1e-9) || o.HoldingDays != 315 {
		t.Fatalf("open %+v", o)
	}
	if len(tl.Years) != 1 || tl.Years[0].Year != 2027 || tl.Years[0].Sales != 2 || !near(tl.Years[0].Gain, 244, 1e-9) || tl.Years[0].GainMXN != nil {
		t.Fatalf("years %+v", tl.Years) // one sale lacks a rate, so the year's MXN total is unknown
	}
	if !near(tl.Realized, 244, 1e-9) || !near(tl.Unrealized, 50, 1e-9) {
		t.Fatalf("totals %v %v", tl.Realized, tl.Unrealized)
	}
	if buildTaxLots("mxn", nil, 1, "", fx) != nil {
		t.Fatal("no fills, no lots")
	}
	// Selling more than is held is reported, not hidden.
	if u := buildTaxLots("mxn", []Fill{{FillDate: "2026-01-01", Side: "sell", NetBTC: -0.1, Notional: 10}}, 1, "2026-01-02", fx); !near(u.UnmatchedBTC, 0.1, 1e-12) {
		t.Fatalf("unmatched %v", u.UnmatchedBTC)
	}
}

func TestImpliedFX(t *testing.T) {
	d := func(s string) time.Time { x, _ := time.Parse("2006-01-02", s); return x }
	usd := []dailyrule.Bar{{Date: d("2026-10-01"), Close: 100}, {Date: d("2026-10-03"), Close: 100}}
	mxn := []dailyrule.Bar{{Date: d("2026-10-01"), Close: 1800}, {Date: d("2026-10-03"), Close: 1900}}
	fx := impliedFX(usd, mxn)
	if v, ok := fx("2026-10-02"); !ok || v != 18 { // the last rate at or before
		t.Fatalf("10-02: %v %v", v, ok)
	}
	if v, ok := fx("2026-10-03"); !ok || v != 19 {
		t.Fatalf("10-03: %v", v)
	}
	if _, ok := fx("2026-09-30"); ok {
		t.Fatal("before the series")
	}
}

func TestPerformanceTaxLots(t *testing.T) {
	ts := newTestServer(t, fixedNow, nil)
	for _, b := range []string{"btc_mxn", "btc_usd"} {
		p := get[PerformanceResponse](t, ts, "/api/ui/forward-tests/"+b+"/performance", 200)
		tl := p.TaxLots
		if tl == nil || tl.Method != "FIFO" || len(tl.Open) == 0 {
			t.Fatalf("%s tax lots %+v", b, tl)
		}
		var rem float64
		for _, l := range tl.Open {
			rem += l.RemainingBTC
		}
		// The open lots add up to the average-cost position.
		if p.PnL == nil || !near(rem, p.PnL.PositionBTC, 1e-9) || !near(tl.Realized+tl.Unrealized, p.PnL.Total, 1e-6) {
			t.Fatalf("%s: lots %v BTC / %v vs position %v / %v", b, rem, tl.Realized+tl.Unrealized, p.PnL.PositionBTC, p.PnL.Total)
		}
	}
}
