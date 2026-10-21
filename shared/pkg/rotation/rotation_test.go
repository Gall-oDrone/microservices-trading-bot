package rotation

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/dailyrule"
)

// weekdays returns n weekday bars from start with closes from f(i).
func weekdays(start time.Time, n int, f func(i int) float64) []dailyrule.Bar {
	var out []dailyrule.Bar
	d := start
	for len(out) < n {
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday {
			c := f(len(out))
			out = append(out, dailyrule.Bar{Date: d, Open: c, High: c, Low: c, Close: c})
		}
		d = d.AddDate(0, 0, 1)
	}
	return out
}

func TestIsMonthEnd(t *testing.T) {
	bars := weekdays(time.Date(2026, 1, 28, 0, 0, 0, 0, time.UTC), 6, func(int) float64 { return 1 })
	// Jan 28 (Wed), 29, 30 (Fri), Feb 2 (Mon), 3, 4.
	want := []bool{false, false, true, false, false, false} // Feb 4 is not a month end: the next NYSE day is Feb 5
	for i, w := range want {
		if got := IsMonthEnd(bars, i); got != w {
			t.Errorf("bar %d (%s): month end %v, want %v", i, bars[i].Date.Format("2006-01-02"), got, w)
		}
	}
}

func TestEvaluatePicksStrongerAndRespectsHurdle(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	n := 300
	// Asset 0 rises 0.1 %/day, asset 1 rises 0.05 %/day: 0 always wins.
	a := weekdays(start, n, func(i int) float64 { return 100 * pow(1.001, i) })
	b := weekdays(start, n, func(i int) float64 { return 100 * pow(1.0005, i) })
	pts := Evaluate([][]dailyrule.Bar{a, b}, Params{Lookback: 126})
	// Before warm-up: flat, no decision.
	for i := 0; i < 126; i++ {
		if pts[i].Warm || pts[i].Decision || pts[i].Target != Flat {
			t.Fatalf("bar %d before warm-up: %+v", i, pts[i])
		}
	}
	decided := 0
	for i := 126; i < n; i++ {
		if pts[i].Decision {
			decided++
			if pts[i].Target != 0 {
				t.Fatalf("bar %d: target %d, want 0 (momentum %v)", i, pts[i].Target, pts[i].Momentum)
			}
			if !IsMonthEnd(a, i) {
				t.Fatalf("bar %d decided but is not a month end", i)
			}
		}
	}
	if decided < 7 {
		t.Fatalf("expected monthly decisions over ~8 months, got %d", decided)
	}
	// With a hurdle above both returns (126 x 0.1 % ~ 13.4 %), everything is flat.
	high := Evaluate([][]dailyrule.Bar{a, b}, Params{Lookback: 126, Hurdle: func(time.Time) float64 { return 0.5 }})
	for i := range high {
		if high[i].Target != Flat {
			t.Fatalf("bar %d: target %d with a 50 %% hurdle", i, high[i].Target)
		}
	}
}

func TestEvaluateCarriesTargetBetweenDecisions(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	// Asset 0 rises then falls; asset 1 flat. The target must only change
	// on decision bars.
	a := weekdays(start, 260, func(i int) float64 {
		if i < 160 {
			return 100 + float64(i)
		}
		return 260 - float64(i-160)*2
	})
	b := weekdays(start, 260, func(int) float64 { return 100 })
	pts := Evaluate([][]dailyrule.Bar{a, b}, Params{Lookback: 63, Skip: 0})
	for i := 1; i < len(pts); i++ {
		if pts[i].Target != pts[i-1].Target && !pts[i].Decision {
			t.Fatalf("bar %d: target changed %d->%d outside a decision bar", i, pts[i-1].Target, pts[i].Target)
		}
	}
	// Must have been long 0 while rising and flat after the fall shows in the lookback.
	if pts[150].Target != 0 {
		t.Fatalf("bar 150 target %d, want 0", pts[150].Target)
	}
	if pts[259].Target != Flat {
		t.Fatalf("last bar target %d, want flat (momentum %v)", pts[259].Target, lastDecision(pts).Momentum)
	}
}

func TestSkipExcludesRecentBars(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	a := weekdays(start, 200, func(i int) float64 { return 100 + float64(i) })
	pts0 := Evaluate([][]dailyrule.Bar{a}, Params{Lookback: 100, Skip: 0})
	pts1 := Evaluate([][]dailyrule.Bar{a}, Params{Lookback: 100, Skip: 21})
	d0, d1 := lastDecision(pts0), lastDecision(pts1)
	if d0.Momentum[0] == d1.Momentum[0] {
		t.Fatalf("skip had no effect: %v vs %v", d0.Momentum, d1.Momentum)
	}
	// Warm-up is lookback+skip.
	if pts1[120].Warm || !pts1[121].Warm {
		t.Fatalf("warm-up with skip: bar 120 %v bar 121 %v", pts1[120].Warm, pts1[121].Warm)
	}
}

func TestAlign(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	a := weekdays(start, 10, func(i int) float64 { return float64(i) })
	b := append([]dailyrule.Bar(nil), a[2:8]...)
	b = append(b[:2], b[3:]...) // drop one date in the middle
	al := Align(a, b)
	if len(al[0]) != len(b) || len(al[1]) != len(b) {
		t.Fatalf("aligned lengths %d / %d, want %d", len(al[0]), len(al[1]), len(b))
	}
	for i := range al[0] {
		if !al[0][i].Date.Equal(al[1][i].Date) {
			t.Fatalf("dates differ at %d", i)
		}
	}
}

func lastDecision(pts []Point) Point {
	for i := len(pts) - 1; i >= 0; i-- {
		if pts[i].Decision {
			return pts[i]
		}
	}
	return Point{}
}

func pow(b float64, n int) float64 {
	r := 1.0
	for i := 0; i < n; i++ {
		r *= b
	}
	return r
}
