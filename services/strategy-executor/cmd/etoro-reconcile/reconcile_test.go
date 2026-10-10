package main

import (
	"context"
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
	panic("reconcile must never place an order")
}
func (f *fake) Close(context.Context, broker.CloseRequest) (broker.Order, error) {
	panic("reconcile must never close a position")
}
func (f *fake) Positions(context.Context) ([]broker.Position, error) { return f.positions, nil }
func (f *fake) Account(context.Context) (broker.Account, error) {
	return broker.Account{Currency: "USD", Cash: 97800, Equity: 99990}, nil
}
func (f *fake) PreviewOpen(context.Context, broker.OpenRequest) (broker.Costs, error) {
	return broker.Costs{}, nil
}
func (f *fake) ClosedPositions(context.Context, time.Time) ([]broker.ClosedPosition, error) {
	return f.closed, nil
}

var (
	t0   = time.Date(2026, 10, 12, 13, 35, 0, 0, time.UTC)
	nsdq = etoroledger.Instrument{Symbol: "NSDQ100", ID: 28}
	spx  = etoroledger.Instrument{Symbol: "SPX500", ID: 27}
)

func open(book string, in etoroledger.Instrument, bar, pid string, units float64) etoroledger.Record {
	return etoroledger.Record{Mode: etoroledger.ModeDemo, Book: book, Instrument: in, Decision: etoroledger.Decision{BarDate: bar},
		Demo: &etoroledger.Demo{Action: etoroledger.ActionOpen, PositionBefore: etoroledger.Flat(),
			PositionAfter: etoroledger.Position{State: "long", PositionID: pid, Units: units},
			Order:         &etoroledger.Order{Kind: etoroledger.ActionOpen, ClientRef: "sma50-" + book + "-" + bar + "-open", Status: "executed", IntentAt: t0}}}
}

func pos(id string, inst int64, units float64) broker.Position {
	return broker.Position{ID: id, Instrument: broker.Instrument{ID: inst}, Units: units, Fees: 0.25}
}

func has(r report, code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestCleanBothLong(t *testing.T) {
	recs := []etoroledger.Record{open("nsdq100", nsdq, "2026-10-09", "11", 0.0355), open("spx500", spx, "2026-10-09", "22", 0.1407)}
	f := &fake{positions: []broker.Position{pos("11", 28, 0.0355), pos("22", 27, 0.1407)}}
	intents := []etoroledger.Intent{{ClientRef: "sma50-nsdq100-2026-10-09-open"}, {ClientRef: "sma50-spx500-2026-10-09-open"}}
	r, err := reconcile(context.Background(), f, recs, intents, t0)
	if err != nil || !r.Clean || len(r.Findings) != 0 || len(r.Books) != 2 || r.Books[0].FinancingToDate != 0.25 || r.Books[0].OrdersChecked != 1 {
		t.Fatalf("report %+v %v", r, err)
	}
}

func TestDrift(t *testing.T) {
	recs := []etoroledger.Record{open("nsdq100", nsdq, "2026-10-09", "11", 0.0355)}
	cases := []struct {
		name    string
		f       *fake
		intents []etoroledger.Intent
		code    string
	}{
		{"missing", &fake{}, nil, codeMissingPosition},
		{"orphan", &fake{positions: []broker.Position{pos("11", 28, 0.0355), pos("99", 28, 1)}}, nil, codeOrphanPosition},
		{"units", &fake{positions: []broker.Position{pos("11", 28, 0.02)}}, nil, codeUnitsDrift},
		{"unfinished intent", &fake{positions: []broker.Position{pos("11", 28, 0.0355)}},
			[]etoroledger.Intent{{ClientRef: "sma50-nsdq100-2026-10-12-close", Book: "nsdq100", IntentAt: t0}}, codeUnfinishedIntent},
	}
	for _, c := range cases {
		r, err := reconcile(context.Background(), c.f, recs, c.intents, t0)
		if err != nil || r.Clean || !has(r, c.code) {
			t.Errorf("%s: want drift %s, got %+v %v", c.name, c.code, r.Findings, err)
		}
	}
}

func TestClosedAndForeign(t *testing.T) {
	cl := etoroledger.Record{Mode: etoroledger.ModeDemo, Book: "nsdq100", Instrument: nsdq, Decision: etoroledger.Decision{BarDate: "2026-10-12"},
		Demo: &etoroledger.Demo{Action: etoroledger.ActionClose, PositionBefore: etoroledger.Position{State: "long", PositionID: "11"},
			PositionAfter: etoroledger.Flat(),
			Order:         &etoroledger.Order{Kind: etoroledger.ActionClose, ClientRef: "c", Status: "executed", AvgPrice: 30900, IntentAt: t0}}}
	recs := []etoroledger.Record{open("nsdq100", nsdq, "2026-10-09", "11", 0.0355), cl,
		{Mode: etoroledger.ModeDryRun, Book: "spx500", Instrument: spx}} // dry-run lines are ignored
	// A foreign instrument (an ad-hoc spike round trip) only warns.
	f := &fake{positions: []broker.Position{pos("77", 1001, 1)}, closed: []broker.ClosedPosition{{ID: "11", CloseRate: 30900}}}
	r, err := reconcile(context.Background(), f, recs, nil, t0)
	if err != nil || !r.Clean || !has(r, codeForeignPositions) || len(r.Books) != 1 || r.Books[0].LedgerState != "flat" {
		t.Fatalf("closed: %+v %v", r, err)
	}
	f.closed = nil
	if r, _ := reconcile(context.Background(), f, recs, nil, t0); r.Clean || !has(r, codeCloseNotInHist) {
		t.Fatalf("close missing from history: %+v", r.Findings)
	}
	f.closed = []broker.ClosedPosition{{ID: "11", CloseRate: 30800}}
	if r, _ := reconcile(context.Background(), f, recs, nil, t0); !r.Clean || !has(r, codeClosePriceDiff) {
		t.Fatalf("close price differs: %+v", r.Findings)
	}
}
