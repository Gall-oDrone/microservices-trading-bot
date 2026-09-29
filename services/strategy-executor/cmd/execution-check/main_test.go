package main

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC)

func at(min int, price, amt float64, taker string) print {
	return print{TS: t0.Add(time.Duration(min) * time.Minute), Price: price, Amount: amt, Taker: taker}
}

func TestBarFor_RebuildsWindowOnly(t *testing.T) {
	ps := []print{
		{TS: t0.Add(-time.Minute), Price: 1, Amount: 9, Taker: "buy"}, // previous day
		at(0, 100, 1, "buy"), at(10, 110, 2, "sell"), at(20, 90, 1, "buy"), at(30, 105, 1, "sell"),
		{TS: t0.Add(day), Price: 999, Amount: 9, Taker: "buy"}, // next day
	}
	b, ok := barFor(ps, t0)
	if !ok || b.Open != 100 || b.High != 110 || b.Low != 90 || b.Close != 105 || b.Volume != 5 || b.N != 4 {
		t.Fatalf("bar = %+v", b)
	}
	// longest trade-free stretch is from the last print to the end of the day
	if want := day - 30*time.Minute; b.MaxGap != want {
		t.Fatalf("max gap %v, want %v", b.MaxGap, want)
	}
}

// A resting buy at 100 is only guaranteed filled when a taker SELL prints
// BELOW 100. A print at exactly 100 is a touch (depends on queue position);
// a taker BUY never fills a resting buy.
func TestMakerFilled_TradeThroughVsTouch(t *testing.T) {
	ps := []print{at(1, 99, 1, "buy"), at(2, 100, 1, "sell"), at(30, 99.5, 1, "sell")}
	if makerFilled(ps, t0, t0.Add(10*time.Minute), 100, "buy", true) {
		t.Fatal("touch at 100 must not count as trade-through")
	}
	if !makerFilled(ps, t0, t0.Add(10*time.Minute), 100, "buy", false) {
		t.Fatal("touch at 100 should count as touch")
	}
	if !makerFilled(ps, t0, t0.Add(time.Hour), 100, "buy", true) {
		t.Fatal("sell print at 99.5 trades through a 100 bid")
	}
	if makerFilled(ps, t0, t0.Add(time.Hour), 100, "sell", true) {
		t.Fatal("no taker buy above 100: resting sell must stay unfilled")
	}
}

func TestMakerThenChase_CostsFeePlusAdverseMove(t *testing.T) {
	// Price runs away upward: the resting buy never fills, chase at 101 = +100 bps.
	up := []print{at(1, 100.5, 1, "buy"), at(59, 101, 1, "buy"), at(61, 90, 1, "sell")}
	cost, filled := makerThenChase(up, t0, time.Hour, 100, "buy", 60, 78)
	if filled || cost < 177.9 || cost > 178.1 {
		t.Fatalf("buy chase: cost=%v filled=%v, want 178 unfilled", cost, filled)
	}
	// Same path is favourable for a resting sell: filled as maker at the fee.
	cost, filled = makerThenChase(up, t0, time.Hour, 100, "sell", 60, 78)
	if !filled || cost != 60 {
		t.Fatalf("sell: cost=%v filled=%v, want 60 filled", cost, filled)
	}
}

func TestVWAP_TakerSideAndWindow(t *testing.T) {
	ps := []print{at(0, 100, 1, "buy"), at(1, 102, 3, "buy"), at(2, 50, 5, "sell"), at(6, 200, 9, "buy")}
	v, ok := vwap(ps, t0, t0.Add(5*time.Minute), "buy")
	if !ok || v != 101.5 {
		t.Fatalf("vwap = %v", v)
	}
}

func TestFullDays_OnlyCompleteDays(t *testing.T) {
	cs := []candle{{Date: "a", Start: t0}, {Date: "b", Start: t0.Add(day)}, {Date: "c", Start: t0.Add(2 * day)}}
	got := fullDays(cs, t0.Add(time.Minute), t0.Add(3*day))
	if len(got) != 2 || got[0].Date != "b" || got[1].Date != "c" {
		t.Fatalf("got %+v", got)
	}
}
