package cfdrisk

import (
	"path/filepath"
	"testing"

	"bitso-trading-platform/shared/pkg/etoroledger"
)

// The two real 2026-10-12 opens (testdata/ledger_2026-10-12.jsonl, copied
// from the demo ledger: ids and keys are already public in the ledger, no
// secrets), marked at the 2026-10-12 closes as of 2026-10-13.
func readFixture(t *testing.T) []etoroledger.Record {
	t.Helper()
	recs, err := etoroledger.ReadFile(filepath.Join("testdata", "ledger_2026-10-12.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("%d records", len(recs))
	}
	return recs
}

func TestAttributionOnRealOpens(t *testing.T) {
	recs := readFixture(t)
	charged := 0.26
	a := Attribute(AttributionInput{
		Records:          recs,
		Marks:            map[string]Mark{"nsdq100": {Date: ymd("2026-10-12"), Close: 30649.5}, "spx500": {Date: ymd("2026-10-12"), Close: 7806.65}},
		AsOf:             ymd("2026-10-13"),
		OvernightBps:     map[string]float64{"nsdq100": 2.3, "spx500": 2.3},
		FinancingCharged: map[string]float64{"nsdq100": charged},
	})
	if len(a.Books) != 2 || a.Books[0].Book != "nsdq100" || a.Books[1].Book != "spx500" {
		t.Fatalf("%+v", a.Books)
	}
	n, s := a.Books[0], a.Books[1]
	if len(n.Lots) != 1 || !n.Lots[0].Open || n.Lots[0].PositionID != "3616683146" || n.Lots[0].OpenDate != "2026-10-12" || n.Lots[0].Nights != 1 {
		t.Fatalf("nsdq100 lot %+v", n.Lots)
	}
	// Price: (30649.5 - 30726.9) x 0.035799; spread 0.04; financing one
	// night at 2.3 bps of 1,099.99.
	if !near(n.Total.PricePnL, -77.4*0.035799, 1e-9) || n.Total.SpreadPaid != 0.04 || !near(n.Total.FinancingModelled, 2.3e-4*1099.99, 1e-12) || n.Total.Markup != 0 {
		t.Fatalf("nsdq100 %+v", n.Total)
	}
	if !near(n.Total.Net, n.Total.PricePnL-0.04-2.3e-4*1099.99, 1e-12) || n.Realized.Net != 0 || n.Unrealized.Net != n.Total.Net {
		t.Fatalf("nsdq100 net %+v", n)
	}
	if n.FinancingCharged == nil || *n.FinancingCharged != charged || n.NetWithCharged == nil || !near(*n.NetWithCharged, n.Total.PricePnL-0.04-charged, 1e-12) {
		t.Fatalf("charged %v %v", n.FinancingCharged, n.NetWithCharged)
	}
	if s.FinancingCharged != nil || !near(s.Total.PricePnL, -9.42*0.140735, 1e-9) {
		t.Fatalf("spx500 %+v", s.Total)
	}
	if !near(a.Total.Net, n.Total.Net+s.Total.Net, 1e-12) || !near(a.Unrealized.PricePnL, n.Total.PricePnL+s.Total.PricePnL, 1e-12) || a.Realized.Net != 0 {
		t.Fatalf("totals %+v", a)
	}
	// Paper not started (forward window begins 2026-10-13): equity 1.0;
	// the demo lot is marked below its open, so the tracking is negative.
	if n.PaperStarted || n.PaperEquity != 1 || !near(n.DemoEquity, 1+n.Total.Net/1099.99, 1e-12) || n.TrackingBps >= 0 || !near(n.TrackingBps, (n.DemoEquity-1)*1e4, 1e-9) {
		t.Fatalf("tracking %+v", n)
	}
}

// A close record after the open realizes the lot: nights from fill to
// fill, both legs' spread, price P&L at the close fill.
func TestAttributionRealizesClosedLot(t *testing.T) {
	recs := readFixture(t)
	open := recs[0] // nsdq100
	closeRec := open
	closeRec.Decision.BarDate, closeRec.Decision.FillDate = "2026-10-16", "2026-10-19" // Fri decision, Mon fill: 7 nights
	closeRec.Paper.Started, closeRec.Paper.Equity = true, 1.01
	d := *open.Demo
	o := *open.Demo.Order
	o.Kind, o.AvgPrice, o.SpreadCost, o.Fees = etoroledger.ActionClose, 31000, 0.05, 0
	d.Order, d.Action = &o, etoroledger.ActionClose
	d.PositionBefore, d.PositionAfter = open.Demo.PositionAfter, etoroledger.Flat()
	closeRec.Demo = &d
	a := Attribute(AttributionInput{Records: []etoroledger.Record{open, closeRec}, AsOf: ymd("2026-10-20"),
		OvernightBps: map[string]float64{"nsdq100": 2.3}})
	n := a.Books[0]
	lot := n.Lots[0]
	if lot.Open || lot.CloseDate != "2026-10-19" || lot.Nights != 7 || lot.ClosePrice != 31000 {
		t.Fatalf("%+v", lot)
	}
	wantPrice := (31000 - 30726.9) * 0.035799
	if !near(n.Realized.PricePnL, wantPrice, 1e-9) || !near(n.Realized.SpreadPaid, 0.09, 1e-12) || !near(n.Realized.FinancingModelled, 7*2.3e-4*1099.99, 1e-12) || n.Unrealized.Net != 0 {
		t.Fatalf("%+v", n)
	}
	if !n.PaperStarted || n.PaperEquity != 1.01 || !near(n.TrackingBps, (1+n.Total.Net/1099.99-1.01)*1e4, 1e-9) {
		t.Fatalf("tracking %+v", n)
	}
}

// Fill quality on the two real opens: the ledger's own fill_vs_mid and
// fill_vs_close are carried through, fill vs the day's bar open is
// computed, the latency is finished - started, the 2026-10-12 reconcile
// (missing_position 5 s after the fill) is read as portfolio lag, and the
// measured spread per leg is eToro's spread cost against the frozen half
// round trip.
func TestFillQualityOnRealOpens(t *testing.T) {
	recs := readFixture(t)
	q := MeasureFills(FillInput{
		Records: recs,
		Opens:   map[string]map[string]float64{"nsdq100": {"2026-10-12": 30899.1}, "spx500": {"2026-10-12": 7818.02}},
		Reconcile: map[string]ReconcileDay{"2026-10-12": {
			GeneratedAt: ymd("2026-10-12").Add(13*3600*1e9 + 35*60*1e9 + 14*1e9), Clean: false, PositionReads: 1,
			Findings: []ReconcileFinding{{"drift", "nsdq100", "missing_position"}, {"drift", "spx500", "missing_position"}},
		}},
		FrozenSpreadRTBps: map[string]float64{"nsdq100": 15.5, "spx500": 14.8},
	})
	if len(q.Fills) != 2 || q.Fills[0].Book != "nsdq100" || q.Fills[1].Book != "spx500" {
		t.Fatalf("%+v", q.Fills)
	}
	n, s := q.Fills[0], q.Fills[1]
	if !near(n.FillVsMidBps, -5.220709138192423, 1e-12) || !near(n.FillVsCloseBps, -53.86281167761342, 1e-12) || !near(n.QuotedSpreadBps, 0.22769447954758007, 1e-12) {
		t.Fatalf("nsdq100 ledger figures %+v", n)
	}
	if n.FillVsOpenBps == nil || !near(*n.FillVsOpenBps, (30726.9-30899.1)/30899.1*1e4, 1e-9) || n.DayOpen != 30899.1 {
		t.Fatalf("nsdq100 fill vs open %v", n.FillVsOpenBps)
	}
	if !near(n.LatencyS, 4.592558185, 1e-6) || !near(s.LatencyS, 3.742487983, 1e-6) {
		t.Fatalf("latency %v %v", n.LatencyS, s.LatencyS)
	}
	if !n.PortfolioLag || !s.PortfolioLag || n.ReconcileClean == nil || *n.ReconcileClean || len(n.ReconcileCodes) != 1 {
		t.Fatalf("lag %+v %+v", n, s)
	}
	if !near(n.SpreadCostBps, 0.04/1099.99*1e4, 1e-12) || n.PositionID != "3616683146" || n.Kind != "open" {
		t.Fatalf("%+v", n)
	}
	// Aggregates.
	if len(q.Books) != 2 || q.All.N != 2 || q.All.PortfolioLags != 2 {
		t.Fatalf("%+v", q.All)
	}
	bn := q.Books[0]
	if bn.FrozenSpreadPerLegBps != 7.75 || !near(bn.MeasuredSpreadPerLegBps, 0.04/1099.99*1e4, 1e-12) || !near(bn.MeasuredVsFrozen, 0.04/1099.99*1e4/7.75, 1e-12) {
		t.Fatalf("cost-model check %+v", bn)
	}
	if !near(bn.HalfQuotedSpreadBps, 0.22769447954758007/2, 1e-12) || bn.FillVsMid.Worst != n.FillVsMidBps || bn.FillVsOpen.N != 1 {
		t.Fatalf("%+v", bn)
	}
	if !near(q.All.FrozenSpreadPerLegBps, (7.75+7.4)/2, 1e-12) || !near(q.All.FillVsMid.Mean, (n.FillVsMidBps+s.FillVsMidBps)/2, 1e-12) || q.All.FillVsMid.Worst != s.FillVsMidBps {
		t.Fatalf("all %+v", q.All)
	}
	// Without the day's bar and report: no fill-vs-open, no lag, no clean flag.
	bare := MeasureFills(FillInput{Records: recs})
	if bare.Fills[0].FillVsOpenBps != nil || bare.Fills[0].PortfolioLag || bare.Fills[0].ReconcileClean != nil || bare.All.FillVsOpen.N != 0 {
		t.Fatalf("%+v", bare.Fills[0])
	}
}
