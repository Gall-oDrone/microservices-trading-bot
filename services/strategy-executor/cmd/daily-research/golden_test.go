package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bitso-trading-platform/strategy-executor/internal/bitsodaily"
)

// goldenSimulateSHA256 is the SHA-256 of the exact float bits simulate
// produced on the registered evidence before the loop moved to
// shared/pkg/dailyrule (plan §7 item 5, 2026-10-08). The same digest was
// reproduced after the move. A change here means the simulator's numbers
// changed: the registered results would no longer reproduce, so do not
// update the digest without a new pre-registration.
const goldenSimulateSHA256 = "f9df0232393cefc249760ce4c72b1f4164215a0b6f3ab9a1d0dfdb26a1980545"

// TestSimulateGolden runs simulate over both Bitso evidence files, three
// cost models, four windows, three rule signals and 25 random signals per
// window, and hashes every result's float bits. Set GOLDEN_OUT to a path to
// also write the 672 lines for a diff.
func TestSimulateGolden(t *testing.T) {
	h := sha256.New()
	var w hash.Hash = h
	if out := os.Getenv("GOLDEN_OUT"); out != "" {
		f, err := os.Create(out)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		w = teeHash{h, f}
	}
	lines := 0
	root := filepath.Join("..", "..", "..", "..", "docs", "backtest-readiness")
	for _, file := range []string{"evidence-2026-09-27/btc_mxn_daily_bitso.csv", "evidence-2026-09-28/btc_usd_daily_bitso.csv"} {
		rows, err := bitsodaily.ReadCSV(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		bars := make([]bar, len(rows))
		for i, r := range rows {
			d, err := time.Parse("2006-01-02", r.Date)
			if err != nil {
				t.Fatal(err)
			}
			bars[i] = bar{Date: d, Open: r.Open, High: r.High, Low: r.Low, Close: r.Close}
		}
		cs := []costModel{{}, {buy: 0.0088, sell: 0.0088}, {buy: 0.0035, sell: 0.006}}
		wins := [][2]int{{0, len(bars) - 1}, {1, 400}, {200, len(bars) - 1}, {len(bars) - 120, len(bars) - 1}}
		r := newRand(7)
		for _, c := range cs {
			for _, win := range wins {
				for _, want := range [][]bool{signalTrend(bars, 50), signalBuyAndHold(len(bars)), signalTrend(bars, 20)} {
					res := simulate(bars, want, win[0], win[1], c)
					fmt.Fprintf(w, "%s %v %v %x %d %x %x %x\n", file, c, win, math.Float64bits(res.ReturnPct), res.RoundTrips, math.Float64bits(res.ExposurePct), math.Float64bits(res.MaxDDPct), math.Float64bits(res.CostPct))
					lines++
				}
				for s := 0; s < 25; s++ {
					res := simulate(bars, randomWant(r, len(bars), win[0], win[1], 1+s%9), win[0], win[1], c)
					fmt.Fprintf(w, "rnd %x %d %x %x %x\n", math.Float64bits(res.ReturnPct), res.RoundTrips, math.Float64bits(res.ExposurePct), math.Float64bits(res.MaxDDPct), math.Float64bits(res.CostPct))
					lines++
				}
			}
		}
	}
	if lines != 672 {
		t.Fatalf("golden lines = %d, want 672", lines)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != goldenSimulateSHA256 {
		t.Fatalf("simulate output changed: sha256 %s, want %s (rerun with GOLDEN_OUT=/tmp/g.txt to diff)", got, goldenSimulateSHA256)
	}
}

// teeHash writes to the hash and a file.
type teeHash struct {
	hash.Hash
	f *os.File
}

func (t teeHash) Write(p []byte) (int, error) {
	if _, err := t.f.Write(p); err != nil {
		return 0, err
	}
	return t.Hash.Write(p)
}
