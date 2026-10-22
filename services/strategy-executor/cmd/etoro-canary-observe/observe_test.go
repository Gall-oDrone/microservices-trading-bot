package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
)

// fakeVenue answers the five read calls from fixtures; it has no way to
// send anything, like the venue interface itself.
type fakeVenue struct {
	portfolio *etoro.Portfolio
	history   []etoro.ClosedTrade
	costs     map[int64]etoro.CostPreview
	rates     []etoro.Rate
	elig      []etoro.Eligibility
	portErr   error
	histErr   error
	costsErr  error
	costCalls []etoro.OrderRequest
}

func (f *fakeVenue) GetPortfolio(context.Context) (*etoro.Portfolio, error) {
	return f.portfolio, f.portErr
}
func (f *fakeVenue) TradingHistory(_ context.Context, since time.Time, page, pageSize int) ([]etoro.ClosedTrade, error) {
	if f.histErr != nil {
		return nil, f.histErr
	}
	if page > 1 {
		return nil, nil
	}
	return f.history, nil
}
func (f *fakeVenue) Costs(_ context.Context, req etoro.OrderRequest) (etoro.CostPreview, error) {
	f.costCalls = append(f.costCalls, req)
	if f.costsErr != nil {
		return etoro.CostPreview{}, f.costsErr
	}
	// As etoro.Client.Costs does: a leveraged preview without a stop never
	// reaches the venue.
	if err := req.Validate(); err != nil {
		return etoro.CostPreview{}, err
	}
	return f.costs[req.InstrumentID], nil
}
func (f *fakeVenue) GetRates(context.Context, ...int64) ([]etoro.Rate, error) { return f.rates, nil }
func (f *fakeVenue) Eligibility(context.Context, []int64) ([]etoro.Eligibility, error) {
	return f.elig, nil
}

func canaryPortfolio(fees float64) *etoro.Portfolio {
	return &etoro.Portfolio{Credit: 10.2, UnrealizedPnL: -1.3, Positions: []etoro.Position{{
		PositionID: 4100000001, InstrumentID: 28, IsBuy: true, Leverage: 20, Amount: 50, Units: 0.04, OpenRate: 25000,
		OpenDateTime: "2026-11-03T15:05:00.000Z", StopLossRate: 24387.5, TakeProfitRate: 0.01, IsNoTakeProfit: true, TotalFees: fees,
		UnrealizedPnL: etoro.PositionPnL{PnL: -1.3, ExposureInAccountCurrency: 998.7, CloseRate: 24967.5},
	}}}
}

func canaryLedger() []etoroledger.Record {
	intent := time.Date(2026, 11, 3, 15, 4, 50, 0, time.UTC)
	return []etoroledger.Record{{Mode: etoroledger.ModeReal, Book: "nsdq100", Instrument: etoroledger.Instrument{Symbol: "NSDQ100", ID: 28},
		Decision: etoroledger.Decision{BarDate: "2026-11-03", FillDate: "2026-11-03"}, Source: etoroledger.SourceUI,
		Demo: &etoroledger.Demo{Env: "real", Action: etoroledger.ActionOpen, PositionBefore: etoroledger.Flat(),
			PositionAfter: etoroledger.Position{State: "long", PositionID: "4100000001", Units: 0.04, Amount: 50},
			Order:         &etoroledger.Order{Kind: etoroledger.ActionOpen, ClientRef: "ui-real-nsdq100-2026-11-03-open-abcdef", IntentAt: intent, PositionIDs: []string{"4100000001"}}}}}
}

func costs(overnight, weekend float64) etoro.CostPreview {
	return etoro.CostPreview{InstrumentID: 28, Costs: []etoro.Cost{{Type: etoro.CostMarketSpread, Value: 0.12}, {Type: etoro.CostOvernightFee, Value: overnight}, {Type: etoro.CostOverWeekendFee, Value: weekend}}}
}

func TestObserveSeriesAndDeltas(t *testing.T) {
	ny := mktcal.NewYork
	day1 := time.Date(2026, 11, 3, 18, 0, 0, 0, ny) // day 0, 18:00 NY: one night crossed
	v := &fakeVenue{portfolio: canaryPortfolio(0), costs: map[int64]etoro.CostPreview{28: costs(-0.22, -0.66)},
		rates: []etoro.Rate{{InstrumentID: 28, Bid: 24999, Ask: 25001}}}
	base := input{recs: canaryLedger(), env: "real", ledger: "etoro-real-canary", sinceDays: 90, previewInstrument: 28, previewAmount: 50, previewLeverage: 20}

	in := base
	in.now = day1
	first := observe(context.Background(), v, in)
	if len(first.Errors) != 0 || first.NYDate != "2026-11-03" || first.Env != "real" || first.PreviousAt != "" || first.Schema != etoroledger.ObservationSchema {
		t.Fatalf("first %+v", first)
	}
	if len(first.Positions) != 1 {
		t.Fatalf("positions %+v", first.Positions)
	}
	p := first.Positions[0]
	if p.PositionID != "4100000001" || p.Symbol != "NSDQ100" || p.Leverage != 20 || p.Amount != 50 || p.FeesToDate != 0 || p.FeesDelta != nil || p.Nights != 1 ||
		!p.InLedger || p.StopLossRate != 24387.5 || p.TakeProfitRate != 0 || p.Exposure != 998.7 || p.Mark != 24967.5 || p.Unrealized != -1.3 ||
		p.Preview == nil || p.Preview.OvernightFee != -0.22 || p.Preview.WeekendFee != -0.66 || p.Preview.Spread != 0.12 {
		t.Fatalf("position %+v preview %+v", p, p.Preview)
	}
	if first.Account.Cash != 10.2 || first.Account.Exposure != 998.7 || first.Account.Equity < 58.8 || first.Account.Equity > 59.0 {
		t.Fatalf("account %+v", first.Account)
	}
	// The preview was asked at the held size and leverage, with the held
	// stop (eToro's costs route needs one at leverage > 1).
	if len(v.costCalls) != 1 || v.costCalls[0].Leverage != 20 || *v.costCalls[0].Amount != 50 || v.costCalls[0].InstrumentID != 28 ||
		v.costCalls[0].StopLossRate == nil || *v.costCalls[0].StopLossRate != 24387.5 {
		t.Fatalf("cost calls %+v", v.costCalls)
	}
	if len(first.HistoryNew) != 0 || len(first.HistoryIDs) != 0 {
		t.Fatalf("history on day 0 %+v", first)
	}

	// Day 1: one more night, fees moved by -0.23.
	v.portfolio = canaryPortfolio(-0.23)
	in.now, in.prev = day1.AddDate(0, 0, 1), &first
	second := observe(context.Background(), v, in)
	if second.PreviousAt != first.At || len(second.Errors) != 0 {
		t.Fatalf("second %+v", second)
	}
	p = second.Positions[0]
	if p.Nights != 2 || p.FeesDelta == nil || *p.FeesDelta < -0.2301 || *p.FeesDelta > -0.2299 || p.FeesToDate != -0.23 {
		t.Fatalf("day-1 position %+v delta %v", p, p.FeesDelta)
	}
	if len(second.Notes) != 0 {
		t.Fatalf("notes %v", second.Notes)
	}
	// Fees that move without a new night are noted (a re-read within the
	// same day, or a venue adjustment).
	v.portfolio = canaryPortfolio(-0.30)
	in.now, in.prev = in.now.Add(time.Hour), &second
	same := observe(context.Background(), v, in)
	if len(same.Notes) != 1 || !strings.Contains(same.Notes[0], "without a new night") {
		t.Fatalf("same-night notes %v", same.Notes)
	}

	// Stopped out: the position is gone, the history lists it four hours
	// later; the first observation after that carries the row once.
	v.portfolio = &etoro.Portfolio{Credit: 35.9}
	in.now, in.prev = day1.AddDate(0, 0, 2), &second
	gone := observe(context.Background(), v, in)
	if len(gone.Positions) != 0 || len(gone.Notes) != 2 || !strings.Contains(gone.Notes[1], "is gone") || len(gone.HistoryNew) != 0 {
		t.Fatalf("gone %+v", gone)
	}
	// With nothing held the planned order is previewed instead (the first
	// note, sorted), its stop 2.45 % below the ask of one rate read.
	last := v.costCalls[len(v.costCalls)-1]
	if !strings.Contains(gone.Notes[0], "no position held; preview") || last.Leverage != 20 || *last.Amount != 50 ||
		last.StopLossRate == nil || *last.StopLossRate != 24388.48 {
		t.Fatalf("empty-account preview %+v", last)
	}
	v.history = []etoro.ClosedTrade{{PositionID: 4100000001, InstrumentID: 28, Leverage: 20, Units: 0.04, OpenRate: 25000, CloseRate: 24380.1,
		OpenTimestamp: "2026-11-03T15:05:00.000Z", CloseTimestamp: "2026-11-05T08:12:33.000Z", NetProfit: -24.80, Fees: -0.46, StopLossRate: 24387.5}}
	in.now, in.prev = day1.AddDate(0, 0, 3), &gone
	listed := observe(context.Background(), v, in)
	if len(listed.HistoryNew) != 1 || len(listed.HistoryIDs) != 1 || listed.HistoryIDs[0] != "4100000001" {
		t.Fatalf("listed %+v", listed)
	}
	h := listed.HistoryNew[0]
	if h.PositionID != "4100000001" || h.CloseRate != 24380.1 || h.NetProfit != -24.80 || h.Fees != -0.46 || h.StopLossRate != 24387.5 ||
		h.ClosedAt != "2026-11-05T08:12:33Z" || h.OpenedAt != "2026-11-03T15:05:00Z" || h.SeenAt != listed.At {
		t.Fatalf("history row %+v", h)
	}
	// The next observation does not list it again.
	in.now, in.prev = in.now.AddDate(0, 0, 1), &listed
	again := observe(context.Background(), v, in)
	if len(again.HistoryNew) != 0 || len(again.HistoryIDs) != 1 {
		t.Fatalf("again %+v", again)
	}
}

func TestObserveKeepsGoingOnErrors(t *testing.T) {
	v := &fakeVenue{portErr: errors.New("502"), histErr: errors.New("429"), costs: map[int64]etoro.CostPreview{}}
	prev := etoroledger.Observation{At: "2026-11-04T23:00:00Z", HistoryIDs: []string{"1"}}
	o := observe(context.Background(), v, input{now: time.Date(2026, 11, 5, 23, 0, 0, 0, time.UTC), env: "real", ledger: "etoro-real-canary", prev: &prev, sinceDays: 90})
	if len(o.Errors) != 2 || !strings.Contains(o.Errors[0], "portfolio") || !strings.Contains(o.Errors[1], "history") {
		t.Fatalf("errors %v", o.Errors)
	}
	// The known history ids survive a failed read.
	if len(o.HistoryIDs) != 1 || o.HistoryIDs[0] != "1" || o.PreviousAt != prev.At || len(o.Positions) != 0 {
		t.Fatalf("observation %+v", o)
	}
	// A failed cost preview is an error, the position is still recorded.
	v2 := &fakeVenue{portfolio: canaryPortfolio(0), costsErr: errors.New("boom")}
	o2 := observe(context.Background(), v2, input{now: time.Now(), env: "real", ledger: "etoro-real-canary", sinceDays: 90})
	if len(o2.Positions) != 1 || o2.Positions[0].Preview != nil || len(o2.Errors) != 1 || !strings.Contains(o2.Errors[0], "costs for position") {
		t.Fatalf("o2 %+v", o2)
	}
}

func TestLedgerHelpers(t *testing.T) {
	recs := canaryLedger()
	if !ledgerPositions(recs)["4100000001"] || ledgerPositions(recs)["x"] {
		t.Fatal("ledgerPositions")
	}
	if ledgerSymbols(recs)[28] != "NSDQ100" || ledgerSymbols(nil)[27] != "SPX500" {
		t.Fatal("ledgerSymbols")
	}
	if t0, ok := firstOpen(recs); !ok || t0 != time.Date(2026, 11, 3, 15, 4, 50, 0, time.UTC) {
		t.Fatalf("firstOpen %v %v", t0, ok)
	}
	if _, ok := firstOpen(nil); ok {
		t.Fatal("firstOpen on an empty ledger")
	}
	if parseTime("2026-11-05T08:12:33.000Z").IsZero() || parseTime("2026-11-05T08:12:33.1234567").IsZero() || !parseTime("garbage").IsZero() {
		t.Fatal("parseTime")
	}
}

func TestCaptureDay0WritesTheFourFilesAndSums(t *testing.T) {
	v := &fakeVenue{portfolio: &etoro.Portfolio{Credit: 60}, costs: map[int64]etoro.CostPreview{28: costs(-0.22, -0.66)},
		rates: []etoro.Rate{{InstrumentID: 28, Bid: 24999, Ask: 25001}}, elig: []etoro.Eligibility{{InstrumentID: 28, Symbol: "NSDQ100", MinPositionExposure: 1000}}}
	dir := filepath.Join(t.TempDir(), "evidence")
	if errs := captureDay0(context.Background(), v, dir, 28, 50, 20, true); len(errs) != 0 {
		t.Fatalf("dry: %v", errs)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("dry wrote the directory")
	}
	if errs := captureDay0(context.Background(), v, dir, 28, 50, 20, false); len(errs) != 0 {
		t.Fatalf("capture: %v", errs)
	}
	for _, n := range []string{"eligibility.json", "costs.json", "rates.json", "account.json", "SHA256SUMS"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, "costs.json"))
	if !strings.Contains(string(b), `"exposure_usd": 1000`) || !strings.Contains(string(b), `"leverage": 20`) {
		t.Fatalf("costs.json %s", b)
	}
	sums, _ := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if strings.Count(string(sums), "\n") != 4 {
		t.Fatalf("sums %s", sums)
	}
}

func TestPreviewRequestStop(t *testing.T) {
	// x1: no stop, as before.
	if r := previewRequest(3006, 1100, 1, 0, 500); r.StopLossRate != nil || r.Leverage != 1 {
		t.Fatalf("x1 %+v", r)
	}
	// Held stop wins over any reference rate.
	if r := previewRequest(28, 50, 20, 24387.5, 24967.5, 25000); r.StopLossRate == nil || *r.StopLossRate != 24387.5 {
		t.Fatalf("held %+v", r)
	}
	// No held stop: 2.45 % below the first positive reference (the mark here).
	if r := previewRequest(28, 50, 20, 0, 0, 25000); r.StopLossRate == nil || *r.StopLossRate != 24387.5 {
		t.Fatalf("ref %+v", r)
	}
	// No reference at all: no stop, and Validate refuses it locally.
	if r := previewRequest(28, 50, 20, 0, 0, 0); r.StopLossRate != nil || r.Validate() == nil {
		t.Fatalf("none %+v", r)
	}
}

func TestCandlePathFollowsTheExecutorsNaming(t *testing.T) {
	rows := []bitsodaily.Row{{Date: "2026-10-20"}, {Date: "2026-10-21"}, {Date: "2026-10-22"}}
	p, err := candlePath("/x/real-canary/candles", "nsdq100", rows)
	if err != nil || p != "/x/real-canary/candles/nsdq100_daily_2026-10-22.csv" {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := candlePath("/x", "nsdq100", nil); err == nil {
		t.Fatal("no bars must be an error, not an empty file")
	}
	if _, ok := candleBooks[28]; !ok || candleBooks[28].Symbol != "NSDQ100" {
		t.Fatalf("books %+v", candleBooks)
	}
}
