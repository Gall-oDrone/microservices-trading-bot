package varmodel

import (
	"path/filepath"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
)

// The model on the committed Bitso daily history (2017-2026 btc_mxn,
// 2020-2026 btc_usd), at four cut-off dates a year apart. As of 2026-10-08:
// next-day vol 2.0-2.6 % (below the 4 % fixed default it replaces), the long
// window binding in calm years (EWMA 1.1 % vs 2.1 % in Sept 2025), and the
// backtest green except btc_mxn to 2026-09-26: yellow, 5 long-side
// exceptions, Kupiec p 0.16 (fat tails; the model is not rejected).
func TestRealBitsoHistory(t *testing.T) {
	root := filepath.Join("..", "..", "..", "docs", "backtest-readiness")
	for _, f := range []string{"evidence-2026-09-27/btc_mxn_daily_bitso.csv", "evidence-2026-09-28/btc_usd_daily_bitso.csv"} {
		rows, err := bitsodaily.ReadCSV(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		cs := make([]Close, len(rows))
		for i, r := range rows {
			d, err := time.Parse("2006-01-02", r.Date)
			if err != nil {
				t.Fatal(err)
			}
			cs[i] = Close{Date: d, Price: r.Close}
		}
		for _, cut := range []int{len(cs), len(cs) - 365, len(cs) - 730, len(cs) - 1100} {
			name := filepath.Base(f) + " to " + cs[cut-1].Date.Format("2006-01-02")
			e, err := EstimateFromCloses(cs[:cut], Lambda)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if e.Vol < 0.015 || e.Vol > 0.035 || e.Vol < e.EWMA || e.Vol < e.Long {
				t.Errorf("%s: vol %+v", name, e.Forecast)
			}
			b := e.Backtest
			if b.Observations != BacktestWindow || b.Zone == ZoneRed || b.Zone == ZoneInsufficient {
				t.Errorf("%s: backtest %+v", name, b)
			}
			if len(e.HistReturns) != LongWindow {
				t.Errorf("%s: %d historical returns", name, len(e.HistReturns))
			}
		}
	}
}
