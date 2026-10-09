package execsim

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 1, 6, 15, 0, 0, time.UTC)

func tr(min float64, px, amt float64) Trade {
	return Trade{At: t0.Add(time.Duration(min * float64(time.Minute))), Price: px, Amount: amt}
}

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("%s = %.8f, want %.8f", name, got, want)
	}
}

func TestRun_MarketPaysHalfSpreadImpactAndTakerFee(t *testing.T) {
	trades := []Trade{tr(-1, 100, 1)}
	r, ok := Run(trades, t0, Params{Buy: true, Qty: 1, HalfSpreadBps: 2, TakerFeeBps: 30, MakerFeeBps: 10,
		ImpactBps: func(q float64) float64 { return 3 * q }})
	if !ok {
		t.Fatal("not ok")
	}
	near(t, "price bps", r.PriceBps, (1.0002*1.0003-1)*1e4) // the ask (2 bp half-spread), then 3 bp impact
	near(t, "fee bps", r.FeeBps, 30)
	near(t, "cost", r.CostBps, (1.0002*1.0003-1)*1e4+30)
	near(t, "taker", r.TakerQty, 1)
	r, _ = Run(trades, t0, Params{Buy: false, Qty: 1, HalfSpreadBps: 2, TakerFeeBps: 30})
	near(t, "sell price bps", r.PriceBps, 2)
}

func TestRun_ThroughNeedsATradePastThePriceTouchDoesNot(t *testing.T) {
	// Buy rests at 100*(1-1bp) = 99.99. A print at 99.99 touches; 99.98 goes through.
	trades := []Trade{tr(-1, 100, 1), tr(5, 99.99, 1), tr(10, 100.5, 1)}
	p := Params{Buy: true, Qty: 0.5, Window: time.Hour, HalfSpreadBps: 1, MakerFeeBps: 10, TakerFeeBps: 30}
	p.Model = Touch
	r, _ := Run(trades, t0, p)
	near(t, "touch maker", r.MakerQty, 0.5)
	near(t, "touch cost", r.CostBps, -1+10)
	p.Model = Through
	r, _ = Run(trades, t0, p)
	near(t, "through maker", r.MakerQty, 0)
	// Fallback at the last price before the deadline (100.5) + 1 bp half-spread, taker fee.
	near(t, "through price", r.AvgPrice, 100.5*1.0001)
	near(t, "through fee", r.FeeBps, 30)
	if !r.End.Equal(t0.Add(time.Hour)) {
		t.Fatalf("end %v", r.End)
	}
}

func TestRun_FillsAreCappedByTradedAmount(t *testing.T) {
	trades := []Trade{tr(-1, 100, 1), tr(1, 99, 0.3), tr(70, 100, 1)}
	r, _ := Run(trades, t0, Params{Buy: true, Qty: 1, Window: time.Hour, HalfSpreadBps: 0, MakerFeeBps: 0, TakerFeeBps: 0})
	near(t, "maker", r.MakerQty, 0.3)
	near(t, "taker", r.TakerQty, 0.7)
	if r.MakerShare() < 0.29 || r.MakerShare() > 0.31 {
		t.Fatalf("maker share %v", r.MakerShare())
	}
}

func TestRun_SellSideIsSymmetric(t *testing.T) {
	trades := []Trade{tr(-1, 100, 1), tr(5, 100.02, 1)}
	r, _ := Run(trades, t0, Params{Buy: false, Qty: 1, Window: time.Hour, HalfSpreadBps: 1, Model: Through, MakerFeeBps: 10})
	near(t, "maker", r.MakerQty, 1)
	near(t, "price bps", r.PriceBps, -1) // sold at the ask: 1 bp better than arrival
}

func TestRun_RepricingChasesTheMarket(t *testing.T) {
	// Price runs up; without repricing the bid at 99.99 never fills.
	trades := []Trade{tr(-1, 100, 1), tr(2, 101, 1), tr(7, 100.9, 1), tr(12, 100.8, 1)}
	p := Params{Buy: true, Qty: 1, Window: time.Hour, HalfSpreadBps: 1, Model: Through}
	r, _ := Run(trades, t0, p)
	near(t, "static maker", r.MakerQty, 0)
	p.RepriceEvery = 5 * time.Minute
	r, _ = Run(trades, t0, p)
	// At 5m the bid re-pegs to 101*(1-1bp)=100.9899; the 100.9 print at 7m goes through it.
	near(t, "repriced maker", r.MakerQty, 1)
	near(t, "repriced avg", r.AvgPrice, 101*0.9999)
}

func TestRun_SlicesRestAndFallBackPerChild(t *testing.T) {
	// Two children of 0.5 over an hour; the first fills as maker, the second does not.
	trades := []Trade{tr(-1, 100, 1), tr(10, 99, 1), tr(31, 100, 1), tr(59, 102, 1)}
	r, _ := Run(trades, t0, Params{Buy: true, Qty: 1, Window: time.Hour, Slices: 2, Model: Through})
	near(t, "maker", r.MakerQty, 0.5)
	near(t, "taker", r.TakerQty, 0.5)
	near(t, "avg", r.AvgPrice, (0.5*100+0.5*102)/1)
}

func TestRun_BidAndAskPrintsSetTheQuotes(t *testing.T) {
	// A sell hit the bid at 99.9 (maker buy), then a buy lifted the ask at 100.1.
	bid := Trade{At: t0.Add(-3 * time.Minute), Price: 99.9, Amount: 1, MakerSide: 1}
	ask := Trade{At: t0.Add(-1 * time.Minute), Price: 100.1, Amount: 1, MakerSide: -1}
	trades := []Trade{bid, ask, {At: t0.Add(5 * time.Minute), Price: 99.9, Amount: 1, MakerSide: 1}}
	p := Params{Buy: true, Qty: 1, Window: time.Hour, HalfSpreadBps: 50, QuoteMaxAge: 10 * time.Minute, Model: Touch}
	r, _ := Run(trades, t0, p)
	near(t, "rests on the bid print", r.AvgPrice, 99.9)
	p.Model = Through // the 99.9 print does not go through 99.9: market at the ask print
	r, _ = Run(trades, t0, p)
	near(t, "takes the ask print", r.AvgPrice, 100.1)
	p.QuoteMaxAge = 30 * time.Second // prints too old: last trade ± 50 bps
	r, _ = Run(trades, t0, p)
	near(t, "half-spread fallback", r.AvgPrice, 99.9*1.005)
}

func TestRun_NoArrivalPriceIsNotOK(t *testing.T) {
	if _, ok := Run([]Trade{tr(1, 100, 1)}, t0, Params{Buy: true, Qty: 1}); ok {
		t.Fatal("want !ok without a trade before start")
	}
}

func TestVsBps(t *testing.T) {
	r := Result{AvgPrice: 101, FeeBps: 10}
	near(t, "buy vs open", r.VsBps(100, true), 110)
	near(t, "sell vs open", r.VsBps(100, false), -90)
}

func TestMaxGap(t *testing.T) {
	trades := []Trade{tr(-30, 100, 1), tr(5, 100, 1), tr(40, 100, 1)}
	if g := MaxGap(trades, t0, t0.Add(time.Hour)); g != 35*time.Minute {
		t.Fatalf("gap %v", g)
	}
	if g := MaxGap(nil, t0, t0.Add(time.Hour)); g != time.Hour {
		t.Fatalf("empty gap %v", g)
	}
}

func TestSummarize(t *testing.T) {
	legs := []Leg{
		{Result: Result{MakerQty: 1, CostBps: 10}, VsOpenBps: 0},
		{Result: Result{MakerQty: 0.5, TakerQty: 0.5, CostBps: 20}, VsOpenBps: 10},
		{Result: Result{TakerQty: 1, CostBps: 30}, VsOpenBps: 20},
	}
	s := Summarize(legs)
	near(t, "maker share", s.MakerShare, 0.5)
	near(t, "full maker", s.FullMakerRate, 1.0/3)
	near(t, "mean", s.MeanBps, 20)
	near(t, "median", s.MedianBps, 20)
	near(t, "p90", s.P90Bps, 28)
	near(t, "std", s.StdBps, 10)
	near(t, "vs open", s.MeanVsOpenBps, 10)
	if Summarize(nil).Legs != 0 {
		t.Fatal("empty")
	}
}
