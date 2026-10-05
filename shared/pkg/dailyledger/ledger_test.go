package dailyledger

import (
	"math"
	"strings"
	"testing"
	"time"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestReadRealLedger(t *testing.T) {
	recs, err := ReadFile("testdata/ledger.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 6 {
		t.Fatalf("want 6 records, got %d", len(recs))
	}
	by := ByBook(recs)
	mxn := by["btc_mxn"]
	if len(mxn) != 3 || mxn[0].Decision.BarDate != "2026-09-29" || mxn[2].Decision.BarDate != "2026-10-01" {
		t.Fatalf("btc_mxn not sorted: %+v", mxn)
	}
	leg := mxn[0].Stage.Leg
	if leg == nil || !leg.Fallback || leg.Started.IsZero() {
		t.Fatalf("leg not parsed: %+v", leg)
	}
	// 7.86e-6 BTC fee at 1,510,998 on 1522.86 notional ~= 78 bps (taker).
	if bps := FeeBps("btc_mxn", *leg); !near(bps, 78, 1) {
		t.Fatalf("btc_mxn fee bps = %v, want ~78", bps)
	}
	usd := by["btc_usd"][0].Stage.Leg
	if bps := FeeBps("btc_usd", *usd); !near(bps, 25, 1) {
		t.Fatalf("btc_usd fee bps = %v, want ~25 (maker)", bps)
	}
	if p := LastStagePosition(mxn); p.State != "long" || !near(p.BTC, 0.00099999, 1e-12) {
		t.Fatalf("position = %+v", p)
	}
	if n := LegsOn(mxn, "2026-09-30"); n != 1 {
		t.Fatalf("legs on 2026-09-30 = %d", n)
	}
}

func TestReadRejectsBadLine(t *testing.T) {
	_, err := Read(strings.NewReader("{\"book\":\"btc_mxn\"}\n\nnot json\n"))
	if err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("want line 3 error, got %v", err)
	}
}

func TestSlippage(t *testing.T) {
	if s, ok := SlippageBps(Leg{Side: "buy", AvgPrice: 101}, 100); !ok || !near(s, 100, 1e-9) {
		t.Fatalf("buy slippage %v", s)
	}
	if s, ok := SlippageBps(Leg{Side: "sell", AvgPrice: 99}, 100); !ok || !near(s, 100, 1e-9) {
		t.Fatalf("sell slippage %v", s)
	}
	if _, ok := SlippageBps(Leg{Side: "buy", AvgPrice: 1}, 0); ok {
		t.Fatal("no ref must be !ok")
	}
}

func TestMissingDays(t *testing.T) {
	// 2026-10-03 21:00 UTC = 15:00 Mexico City; expected last bar 2026-10-02.
	now := time.Date(2026, 10, 3, 21, 0, 0, 0, time.UTC)
	got := MissingDays("2026-09-30", now)
	if strings.Join(got, ",") != "2026-10-01,2026-10-02" {
		t.Fatalf("got %v", got)
	}
	if len(MissingDays("2026-10-02", now)) != 0 {
		t.Fatal("up to date must have no gaps")
	}
	// 05:00 UTC on 2026-10-03 is still 2026-10-02 in Mexico City: expected 2026-10-01.
	if got := MissingDays("2026-10-01", time.Date(2026, 10, 3, 5, 0, 0, 0, time.UTC)); len(got) != 0 {
		t.Fatalf("Mexico day boundary wrong: %v", got)
	}
}
