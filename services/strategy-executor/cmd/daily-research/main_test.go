package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func day(i int) time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i) }

func flatBars(n int, open, close float64) []bar {
	bs := make([]bar, n)
	for i := range bs {
		bs[i] = bar{Date: day(i), Open: open, High: math.Max(open, close), Low: math.Min(open, close), Close: close}
	}
	return bs
}

// A signal raised at day t's close must fill at day t+1's OPEN, never at the
// close that produced it. Opens and closes differ here so the two are
// distinguishable in the result.
func TestSimulate_FillsAtNextOpenWithBothLegCosts(t *testing.T) {
	bs := flatBars(4, 100, 110)               // every open 100, every close 110
	want := []bool{true, false, false, false} // long after day 0 close, flat after day 1 close
	c := costModel{buy: 0.01, sell: 0.02}
	res := simulate(bs, want, 0, 3, c)

	// Buy at day 1 open (100) with 1% fee, sell at day 2 open (100) with 2%.
	units := (1 - 0.01) / 100
	final := units * 100 * (1 - 0.02)
	if got, wantPct := res.ReturnPct, (final-1)*100; math.Abs(got-wantPct) > 1e-9 {
		t.Fatalf("return = %.6f%%, want %.6f%% (fills must be at next open, both legs charged)", got, wantPct)
	}
	if res.RoundTrips != 1 {
		t.Fatalf("round trips = %d, want 1", res.RoundTrips)
	}
}

func TestSimulate_OpenPositionClosedAtFinalCloseWithCost(t *testing.T) {
	bs := flatBars(3, 100, 120)
	res := simulate(bs, signalBuyAndHold(3), 0, 2, costModel{buy: 0, sell: 0.01})
	// Enters day 1 open at 100, closed at the final close 120 less 1%.
	want := (1.0/100*120*(1-0.01) - 1) * 100
	if math.Abs(res.ReturnPct-want) > 1e-9 || res.RoundTrips != 1 {
		t.Fatalf("got %.6f%% / %d trips, want %.6f%% / 1", res.ReturnPct, res.RoundTrips, want)
	}
}

// The window must be able to start mid-series: a decision taken on the day
// before the window fills at the window's first open.
func TestSimulate_WindowUsesPriorDayDecision(t *testing.T) {
	bs := flatBars(5, 100, 100)
	bs[2].Open = 50 // first bar of the window
	want := []bool{false, true, true, true, true}
	res := simulate(bs, want, 2, 4, costModel{})
	if math.Abs(res.ReturnPct-100) > 1e-9 { // bought at 50, marked out at 100
		t.Fatalf("return = %.4f%%, want 100%% (entry must be at the window's first open)", res.ReturnPct)
	}
}

func TestSignalTrend_RestartsAfterLongGap(t *testing.T) {
	var bs []bar
	for i := 0; i < 5; i++ {
		bs = append(bs, bar{Date: day(i), Close: 100})
	}
	for i := 0; i < 5; i++ { // 200 days later, much higher prices
		bs = append(bs, bar{Date: day(205 + i), Close: 200})
	}
	w := signalTrend(bs, 3)
	// Without the reset, day 5 would compare 200 against SMA(100,100,200)
	// and read as an uptrend from stale pre-gap data.
	if w[5] || w[6] {
		t.Fatal("trend must not be live until SMA has re-warmed on post-gap bars")
	}
	if w[7] { // SMA(200,200,200) == 200, not strictly above
		t.Fatal("close equal to SMA is not an uptrend")
	}
}

func TestRandomWant_MatchesTripCount(t *testing.T) {
	bs := flatBars(120, 100, 100)
	for trips := 1; trips <= 10; trips++ {
		for s := int64(0); s < 20; s++ {
			w := randomWant(newRand(s), len(bs), 10, 119, trips)
			if got := simulate(bs, w, 10, 119, costModel{}).RoundTrips; got != trips {
				t.Fatalf("trips=%d seed=%d: random strategy made %d round trips", trips, s, got)
			}
		}
	}
}

func TestLoadNews_DedupesAndFilters(t *testing.T) {
	dir := t.TempDir()
	csv := "id,datetime,llm_ticker,llm_overall_sentiment,llm_confidence,llm_signal\n" +
		"1,2025-01-01T10:00:00.000Z,BTC,0.8,0.5,bullish\n" +
		"1,2025-01-01T10:00:00.000Z,BTC,0.8,0.5,bullish\n" + // duplicate id
		"2,2025-01-01T11:00:00.000Z,XRP,-1,1,bearish\n" + // other ticker
		"3,2025-01-01T12:00:00.000Z,BTC-USD,None,0.9,neutral\n" + // unscored
		"4,2025-01-02T00:30:00.000Z,BTC-USD,-0.4,1,bearish\n"
	if err := os.WriteFile(filepath.Join(dir, "a.csv"), []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	news, err := loadNews(dir, map[string]bool{"BTC": true, "BTC-USD": true})
	if err != nil {
		t.Fatal(err)
	}
	d1, d2 := news["2025-01-01"], news["2025-01-02"]
	if d1.N != 1 || math.Abs(d1.score()-0.4) > 1e-12 {
		t.Fatalf("day1 = %+v (score %.3f), want 1 item scoring 0.4", d1, d1.score())
	}
	if d2.N != 1 || math.Abs(d2.score()+0.4) > 1e-12 {
		t.Fatalf("day2 = %+v, want 1 item scoring -0.4", d2)
	}
}
