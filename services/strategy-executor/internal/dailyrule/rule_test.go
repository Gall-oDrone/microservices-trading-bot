package dailyrule_test

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"bitso-trading-platform/strategy-executor/internal/bitsodaily"
	"bitso-trading-platform/strategy-executor/internal/dailyrule"
)

func d(i int) time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i) }

func closes(cs ...float64) []dailyrule.Bar {
	bs := make([]dailyrule.Bar, len(cs))
	for i, c := range cs {
		bs[i] = dailyrule.Bar{Date: d(i), Open: c, High: c, Low: c, Close: c}
	}
	return bs
}

func TestEvaluate_WarmUpStrictInequalityAndSMA(t *testing.T) {
	pts := dailyrule.Evaluate(closes(1, 2, 3, 3, 1), 3)
	if pts[0].Warm || pts[1].Warm || pts[0].Long || pts[1].Long {
		t.Fatal("no decision before N closes are available")
	}
	// day 2: SMA(1,2,3)=2, close 3 > 2 -> long
	if !pts[2].Warm || pts[2].SMA != 2 || !pts[2].Long {
		t.Fatalf("day 2: %+v", pts[2])
	}
	// day 3: SMA(2,3,3)=8/3, close 3 > 2.67 -> long
	if !pts[3].Long || math.Abs(pts[3].SMA-8.0/3) > 1e-12 {
		t.Fatalf("day 3: %+v", pts[3])
	}
	// day 4: SMA(3,3,1)=7/3, close 1 -> flat
	if pts[4].Long {
		t.Fatalf("day 4: %+v", pts[4])
	}
	// close == SMA is NOT long
	if eq := dailyrule.Evaluate(closes(5, 5, 5), 3); eq[2].Long {
		t.Fatal("close equal to SMA must be flat")
	}
}

func TestTrendMatchesEvaluate(t *testing.T) {
	bs := closes(3, 1, 4, 1, 5, 9, 2, 6, 5, 3, 5, 8, 9, 7, 9)
	pts := dailyrule.Evaluate(bs, 4)
	for i, long := range dailyrule.Trend(bs, 4) {
		if long != pts[i].Long {
			t.Fatalf("bar %d: Trend=%v Evaluate=%v", i, long, pts[i].Long)
		}
	}
}

func TestEvaluate_RestartsAfterLongGap(t *testing.T) {
	bs := closes(100, 100, 100)
	for i := 0; i < 3; i++ {
		bs = append(bs, dailyrule.Bar{Date: d(3 + dailyrule.MaxGapDays + 1 + i), Close: 200})
	}
	pts := dailyrule.Evaluate(bs, 3)
	if pts[3].Warm || pts[4].Warm {
		t.Fatal("warm-up must restart after a gap longer than MaxGapDays")
	}
	if !pts[5].Warm || pts[5].SMA != 200 {
		t.Fatalf("re-warmed state: %+v", pts[5])
	}
}

// The registered evidence snapshots must reproduce the state each
// pre-registration recorded on its registration day. If this fails, either
// the rule or the candle reader changed, and the forward test would no longer
// be running the frozen rule.
func TestRegisteredSnapshotsReproducePreRegisteredState(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "docs", "backtest-readiness")
	cases := []struct {
		file     string
		date     string
		close    float64
		smaRound float64 // as printed in the pre-registration, rounded to units
		wantLong bool
		prereg   string
	}{
		{"evidence-2026-09-28/btc_usd_daily_bitso.csv", "2026-09-27", 83167, 76148, true, "SMA50-BTCUSD-2026-09-29"},
		{"evidence-2026-09-27/btc_mxn_daily_bitso.csv", "2026-09-26", 1493130, 1294875, true, "SMA50-2026-09-27"},
	}
	for _, c := range cases {
		rows, err := bitsodaily.ReadCSV(filepath.Join(root, c.file))
		if err != nil {
			t.Fatal(err)
		}
		bars := make([]dailyrule.Bar, len(rows))
		for i, r := range rows {
			day, _ := time.Parse("2006-01-02", r.Date)
			bars[i] = dailyrule.Bar{Date: day, Open: r.Open, High: r.High, Low: r.Low, Close: r.Close}
		}
		pts := dailyrule.Evaluate(bars, 50)
		last := len(bars) - 1
		if rows[last].Date != c.date {
			t.Fatalf("%s: last bar %s, want %s", c.file, rows[last].Date, c.date)
		}
		p := pts[last]
		if bars[last].Close != c.close || math.Round(p.SMA) != c.smaRound || p.Long != c.wantLong {
			t.Fatalf("%s (%s): close=%v sma=%.2f long=%v; pre-registration says close=%v sma=%v long=%v",
				c.file, c.prereg, bars[last].Close, p.SMA, p.Long, c.close, c.smaRound, c.wantLong)
		}
	}
}
