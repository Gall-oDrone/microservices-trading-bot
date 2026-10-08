package varmodel

import (
	"math"
	"testing"
	"time"
)

var d0 = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// closesFromReturns builds daily closes from log returns, starting at 100.
func closesFromReturns(rs []float64) []Close {
	out := []Close{{Date: d0, Price: 100}}
	p := 100.0
	for i, r := range rs {
		p *= math.Exp(r)
		out = append(out, Close{Date: d0.AddDate(0, 0, i+1), Price: p})
	}
	return out
}

func alternating(n int, a float64) []float64 {
	rs := make([]float64, n)
	for i := range rs {
		rs[i] = a
		if i%2 == 1 {
			rs[i] = -a
		}
	}
	return rs
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestLogReturnsBreakOnGaps(t *testing.T) {
	cs := []Close{
		{d0, 100}, {d0.AddDate(0, 0, 1), 110}, // 1 return
		{d0.AddDate(0, 0, 3), 121}, // gap: no return
		{d0.AddDate(0, 0, 4), 0},   // bad price: no return either side
		{d0.AddDate(0, 0, 5), 130},
		{d0.AddDate(0, 0, 6), 143}, // 1 return
	}
	rs := LogReturns(cs)
	if len(rs) != 2 || !near(rs[0].R, math.Log(1.1), 1e-15) || !rs[1].Date.Equal(d0.AddDate(0, 0, 6)) {
		t.Fatalf("returns %+v", rs)
	}
}

func TestForecastsConstantVol(t *testing.T) {
	f := Forecasts(LogReturns(closesFromReturns(alternating(400, 0.03))), Lambda)
	last := f[len(f)-1]
	if !last.Ready || !near(last.EWMA, 0.03, 1e-12) || !near(last.Long, 0.03, 1e-12) || !near(last.Vol, 0.03, 1e-12) {
		t.Fatalf("forecast %+v", last)
	}
	if f[MinReturns-2].Ready || !f[MinReturns-1].Ready {
		t.Fatal("ready threshold")
	}
}

// The EWMA recursion, seeded with the mean square of the first 30 returns.
func TestForecastsEWMARecursion(t *testing.T) {
	rs := alternating(SeedReturns, 0.01)
	rs = append(rs, 0.05)
	rs = append(rs, alternating(MinReturns, 0.01)...)
	f := Forecasts(LogReturns(closesFromReturns(rs)), Lambda)
	v := 0.0001
	v = Lambda*v + (1-Lambda)*0.0025
	for range alternating(MinReturns, 0.01) {
		v = Lambda*v + (1-Lambda)*0.0001
	}
	if got := f[len(f)-1].EWMA; !near(got, math.Sqrt(v), 1e-12) {
		t.Fatalf("ewma %v, want %v", got, math.Sqrt(v))
	}
}

// A calm month after a volatile year: EWMA drops, the long window holds VaR.
func TestVolIsMaxOfEWMAAndLong(t *testing.T) {
	rs := append(alternating(335, 0.05), alternating(30, 0.005)...)
	e, err := EstimateFromCloses(closesFromReturns(rs), Lambda)
	if err != nil {
		t.Fatal(err)
	}
	if !(e.EWMA < e.Long) || e.Vol != e.Long || e.Returns != 365 || len(e.HistReturns) != 365 {
		t.Fatalf("estimate %+v", e.Forecast)
	}
	// A shock lifts EWMA above the long window at once.
	rs = append(alternating(400, 0.01), 0.2)
	e, _ = EstimateFromCloses(closesFromReturns(rs), Lambda)
	if !(e.EWMA > e.Long) || e.Vol != e.EWMA {
		t.Fatalf("after shock %+v", e.Forecast)
	}
}

func TestInsufficient(t *testing.T) {
	if _, err := EstimateFromCloses(closesFromReturns(alternating(MinReturns-1, 0.01)), Lambda); err != ErrInsufficient {
		t.Fatalf("err %v", err)
	}
}

func TestBacktestZones(t *testing.T) {
	// Constant vol, no tail days: green with zero exceptions.
	e, _ := EstimateFromCloses(closesFromReturns(alternating(700, 0.02)), Lambda)
	b := e.Backtest
	if b.Observations != BacktestWindow || b.ExceptionsLong != 0 || b.ExceptionsShort != 0 || b.Zone != ZoneGreen || b.KupiecLong >= 0.2 {
		t.Fatalf("calm backtest %+v", b)
	}

	// Twelve -10 % days, 20 days apart, inside the last 250: every one breaks
	// a 99 % VaR of about 6-7 %, so the long side is red; shorts never lose.
	rs := alternating(700, 0.02)
	for k := 0; k < 12; k++ {
		rs[len(rs)-245+20*k] = -0.10
	}
	e, _ = EstimateFromCloses(closesFromReturns(rs), Lambda)
	b = e.Backtest
	if b.ExceptionsLong != 12 || b.ExceptionsShort != 0 || b.Zone != ZoneRed || b.KupiecLong > 1e-4 {
		t.Fatalf("shocked backtest %+v", b)
	}

	// Too little history for 250 observations.
	e, _ = EstimateFromCloses(closesFromReturns(alternating(200, 0.02)), Lambda)
	if e.Backtest.Zone != ZoneInsufficient || e.Backtest.Observations >= BacktestWindow {
		t.Fatalf("short backtest %+v", e.Backtest)
	}
}

func TestBaselZoneBoundaries(t *testing.T) {
	for x, want := range map[int]Zone{0: ZoneGreen, 4: ZoneGreen, 5: ZoneYellow, 9: ZoneYellow, 10: ZoneRed, 30: ZoneRed} {
		if got := BaselZone(250, x); got != want {
			t.Errorf("%d exceptions: %v, want %v", x, got, want)
		}
	}
	if BaselZone(249, 0) != ZoneInsufficient {
		t.Error("249 observations")
	}
}

func TestKupiecPOF(t *testing.T) {
	// 10 exceptions in 250 at 1 %: LR = 12.955..., p = 3.19e-4.
	if p := KupiecPOF(250, 10, 0.01); !near(p, 3.19e-4, 0.05e-4) {
		t.Fatalf("p = %v", p)
	}
	// Exactly the expected rate: LR = 0, p = 1.
	if p := KupiecPOF(300, 3, 0.01); !near(p, 1, 1e-12) {
		t.Fatalf("p = %v", p)
	}
	// Zero exceptions in 250: LR = 5.025, p = 0.025.
	if p := KupiecPOF(250, 0, 0.01); !near(p, 0.0250, 0.0005) {
		t.Fatalf("p = %v", p)
	}
	if KupiecPOF(0, 0, 0.01) != 1 {
		t.Fatal("n = 0")
	}
}

func TestHistoricalVaR(t *testing.T) {
	pnl := make([]float64, 365)
	for i := range pnl {
		pnl[i] = -float64(i + 1) // losses 1..365
	}
	// k = ceil(365 x 0.01) = 4: the 4th worst loss is 362.
	if v, ok := HistoricalVaR(pnl, 0.99, 250); !ok || v != 362 {
		t.Fatalf("var %v %v", v, ok)
	}
	if _, ok := HistoricalVaR(pnl[:100], 0.99, 250); ok {
		t.Fatal("too few scenarios")
	}
	if v, _ := HistoricalVaR([]float64{1, 2, 3}, 0.99, 1); v != 0 {
		t.Fatalf("all gains: %v", v)
	}
}
