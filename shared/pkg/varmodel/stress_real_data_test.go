package varmodel

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
)

func readCloses(t *testing.T, f string) []Close {
	t.Helper()
	rows, err := bitsodaily.ReadCSV(filepath.Join("..", "..", "..", "docs", "backtest-readiness", f))
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
	return cs
}

// The episodes on the committed Bitso closes: each window's trough, as
// documented in Episodes. btc_usd has no Bitso history before 2020-04-24
// (no path: the monitor proxies it with btc_mxn).
func TestEpisodesOnRealBitsoHistory(t *testing.T) {
	want := map[string]map[string]float64{
		"btc_mxn": {
			"2018-01-crash": -0.620, "2018-11-hash-war": -0.411, "2020-03-covid": -0.358,
			"2021-05-china-ban": -0.408, "2022-05-terra": -0.290, "2022-06-celsius-3ac": -0.363,
			"2022-11-ftx": -0.209, "2024-08-carry-unwind": -0.120,
		},
		"btc_usd": {
			"2021-05-china-ban": -0.411, "2022-05-terra": -0.305, "2022-06-celsius-3ac": -0.385,
			"2022-11-ftx": -0.212, "2024-08-carry-unwind": -0.175,
		},
	}
	files := map[string]string{
		"btc_mxn": "evidence-2026-09-27/btc_mxn_daily_bitso.csv",
		"btc_usd": "evidence-2026-09-28/btc_usd_daily_bitso.csv",
	}
	for book, f := range files {
		cs := readCloses(t, f)
		for _, e := range Episodes {
			p, ok := EpisodePath(cs, e)
			w, expect := want[book][e.ID]
			if ok != expect {
				t.Errorf("%s %s: path %v, want %v", book, e.ID, ok, expect)
				continue
			}
			if ok && math.Abs(Trough(p)-w) > 0.001 {
				t.Errorf("%s %s: trough %.4f, want %.3f", book, e.ID, Trough(p), w)
			}
			// Every window is complete on Bitso: one close per day.
			if ok && len(p) != int(e.End.Sub(e.Start).Hours()/24)+1 {
				t.Errorf("%s %s: %d closes in the window", book, e.ID, len(p))
			}
		}
	}
}

// ES 97.5 % on a year of real returns is above the normal ES with the same
// vol: the fat tails ES is meant to see.
func TestExpectedShortfallOnRealBitsoHistory(t *testing.T) {
	cs := readCloses(t, "evidence-2026-09-27/btc_mxn_daily_bitso.csv")
	e, err := EstimateFromCloses(cs, Lambda)
	if err != nil {
		t.Fatal(err)
	}
	pnl := make([]float64, len(e.HistReturns))
	for i, r := range e.HistReturns {
		pnl[i] = math.Expm1(r.R)
	}
	es, ok := ExpectedShortfall(pnl, ESConfidence, 250)
	if !ok {
		t.Fatal("no ES")
	}
	if normal := ZES * e.Long; es < normal || es > 3*normal {
		t.Fatalf("historical ES %.4f vs normal ES %.4f (365-day vol %.4f)", es, normal, e.Long)
	}
}
