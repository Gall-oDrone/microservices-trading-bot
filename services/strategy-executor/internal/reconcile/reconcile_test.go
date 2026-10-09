package reconcile

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/strategy-executor/internal/bitsostage"
	"bitso-trading-platform/strategy-executor/internal/dailyexec"
)

// The real stage client must satisfy the read-only Source.
var _ Source = (*bitsostage.Client)(nil)

type fakeSource struct {
	trades map[string][]bitsostage.Trade
	bal    map[string]float64
	calls  []string
	failOn string
	balErr error
}

func (f *fakeSource) Balances() (map[string]float64, error) {
	f.calls = append(f.calls, "balances")
	return f.bal, f.balErr
}

func (f *fakeSource) TradesByOrigin(o string) ([]bitsostage.Trade, error) {
	f.calls = append(f.calls, o)
	if o == f.failOn {
		return nil, errors.New("bitso down")
	}
	return f.trades[o], nil
}

// The ui-api fixture is the real stage ledger's first days: one leg per book.
func realLedger(t *testing.T) []dailyledger.Record {
	t.Helper()
	recs, err := dailyledger.ReadFile(filepath.Join("..", "..", "..", "ui-api", "internal", "api", "testdata", "ledger.jsonl"))
	if err != nil || len(recs) == 0 {
		t.Fatalf("fixture: %d records, %v", len(recs), err)
	}
	return recs
}

// realTrades are the Bitso trades behind those two legs (amounts from the
// ledger; the btc_mxn leg is 1 maker fill and 1 market fallback fill).
func realTrades() map[string][]bitsostage.Trade {
	return map[string][]bitsostage.Trade{
		"sma50-btc_usd-20260930-b-m": {{Oid: "mykZ1geAhtLuJn5X", Major: 0.001, Minor: 83.481, Fee: 2.5e-06, FeeCurrency: "btc"}},
		"sma50-btc_mxn-20260930-b-m": {{Oid: "XzYf4h0PZsSuECcU", Major: 1.2e-06, Minor: 1.81, Fee: 0, FeeCurrency: "btc"}},
		"sma50-btc_mxn-20260930-b-t": {{Oid: "zuLFQFpzkPv9hKxr", Major: 0.00100665, Minor: 1521.04979, Fee: 7.86e-06, FeeCurrency: "btc"}},
	}
}

// 2026-10-02 06:00 Mexico City: the 2026-10-01 bar (the fixture's last) is
// the latest closed one, so no day is missing.
var at = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func TestRealLedgerReconciles(t *testing.T) {
	src := &fakeSource{trades: realTrades(), bal: map[string]float64{"btc": 0.5, "mxn": 1000}}
	rep, err := Run(src, realLedger(t), at)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK || rep.Breaks != 0 || len(rep.Legs) != 2 || len(rep.Days) != 0 {
		t.Fatalf("report %+v", rep)
	}
	for _, l := range rep.Legs {
		if l.Status != StatusMatched || len(l.Diffs) != 0 {
			t.Errorf("%s: %+v", l.Book, l)
		}
	}
	mxn := rep.Legs[0]
	if mxn.Book != "btc_mxn" || mxn.Exchange.Trades != 2 || mxn.Exchange.BaseDelta != 0.00099999 {
		t.Fatalf("btc_mxn leg %+v", mxn)
	}
	// Both books draw on one BTC balance: 0.00099999 + 0.0009975.
	if len(rep.Balances) != 1 || rep.Balances[0].Currency != "btc" || rep.Balances[0].Required != 0.00099999+0.0009975 {
		t.Fatalf("balances %+v", rep.Balances)
	}
	for _, p := range rep.Positions {
		if !p.OK {
			t.Errorf("position %+v", p)
		}
	}
	// 2 origins per leg, then one balance call: nothing else is asked.
	if len(src.calls) != 5 || src.calls[4] != "balances" {
		t.Fatalf("calls %v", src.calls)
	}
}

func TestLegMismatchAndUnknownOrder(t *testing.T) {
	tr := realTrades()
	// A second market fill the ledger never recorded.
	tr["sma50-btc_mxn-20260930-b-t"] = append(tr["sma50-btc_mxn-20260930-b-t"],
		bitsostage.Trade{Oid: "ghost0000000000", Major: 0.0005, Minor: 755.5, Fee: 3.9e-06, FeeCurrency: "btc"})
	rep, err := Run(&fakeSource{trades: tr, bal: map[string]float64{"btc": 1}}, realLedger(t), at)
	if err != nil {
		t.Fatal(err)
	}
	mxn := rep.Legs[0]
	if rep.OK || rep.Breaks != 1 || mxn.Status != StatusMismatch || len(mxn.UnknownOids) != 1 || mxn.UnknownOids[0] != "ghost0000000000" {
		t.Fatalf("report %+v", rep)
	}
	got := strings.Join(mxn.Diffs, "; ")
	for _, want := range []string{"filled: ledger 0.00100785, Bitso 0.00150785", "notional:", "base change:", "fee btc:", "orders not in the ledger: ghost0000000000"} {
		if !strings.Contains(got, want) {
			t.Errorf("diffs %q lack %q", got, want)
		}
	}
	if rep.Legs[1].Status != StatusMatched {
		t.Errorf("btc_usd %+v", rep.Legs[1])
	}
}

func TestUnrecordedFillsOnMissingDays(t *testing.T) {
	// 2026-10-04 06:00 Mexico City: bars 2026-10-02 and 2026-10-03 have no
	// record. A sell traded at the 2026-10-04 open (bar 2026-10-03) and the
	// executor crashed before writing the ledger.
	later := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	mk, _ := dailyexec.OriginIDs("btc_usd", "2026-10-04", "sell")
	tr := realTrades()
	tr[mk] = []bitsostage.Trade{{Oid: "lost", Major: 0.0009975, Minor: 85, Fee: 0.25, FeeCurrency: "usd"}}
	src := &fakeSource{trades: tr, bal: map[string]float64{"btc": 1}}
	rep, err := Run(src, realLedger(t), later)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Days) != 4 || rep.Breaks != 1 || rep.OK {
		t.Fatalf("days %+v breaks %d", rep.Days, rep.Breaks)
	}
	var bad []DayCheck
	for _, d := range rep.Days {
		if d.Reason != "no_record" || len(d.Origins) != 4 {
			t.Errorf("day %+v", d)
		}
		if d.Status != StatusClean {
			bad = append(bad, d)
		}
	}
	if len(bad) != 1 || bad[0].Book != "btc_usd" || bad[0].BarDate != "2026-10-03" || bad[0].FillDate != "2026-10-04" ||
		bad[0].Status != StatusUnrecorded || bad[0].BaseBTC != 0.0009975 {
		t.Fatalf("unrecorded %+v", bad)
	}
}

func TestBlockedDayChecksOnlyThePlannedSide(t *testing.T) {
	recs := []dailyledger.Record{{Mode: "stage", Book: "btc_usd",
		Decision: dailyledger.Decision{BarDate: "2026-10-01", FillDate: "2026-10-02"},
		Stage: &dailyledger.Stage{Action: dailyledger.ActionBlocked,
			Risk: &dailyledger.RiskCheck{Order: riskOrder("buy")}}}}
	src := &fakeSource{bal: map[string]float64{"btc": 0}}
	rep, err := Run(src, recs, at)
	if err != nil {
		t.Fatal(err)
	}
	mk, tk := dailyexec.OriginIDs("btc_usd", "2026-10-02", "buy")
	if !rep.OK || len(rep.Days) != 1 || rep.Days[0].Reason != "blocked" ||
		strings.Join(rep.Days[0].Origins, ",") != mk+","+tk {
		t.Fatalf("report %+v", rep)
	}
}

func TestBalanceShortfallAndPositionDrift(t *testing.T) {
	recs := realLedger(t)
	// The account holds less BTC than the two books together.
	rep, err := Run(&fakeSource{trades: realTrades(), bal: map[string]float64{"btc": 0.0015}}, recs, at)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK || rep.Breaks != 1 || rep.Balances[0].OK {
		t.Fatalf("balance %+v", rep.Balances)
	}
	// A hand-edited position that no longer equals the sum of the legs.
	for i := range recs {
		if recs[i].Book == "btc_usd" && recs[i].Decision.BarDate == "2026-10-01" {
			recs[i].Stage.PositionAfter.BTC = 0.001
		}
	}
	rep, err = Run(&fakeSource{trades: realTrades(), bal: map[string]float64{"btc": 1}}, recs, at)
	if err != nil {
		t.Fatal(err)
	}
	var usd PositionCheck
	for _, p := range rep.Positions {
		if p.Book == "btc_usd" {
			usd = p
		}
	}
	if rep.OK || usd.OK || usd.LedgerBTC != 0.001 || usd.SumDeltaBTC != 0.0009975 {
		t.Fatalf("positions %+v", rep.Positions)
	}
}

func TestSourceErrorsStopTheRun(t *testing.T) {
	if _, err := Run(&fakeSource{trades: realTrades(), failOn: "sma50-btc_mxn-20260930-b-t"}, realLedger(t), at); err == nil ||
		!strings.Contains(err.Error(), "bitso down") {
		t.Fatalf("err %v", err)
	}
	if _, err := Run(&fakeSource{trades: realTrades(), balErr: errors.New("401")}, realLedger(t), at); err == nil {
		t.Fatal("balance error ignored")
	}
}

func TestDryRunOnlyLedgerHasNothingToReconcile(t *testing.T) {
	recs := []dailyledger.Record{{Mode: "dry-run", Book: "btc_mxn", Decision: dailyledger.Decision{BarDate: "2026-10-01"}}}
	src := &fakeSource{}
	rep, err := Run(src, recs, at)
	if err != nil || !rep.OK || len(src.calls) != 0 {
		t.Fatalf("report %+v calls %v err %v", rep, src.calls, err)
	}
}

func riskOrder(side string) risk.Order { return risk.Order{Book: "btc_usd", Side: side, QtyBTC: 0.001} }
