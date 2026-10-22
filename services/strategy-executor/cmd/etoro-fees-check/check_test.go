package main

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoroledger"
)

type fake struct {
	positions []broker.Position
	closed    []broker.ClosedPosition
}

func (f *fake) Venue() string { return "etoro" }
func (f *fake) Quote(context.Context, broker.Instrument) (broker.Quote, error) {
	return broker.Quote{}, nil
}
func (f *fake) Open(context.Context, broker.OpenRequest) (broker.Order, error) {
	panic("fees-check must never place an order")
}
func (f *fake) Close(context.Context, broker.CloseRequest) (broker.Order, error) {
	panic("fees-check must never close a position")
}
func (f *fake) Positions(context.Context) ([]broker.Position, error) { return f.positions, nil }
func (f *fake) Account(context.Context) (broker.Account, error) {
	return broker.Account{Currency: "USD"}, nil
}
func (f *fake) PreviewOpen(context.Context, broker.OpenRequest) (broker.Costs, error) {
	return broker.Costs{}, nil
}
func (f *fake) ClosedPositions(context.Context, time.Time) ([]broker.ClosedPosition, error) {
	return f.closed, nil
}

// The week-1 positions of FORWARD-TEST-RESULTS-SMA50-INDEX-CFD-2026-10-19 §3.
var (
	nsdq = broker.Instrument{Venue: "etoro", Symbol: "NSDQ100", ID: 28}
	spx  = broker.Instrument{Venue: "etoro", Symbol: "SPX500", ID: 27}
	// NSDQ100: opened 2026-10-12 13:35Z, closed 2026-10-16 13:35Z = 4 nights.
	nsdqClosed = broker.ClosedPosition{ID: "3616683146", Instrument: nsdq, Side: broker.Long, Units: 0.035799,
		OpenRate: 30726.90, CloseRate: 29770.50, Amount: 1099.99,
		OpenedAt: time.Date(2026, 10, 12, 13, 35, 10, 0, time.UTC), ClosedAt: time.Date(2026, 10, 16, 13, 35, 5, 0, time.UTC),
		NetProfit: -34.24}
	// SPX500: opened 2026-10-12, closed 2026-10-19 = 7 nights.
	spxClosed = broker.ClosedPosition{ID: "3616683295", Instrument: spx, Side: broker.Long, Units: 0.140735,
		OpenRate: 7816.07, CloseRate: 7699.85, Amount: 1099.99,
		OpenedAt: time.Date(2026, 10, 12, 13, 35, 12, 0, time.UTC), ClosedAt: time.Date(2026, 10, 19, 13, 35, 8, 0, time.UTC),
		NetProfit: -16.36}
	now = time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
)

func ledgerWith(ids map[string]string) []etoroledger.Record {
	var out []etoroledger.Record
	for id, book := range ids {
		out = append(out, etoroledger.Record{Mode: etoroledger.ModeDemo, Book: book,
			Demo: &etoroledger.Demo{PositionAfter: etoroledger.Position{State: "long", PositionID: id}}})
	}
	return out
}

func TestNights(t *testing.T) {
	cases := []struct {
		open, close time.Time
		want        int
	}{
		{time.Date(2026, 10, 12, 13, 35, 0, 0, time.UTC), time.Date(2026, 10, 16, 13, 35, 0, 0, time.UTC), 4},
		{time.Date(2026, 10, 12, 13, 35, 0, 0, time.UTC), time.Date(2026, 10, 19, 13, 35, 0, 0, time.UTC), 7},
		{time.Date(2026, 10, 12, 23, 59, 0, 0, time.UTC), time.Date(2026, 10, 13, 0, 1, 0, 0, time.UTC), 1}, // one boundary crossed
		{time.Date(2026, 10, 12, 13, 0, 0, 0, time.UTC), time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC), 0}, // intraday
	}
	for _, c := range cases {
		if got := nights(c.open, c.close); got != c.want {
			t.Errorf("nights(%s, %s) = %d, want %d", c.open, c.close, got, c.want)
		}
	}
}

func TestNotCharged(t *testing.T) {
	f := &fake{closed: []broker.ClosedPosition{nsdqClosed, spxClosed}}
	rep, err := check(context.Background(), f, ledgerWith(map[string]string{"3616683146": "nsdq100", "3616683295": "spx500"}),
		now.AddDate(0, 0, -60), 2.3, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictNotCharged {
		t.Fatalf("verdict %s, want %s", rep.Verdict, VerdictNotCharged)
	}
	if len(rep.Rows) != 2 || rep.Rows[0].Book != "nsdq100" || rep.Rows[0].Source != "ledger" {
		t.Fatalf("rows %+v", rep.Rows)
	}
	// 11 nights x 1,099.99 x 2.3 bps = 2.78 USD: the figure the report carries as unknown.
	if math.Abs(rep.SumModel-2.78) > 0.01 {
		t.Fatalf("model sum %.4f, want 2.78", rep.SumModel)
	}
	if rep.Rows[0].Nights != 4 || rep.Rows[1].Nights != 7 {
		t.Fatalf("nights %d / %d, want 4 / 7", rep.Rows[0].Nights, rep.Rows[1].Nights)
	}
	if math.Abs(rep.Rows[0].PriceLoss-(-34.24)) > 0.01 {
		t.Fatalf("price pnl %.4f", rep.Rows[0].PriceLoss)
	}
	md := markdown(rep)
	if !strings.Contains(md, "not_charged") || !strings.Contains(md, "3616683146") {
		t.Fatalf("markdown:\n%s", md)
	}
}

func TestChargedAsModelled(t *testing.T) {
	a, b := nsdqClosed, spxClosed
	a.Fees, b.Fees = 1.01, 1.77 // 4 and 7 nights at 0.253 USD
	f := &fake{closed: []broker.ClosedPosition{a, b}}
	rep, err := check(context.Background(), f, nil, now.AddDate(0, 0, -60), 2.3, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictCharged {
		t.Fatalf("verdict %s, want %s (venue %.2f model %.2f)", rep.Verdict, VerdictCharged, rep.SumVenue, rep.SumModel)
	}
	if rep.Rows[0].Source != "venue_only" {
		t.Fatalf("without a ledger every row is venue_only, got %s", rep.Rows[0].Source)
	}
}

func TestChargedDifferently(t *testing.T) {
	a := nsdqClosed
	a.Fees = 5
	f := &fake{closed: []broker.ClosedPosition{a}}
	rep, err := check(context.Background(), f, nil, now.AddDate(0, 0, -60), 2.3, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictDiffers {
		t.Fatalf("verdict %s, want %s", rep.Verdict, VerdictDiffers)
	}
}

func TestOpenPositionAndFilter(t *testing.T) {
	// The console position of 2026-10-19 still open at `now` (2 nights).
	openPos := broker.Position{ID: "3622570112", Instrument: nsdq, Side: broker.Long, Units: 0.036950, OpenRate: 29769.80,
		Amount: 1099.99, Leverage: 1, OpenedAt: time.Date(2026, 10, 19, 15, 46, 34, 0, time.UTC), Fees: 0.51}
	f := &fake{closed: []broker.ClosedPosition{nsdqClosed, spxClosed}, positions: []broker.Position{openPos}}
	rep, err := check(context.Background(), f, nil, now.AddDate(0, 0, -60), 2.3, now,
		map[string]bool{"3622570112": true, "999": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 1 || rep.Rows[0].State != "open" || rep.Rows[0].Nights != 2 {
		t.Fatalf("rows %+v", rep.Rows)
	}
	if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "999") {
		t.Fatalf("warnings %v", rep.Warnings)
	}
	if rep.Verdict != VerdictCharged { // 0.51 vs 2 x 0.253 = 0.506
		t.Fatalf("verdict %s", rep.Verdict)
	}
}

func TestNoData(t *testing.T) {
	rep, err := check(context.Background(), &fake{}, nil, now.AddDate(0, 0, -60), 2.3, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictNoData {
		t.Fatalf("verdict %s", rep.Verdict)
	}
}

// The real-account canary (2026-10-22): 50 USD at x20 is 1,000 USD of
// exposure and the model prices the exposure, not the amount; the
// observations series adds the per-night table with the weekend flagged.
func TestCanaryExposureModelAndObservedNights(t *testing.T) {
	open := broker.Position{ID: "4100000001", Instrument: nsdq, Side: broker.Long, Units: 0.04, OpenRate: 25000, Amount: 50, Leverage: 20,
		Exposure: 998.7, Fees: -1.15, OpenedAt: time.Date(2026, 11, 3, 15, 5, 0, 0, time.UTC)}
	f := &fake{positions: []broker.Position{open}}
	at := time.Date(2026, 11, 8, 23, 0, 0, 0, time.UTC) // 5 nights by 00:00 UTC boundaries
	recs := []etoroledger.Record{{Mode: etoroledger.ModeReal, Book: "nsdq100", Demo: &etoroledger.Demo{Env: "real", PositionAfter: etoroledger.Position{State: "long", PositionID: "4100000001"}}}}
	rep, err := checkEnv(context.Background(), f, recs, at.AddDate(0, 0, -60), 2.3, at, nil, "real")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Env != "real" || len(rep.Rows) != 1 {
		t.Fatalf("report %+v", rep)
	}
	r := rep.Rows[0]
	// Model: 998.7 (the venue's exposure) x 2.3 bps x 5 nights = 1.1485.
	if r.Leverage != 20 || r.Exposure != 998.7 || r.Nights != 5 || math.Abs(r.ModelFees-998.7*0.00023*5) > 1e-9 || r.Book != "nsdq100" || r.Source != "ledger" {
		t.Fatalf("row %+v", r)
	}
	// A closed x20 row without a reported exposure: amount x leverage.
	c := broker.ClosedPosition{ID: "4100000001", Instrument: nsdq, Units: 0.04, OpenRate: 25000, CloseRate: 24380, Amount: 50, Leverage: 20, Fees: -1.38,
		OpenedAt: open.OpenedAt, ClosedAt: time.Date(2026, 11, 9, 8, 12, 0, 0, time.UTC)}
	rep2, _ := checkEnv(context.Background(), &fake{closed: []broker.ClosedPosition{c}}, nil, at.AddDate(0, 0, -60), 2.3, at, nil, "real")
	if rep2.Rows[0].Exposure != 1000 || math.Abs(rep2.Rows[0].ModelFees-1000*0.00023*6) > 1e-9 {
		t.Fatalf("closed row %+v", rep2.Rows[0])
	}
	// x1 rows are what they were: exposure = amount.
	rep3, _ := check(context.Background(), &fake{closed: []broker.ClosedPosition{nsdqClosed}}, nil, now.AddDate(0, 0, -60), 2.3, now, nil)
	if rep3.Rows[0].Exposure != nsdqClosed.Amount || rep3.Rows[0].Leverage != 1 || math.Abs(rep3.Rows[0].ModelFees-nsdqClosed.Amount*0.00023*4) > 1e-9 {
		t.Fatalf("x1 row %+v", rep3.Rows[0])
	}

	// Observations: four nights, the third three times the others.
	d := func(v float64) *float64 { return &v }
	pos := func(fees float64, delta *float64, nights int) etoroledger.ObservedPosition {
		return etoroledger.ObservedPosition{PositionID: "4100000001", Exposure: 1000, FeesToDate: fees, FeesDelta: delta, Nights: nights, Preview: &etoroledger.Costs{OvernightFee: -0.22, WeekendFee: -0.66}}
	}
	obs := []etoroledger.Observation{
		{At: "2026-11-03T23:00:00Z", NYDate: "2026-11-03", Positions: []etoroledger.ObservedPosition{pos(0, nil, 1)}},
		{At: "2026-11-04T23:00:00Z", NYDate: "2026-11-04", Positions: []etoroledger.ObservedPosition{pos(-0.23, d(-0.23), 2)}},
		{At: "2026-11-05T23:00:00Z", NYDate: "2026-11-05", Positions: []etoroledger.ObservedPosition{pos(-0.46, d(-0.23), 3)}},
		{At: "2026-11-06T23:00:00Z", NYDate: "2026-11-06", Positions: []etoroledger.ObservedPosition{pos(-1.15, d(-0.69), 4)}},
		{At: "2026-11-07T23:00:00Z", NYDate: "2026-11-07", Positions: []etoroledger.ObservedPosition{pos(-1.38, d(-0.23), 5)}},
	}
	addNights(&rep, obs, "observations.jsonl")
	if len(rep.Nights) != 4 || rep.NightsSource != "observations.jsonl" {
		t.Fatalf("nights %+v", rep.Nights)
	}
	for i, n := range rep.Nights {
		if n.PositionID != "4100000001" || n.Preview != -0.22 || math.Abs(n.Frozen-0.23) > 1e-9 || n.Exposure != 1000 {
			t.Errorf("night %d %+v", i, n)
		}
	}
	if rep.Nights[0].Measured != -0.23 || rep.Nights[0].Weekend || !rep.Nights[2].Weekend || rep.Nights[2].Measured != -0.69 || rep.Nights[2].NYDate != "2026-11-06" {
		t.Fatalf("weekend flag %+v", rep.Nights)
	}
	md := markdown(rep)
	for _, want := range []string{"# eToro real:", "Per night, as observed", "| 2026-11-06 | 4100000001 | 4 | 1000.00 | **-0.6900** | -0.2200 | 0.2300 | yes |", "as observed over 4 night(s)", "| x20 |"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	// A series with no delta yet says so instead of a table.
	var fresh Report
	fresh.OvernightBps = 2.3
	addNights(&fresh, obs[:1], "observations.jsonl")
	if len(fresh.Nights) != 0 || len(fresh.Notes) != 1 || !strings.Contains(fresh.Notes[0], "no night-over-night delta yet") {
		t.Fatalf("fresh %+v", fresh)
	}
	addNights(&fresh, nil, "x")
	if fresh.NightsSource != "observations.jsonl" {
		t.Fatal("an empty series must not touch the report")
	}
}
