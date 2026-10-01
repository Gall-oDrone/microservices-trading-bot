package main

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/strategy-executor/internal/dailyrule"
)

func day(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

// series builds consecutive daily bars from closes, ending on lastDate.
// Each open equals the previous close, so opens and closes differ.
func series(lastDate string, closes ...float64) []dailyrule.Bar {
	end := day(lastDate)
	bs := make([]dailyrule.Bar, len(closes))
	for i, c := range closes {
		o := c
		if i > 0 {
			o = closes[i-1]
		}
		bs[i] = dailyrule.Bar{Date: end.AddDate(0, 0, i-len(closes)+1), Open: o, High: math.Max(o, c), Low: math.Min(o, c), Close: c}
	}
	return bs
}

func flat(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// 00:05 Mexico City on 2026-10-01 is 06:05 UTC; yesterday's bar has closed.
var runTime = time.Date(2026, 10, 1, 6, 5, 0, 0, time.UTC)

func TestDecide_BuyOnCrossAbove(t *testing.T) {
	closes := append(flat(50, 100), 110) // day 50 closes above SMA(50) for the first time
	d, _, err := decide(series("2026-09-30", closes...), runTime)
	if err != nil {
		t.Fatal(err)
	}
	if d.BarDate != "2026-09-30" || d.FillDate != "2026-10-01" {
		t.Fatalf("dates: %+v", d)
	}
	if d.Signal != "long" || d.PrevSignal != "flat" || d.Action != "buy" {
		t.Fatalf("want buy on cross: %+v", d)
	}
	if want := (49*100.0 + 110) / 50; math.Abs(d.SMA-want) > 1e-9 {
		t.Fatalf("sma=%v want %v", d.SMA, want)
	}
}

func TestDecide_SellAndHold(t *testing.T) {
	up := append(flat(50, 100), 110, 90) // long at 110, flat at 90
	if d, _, _ := decide(series("2026-09-30", up...), runTime); d.Action != "sell" {
		t.Fatalf("want sell, got %+v", d)
	}
	stay := append(flat(50, 100), 110, 120)
	if d, _, _ := decide(series("2026-09-30", stay...), runTime); d.Action != "hold" || d.Signal != "long" {
		t.Fatalf("want hold long, got %+v", d)
	}
}

func TestDecide_RefusesStaleOrColdData(t *testing.T) {
	closes := append(flat(50, 100), 110)
	if _, _, err := decide(series("2026-09-29", closes...), runTime); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("a missing 2026-09-30 bar must be refused, got %v", err)
	}
	// Just before Mexico midnight, 2026-09-30 has not closed: expected last bar is 09-29.
	before := time.Date(2026, 10, 1, 5, 55, 0, 0, time.UTC)
	if _, _, err := decide(series("2026-09-30", closes...), before); err == nil {
		t.Fatal("a bar that closes in the future must not be expected yet")
	}
	if _, _, err := decide(series("2026-09-30", flat(50, 100)...), runTime); err == nil {
		t.Fatal("rule must be warm on the previous bar too")
	}
}

// Same arithmetic as daily-research's simulate(): decision at t-1's close,
// fill at t's open, each leg charged on its own notional.
func TestPaper_MatchesSimulateArithmetic(t *testing.T) {
	// 50 flat days, then: close 110 (long) -> fill next open.
	closes := append(flat(50, 100), 110, 121, 99)
	bars := series("2026-09-30", closes...)
	pts := dailyrule.Evaluate(bars, smaDays)
	// Forward window starts at the 110 bar: the decision before it (flat) holds.
	start := bars[50].Date.Format("2006-01-02")
	p, err := paper(bars, pts, start, 100) // 1% per leg
	if err != nil {
		t.Fatal(err)
	}
	// Day 50 (close 110): flat. Day 51: buy at open 110, close 121.
	// Day 52: SMA over [100 x47, 110, 121, 99] -> close 99 < SMA? no: SMA ~100.6 -> flat,
	// but the position changes only at the NEXT open, so day 52 is still long.
	units := (1 - 0.01) / 110
	wantEq := units * 99
	if p.Days != 3 || p.Fills != 1 || p.Position != "long" {
		t.Fatalf("days/fills/position: %+v", p)
	}
	if math.Abs(p.Equity-wantEq) > 1e-12 {
		t.Fatalf("equity=%v want %v", p.Equity, wantEq)
	}
	if want := wantEq * (1 - 0.01); math.Abs(p.EquityClosed-want) > 1e-12 {
		t.Fatalf("equity_if_closed=%v want %v", p.EquityClosed, want)
	}
	if p.PendingAction != "sell" {
		t.Fatalf("pending=%s want sell (flat at 99 while holding)", p.PendingAction)
	}
	peak := units * 121
	if want := (peak - wantEq) / peak; math.Abs(p.MaxDrawdown-want) > 1e-12 {
		t.Fatalf("maxDD=%v want %v", p.MaxDrawdown, want)
	}
	if want := (1 - 0.01) / 100 * 99; math.Abs(p.HoldEquity-want) > 1e-12 {
		t.Fatalf("hold=%v want %v (bought at the window's first open)", p.HoldEquity, want)
	}
}

func TestPaper_WindowNotStartedAndMissingStart(t *testing.T) {
	bars := series("2026-09-30", append(flat(50, 100), 110)...)
	pts := dailyrule.Evaluate(bars, smaDays)
	p, err := paper(bars, pts, "2026-10-01", 40)
	if err != nil || p.Days != 0 || p.Equity != 1 || p.PendingAction != "buy" {
		t.Fatalf("not-started window: %+v err=%v", p, err)
	}
	holey := append(append([]dailyrule.Bar{}, bars[:45]...), bars[46:]...)
	if _, err := paper(holey, dailyrule.Evaluate(holey, smaDays), bars[45].Date.Format("2006-01-02"), 40); err == nil {
		t.Fatal("a missing forward-start bar must be an error")
	}
}

func TestLedger_AppendReloadAndRejectDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "ledger.jsonl")
	l, err := openLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	r := record{Book: "btc_usd", Decision: decision{BarDate: "2026-09-30", Signal: "long", Close: 1}}
	if err := l.append(r); err != nil {
		t.Fatal(err)
	}
	if err := l.append(r); err == nil {
		t.Fatal("second record for the same book and day must be rejected")
	}
	other := r
	other.Book = "btc_mxn"
	if err := l.append(other); err != nil {
		t.Fatal(err)
	}
	re, err := openLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := re.get("btc_usd", "2026-09-30"); !ok || got.Decision.Signal != "long" || len(re.entries) != 2 {
		t.Fatalf("reload: %+v ok=%v n=%d", got, ok, len(re.entries))
	}
}

func TestFrozenSpecsMatchPreRegistrations(t *testing.T) {
	want := map[string]struct {
		from, start string
		cost        float64
	}{
		"btc_mxn": {"2017-06-01", "2026-09-27", 70},
		"btc_usd": {"2020-04-01", "2026-09-29", 40},
	}
	if len(frozenSpecs) != len(want) {
		t.Fatalf("specs: %v", frozenSpecs)
	}
	for b, w := range want {
		s := frozenSpecs[b]
		if s.HistoryFrom != w.from || s.ForwardStart != w.start || s.LegCostBps != w.cost {
			t.Fatalf("%s spec drifted from its pre-registration: %+v", b, s)
		}
	}
}
