package varmodel

import (
	"math"
	"testing"
	"time"
)

func TestZES(t *testing.T) {
	// phi(1.959964) / 0.025 = 2.33780: ES 97.5 % is about VaR 99 % for a
	// normal distribution, the calibration FRTB chose.
	if math.Abs(ZES-2.337803) > 1e-5 {
		t.Fatalf("ZES %v", ZES)
	}
}

func TestExpectedShortfall(t *testing.T) {
	pnl := make([]float64, 100)
	for i := range pnl {
		pnl[i] = -float64(i + 1) // losses 1..100
	}
	// k = ceil(100 x 2.5 %) = 3: mean of 100, 99, 98.
	if es, ok := ExpectedShortfall(pnl, ESConfidence, 100); !ok || es != 99 {
		t.Fatalf("es %v %v", es, ok)
	}
	if _, ok := ExpectedShortfall(pnl, ESConfidence, 101); ok {
		t.Fatal("ES from too few scenarios")
	}
	// ES is never below the VaR at the same level.
	v, _ := HistoricalVaR(pnl, ESConfidence, 1)
	if es, _ := ExpectedShortfall(pnl, ESConfidence, 1); es < v {
		t.Fatalf("es %v < var %v", es, v)
	}
	// Only gains: no loss.
	if es, ok := ExpectedShortfall([]float64{1, 2, 3}, ESConfidence, 1); !ok || es != 0 {
		t.Fatalf("gains-only es %v", es)
	}
}

var ep = Episode{ID: "test", Name: "test", Start: date("2022-06-10"), End: date("2022-06-14")}

func TestEpisodePath(t *testing.T) {
	cs := []Close{
		{date("2022-06-08"), 50},  // before the base: ignored
		{date("2022-06-09"), 100}, // base
		{date("2022-06-10"), 90},
		{date("2022-06-12"), 70}, // 06-11 missing: cumulative from base still holds
		{date("2022-06-14"), 80},
		{date("2022-06-15"), 10}, // after the window: ignored
	}
	p, ok := EpisodePath(cs, ep)
	if !ok || len(p) != 3 {
		t.Fatalf("path %+v %v", p, ok)
	}
	if math.Abs(p[1].Cum+0.30) > 1e-12 || math.Abs(Trough(p)+0.30) > 1e-12 || math.Abs(p[2].Cum+0.20) > 1e-12 {
		t.Fatalf("path %+v", p)
	}
	if _, ok := EpisodePath(cs[2:], ep); ok {
		t.Fatal("path without the base close")
	}
	if _, ok := EpisodePath(cs[:2], ep); ok {
		t.Fatal("path without closes in the window")
	}
}

func TestEpisodeLoss(t *testing.T) {
	d := func(s string) time.Time { return date(s) }
	paths := map[string][]PathPoint{
		"btc_mxn": {{d("2022-06-10"), -0.10}, {d("2022-06-11"), -0.40}, {d("2022-06-12"), -0.30}},
		"eth_mxn": {{d("2022-06-10"), -0.20}, {d("2022-06-12"), -0.50}}, // no 06-11
	}
	// Long both: 06-11 is not common, so the worst common day is 06-12:
	// 100k x 30 % + 50k x 50 % = 55k.
	loss, ok := EpisodeLoss(map[string]float64{"btc_mxn": 100_000, "eth_mxn": 50_000}, paths)
	if !ok || math.Abs(loss-55_000) > 1e-6 {
		t.Fatalf("loss %v %v", loss, ok)
	}
	// One book alone uses all its days: worst is 06-11, 40 %.
	if loss, _ := EpisodeLoss(map[string]float64{"btc_mxn": 100_000}, paths); math.Abs(loss-40_000) > 1e-6 {
		t.Fatalf("single-book loss %v", loss)
	}
	// A short gains in a crash: no loss.
	if loss, ok := EpisodeLoss(map[string]float64{"btc_mxn": -100_000}, paths); !ok || loss != 0 {
		t.Fatalf("short loss %v", loss)
	}
	if _, ok := EpisodeLoss(map[string]float64{"xrp_mxn": 1}, paths); ok {
		t.Fatal("loss for a book without a path")
	}
	disjoint := map[string][]PathPoint{"a": {{d("2022-06-10"), -0.1}}, "b": {{d("2022-06-11"), -0.1}}}
	if _, ok := EpisodeLoss(map[string]float64{"a": 1, "b": 1}, disjoint); ok {
		t.Fatal("loss from paths with no common day")
	}
	if loss, ok := EpisodeLoss(nil, paths); !ok || loss != 0 {
		t.Fatal("flat portfolio")
	}
}

func TestShockLoss(t *testing.T) {
	if l := ShockLoss(100_000, -0.3); math.Abs(l-30_000) > 1e-9 {
		t.Fatal(l)
	}
	if l := ShockLoss(100_000, 0.2); l != 0 {
		t.Fatal("a long gains on a rally", l)
	}
	if l := ShockLoss(-100_000, 0.2); math.Abs(l-20_000) > 1e-9 {
		t.Fatal("a short loses on a rally", l)
	}
}

func TestEpisodesWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range Episodes {
		if e.ID == "" || seen[e.ID] || !e.Start.Before(e.End) || e.End.Sub(e.Start) > 60*24*time.Hour {
			t.Errorf("episode %+v", e)
		}
		seen[e.ID] = true
	}
}
