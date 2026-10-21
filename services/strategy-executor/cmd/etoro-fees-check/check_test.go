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
