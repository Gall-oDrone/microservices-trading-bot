package cfdrisk

import (
	"math"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/varmodel"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func ymd(s string) time.Time { return date(s) }

// The trading-day chain: Friday -> Monday is one return, a holiday is
// skipped (Wednesday -> Friday over Thanksgiving), a missing trading day
// breaks the chain, and weekend / holiday bars are dropped before pairing.
func TestTradingLogReturns(t *testing.T) {
	cs := []varmodel.Close{
		{Date: ymd("2025-11-20"), Price: 100}, // Thu
		{Date: ymd("2025-11-21"), Price: 101}, // Fri: return 1
		{Date: ymd("2025-11-22"), Price: 50},  // Sat bar (eToro weekend quote): dropped
		{Date: ymd("2025-11-24"), Price: 102}, // Mon: return 2 (Fri -> Mon)
		{Date: ymd("2025-11-25"), Price: 103}, // Tue: return 3
		{Date: ymd("2025-11-26"), Price: 104}, // Wed: return 4
		{Date: ymd("2025-11-27"), Price: 60},  // Thanksgiving bar: dropped
		{Date: ymd("2025-11-28"), Price: 105}, // Fri: return 5 (Wed -> Fri over the holiday)
		{Date: ymd("2025-12-02"), Price: 106}, // Tue: Monday missing, chain broken
		{Date: ymd("2025-12-03"), Price: 107}, // Wed: return 6
	}
	rs := TradingLogReturns(cs)
	if len(rs) != 6 {
		t.Fatalf("%d returns: %+v", len(rs), rs)
	}
	want := []struct {
		d    string
		a, b float64
	}{{"2025-11-21", 100, 101}, {"2025-11-24", 101, 102}, {"2025-11-25", 102, 103}, {"2025-11-26", 103, 104}, {"2025-11-28", 104, 105}, {"2025-12-03", 106, 107}}
	for i, w := range want {
		if !rs[i].Date.Equal(ymd(w.d)) || !near(rs[i].R, math.Log(w.b/w.a), 1e-15) {
			t.Errorf("return %d: %+v, want %s %v", i, rs[i], w.d, math.Log(w.b/w.a))
		}
	}
	if n := len(TradingDayCloses(cs)); n != 8 {
		t.Fatalf("%d trading-day closes, want 8", n)
	}
	p := DefaultParams()
	if p.LongWindow != 252 || p.BacktestWindow != 250 || p.Lambda != 0.94 || p.MinReturns != 60 || p.Confidence != 0.99 {
		t.Fatalf("params %+v", p)
	}
}

// tradingCloses builds closes on consecutive NYSE trading days from log
// returns, starting at price 100 on the given date.
func tradingCloses(start time.Time, rs []float64) []varmodel.Close {
	out := []varmodel.Close{{Date: start, Price: 100}}
	d, p := start, 100.0
	for _, r := range rs {
		d = NextTradingDay(d)
		p *= math.Exp(r)
		out = append(out, varmodel.Close{Date: d, Price: p})
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

// Estimate on a trading-day series: the 252-return window and the
// 250-observation backtest are full, and the vol is the constant one.
func TestEstimateOnTradingDays(t *testing.T) {
	cs := tradingCloses(ymd("2023-01-03"), alternating(600, 0.01))
	e, err := Estimate(cs)
	if err != nil {
		t.Fatal(err)
	}
	if e.Returns != 600 || len(e.HistReturns) != 252 || e.Backtest.Observations != 250 || !near(e.Vol, 0.01, 1e-12) {
		t.Fatalf("estimate %+v backtest %+v hist %d", e.Forecast, e.Backtest, len(e.HistReturns))
	}
	if e.Backtest.Zone != varmodel.ZoneGreen {
		t.Fatalf("zone %v", e.Backtest.Zone)
	}
}
