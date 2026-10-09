package api

import (
	"io"
	"log"
	"math"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/execcost"
	"bitso-trading-platform/ui-api/internal/live"
)

// A book around mid 1,500,000 MXN: 0.02 BTC at 5 bps, 0.1 more at 9 bps,
// then 1 BTC at 30 bps, per side.
func testHub() *live.Hub {
	h := live.NewHub([]string{"btc_mxn", "btc_usd"}, "test", nil, nil, log.New(io.Discard, "", 0))
	h.Now = func() time.Time { return fixedNow }
	mid := 1_500_000.0
	lv := func(bps, amt float64, ask bool) live.Level {
		p := mid * (1 - bps/1e4)
		if ask {
			p = mid * (1 + bps/1e4)
		}
		return live.Level{Price: p, Amount: amt}
	}
	h.OnTop(live.Top{Book: "btc_mxn", Bid: mid * (1 - 5e-4), Ask: mid * (1 + 5e-4), At: fixedNow,
		Bids: []live.Level{lv(5, 0.02, false), lv(9, 0.1, false), lv(30, 1, false)},
		Asks: []live.Level{lv(5, 0.02, true), lv(9, 0.1, true), lv(30, 1, true)}})
	return h
}

func TestCapacityWithLiveBook(t *testing.T) {
	ts := newTestServer(t, fixedNow, func(s *Server) { s.Live = testHub() })
	c := get[CapacityResponse](t, ts, "/api/ui/forward-tests/btc_mxn/capacity", 200)
	if c.BookStatus != "live" || !near(c.Mid, 1_500_000, 1e-6) || !near(c.SpreadBps, 10, 1e-6) || !near(c.BidDepthBTC, 1.12, 1e-9) {
		t.Fatalf("book %+v", c)
	}
	if c.MakerFeeBps != 60 || c.TakerFeeBps != 78 || c.SlippageBudgetBps != 10 || c.StageSizeBTC != 0.001 || c.PolicyMaxBTC != 0.01 {
		t.Fatalf("costs %+v", c)
	}
	if len(c.Rows) != len(capacitySizes) || c.ADVBTC <= 0 || c.DailyVol <= 0 {
		t.Fatalf("rows %d adv %v vol %v", len(c.Rows), c.ADVBTC, c.DailyVol)
	}
	r := c.Rows[0] // 0.001 BTC: within the first level, 5 bps
	if r.QtyBTC != 0.001 || r.BuyWalkBps == nil || !near(*r.BuyWalkBps, 5, 1e-6) || !r.BookFills || !r.WithinBudget || !near(r.TakerTotalBps, 83, 1e-6) {
		t.Fatalf("0.001 %+v", r)
	}
	var big CapacityRow
	for _, x := range c.Rows {
		if x.QtyBTC == 2 {
			big = x
		}
	}
	if big.BookFills || big.WithinBudget {
		t.Fatalf("2 BTC is past the visible depth: %+v", big)
	}
	// Both inner levels (0.12 BTC) average 8.3 bps; the walk can take part
	// of the 30 bps level until the average reaches 10 bps.
	want := 0.12 + (0.12*10-(0.02*5+0.1*9))/(30-10)
	if c.WalkCapacityBTC == nil || !near(*c.WalkCapacityBTC, want, 1e-6) {
		t.Fatalf("walk capacity %v want %v", c.WalkCapacityBTC, want)
	}
	if !(c.SqrtCapacityHi > c.SqrtCapacityLo && c.SqrtCapacityLo > 0) {
		t.Fatalf("sqrt capacity %v %v", c.SqrtCapacityLo, c.SqrtCapacityHi)
	}
	// Square-root law: 50x the size (0.05 vs 0.001 BTC), sqrt(50)x the impact.
	if !near(c.Rows[3].SqrtHiBps/c.Rows[0].SqrtHiBps, math.Sqrt(50), 1e-9) {
		t.Fatalf("sqrt scaling %v", c.Rows[3].SqrtHiBps/c.Rows[0].SqrtHiBps)
	}
}

func TestCapacityWithoutBook(t *testing.T) {
	c := get[CapacityResponse](t, newTestServer(t, fixedNow, nil), "/api/ui/forward-tests/btc_usd/capacity", 200)
	if c.BookStatus != "none" || c.WalkCapacityBTC != nil || c.Rows[0].BuyWalkBps != nil || c.TakerFeeBps != 36 {
		t.Fatalf("%+v", c)
	}
	// Without a book the total uses the square-root upper estimate.
	if !near(c.Rows[0].TakerTotalBps, 36+c.Rows[0].SqrtHiBps, 1e-9) || !near(c.Rows[0].Notional, 0.001*c.LastClose, 1e-9) {
		t.Fatalf("row %+v", c.Rows[0])
	}
	// A stale book (older than 5 minutes) is not used.
	h := testHub()
	ts := newTestServer(t, fixedNow.Add(10*time.Minute), func(s *Server) { s.Live = h })
	if st := get[CapacityResponse](t, ts, "/api/ui/forward-tests/btc_mxn/capacity", 200); st.BookStatus != "stale" || st.WalkCapacityBTC != nil {
		t.Fatalf("stale %+v", st.BookStatus)
	}
}

func TestCapacityHistoryFromBookSamples(t *testing.T) {
	dir := t.TempDir()
	path := execcost.SamplePath(dir, "btc_mxn")
	for i, spread := range []float64{2, 3, 4} {
		s := execcost.Sample{At: fixedNow.Add(-time.Duration(i+1) * time.Hour).Format(time.RFC3339), Book: "btc_mxn", Mid: 1_500_000,
			SpreadBps: spread, BidDepthBTC: 1, AskDepthBTC: 2, WalkCapacityBTC: 0.1 * float64(i+1), BudgetBps: 10,
			Sizes: []execcost.SizeCost{{QtyBTC: 0.1, BuyBps: spread, SellBps: 1, Complete: true}}}
		if err := execcost.AppendSample(path, s); err != nil {
			t.Fatal(err)
		}
	}
	// One sample older than the 30-day window is left out.
	old := execcost.Sample{At: fixedNow.AddDate(0, 0, -31).Format(time.RFC3339), Book: "btc_mxn", Mid: 1, SpreadBps: 99}
	if err := execcost.AppendSample(path, old); err != nil {
		t.Fatal(err)
	}
	ts := newTestServer(t, fixedNow, func(s *Server) { s.BookSamplesDir = dir })
	c := get[CapacityResponse](t, ts, "/api/ui/forward-tests/btc_mxn/capacity", 200)
	h := c.History
	if h == nil || h.Samples != 3 || c.HistoryDays != 30 || h.SpreadBps.P50 != 3 || !near(h.WalkCapacityBTC.P50, 0.2, 1e-9) || len(h.Sizes) != 1 {
		t.Fatalf("history %+v", h)
	}
	// No samples for btc_usd: history is null.
	if u := get[CapacityResponse](t, ts, "/api/ui/forward-tests/btc_usd/capacity", 200); u.History != nil {
		t.Fatalf("btc_usd history %+v", u.History)
	}
}
