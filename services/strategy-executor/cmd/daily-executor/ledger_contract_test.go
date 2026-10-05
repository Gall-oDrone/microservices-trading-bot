package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/strategy-executor/internal/dailyexec"
)

// TestLedgerContractMatchesShared fails when the executor's ledger JSON and
// the importable mirror in shared/pkg/dailyledger (read by services/ui-api)
// drift apart: a field added, renamed or removed on either side.
func TestLedgerContractMatchesShared(t *testing.T) {
	ts := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	full := record{
		RecordedAt: "2026-10-01T01:00:00Z", CodeVersion: "abc", Mode: "stage", Book: "btc_mxn", Prereg: "P.md",
		Decision: decision{BarDate: "2026-09-30", FillDate: "2026-10-01", Close: 1, SMA: 2, Signal: "long", PrevSignal: "flat", Action: "buy"},
		Paper: paperResult{ForwardStart: "2026-09-27", Days: 3, Position: "long", Fills: 1, LegCostBps: 70,
			Equity: 1.1, EquityClosed: 1.09, HoldEquity: 1.2, MaxDrawdown: 0.01, PendingAction: "hold"},
		Candles: candleInfo{Source: "s", First: "2017-05-31", Last: "2026-09-30", Bars: 9, RecentGaps: "2026-01-01", SHA256Short: "ff"},
		Stage: &stageInfo{Env: "e", Target: "long", Action: "buy",
			PositionBefore: position{State: "flat"}, PositionAfter: position{State: "long", BTC: 0.001},
			Leg: &dailyexec.Result{Side: "buy", Target: 0.001, Filled: 0.001, MakerFilled: 0.0005, TakerFilled: 0.0005,
				BaseDelta: 0.000999, AvgPrice: 3, Notional: 4, Fees: map[string]float64{"btc": 1e-6},
				MakerOrigin: "m", TakerOrigin: "t", Oids: []string{"o"}, Placements: 1, Fallback: true,
				Shortfall: 0.1, Notes: []string{"n"}, Started: ts, Finished: ts.Add(time.Minute)},
			Risk: &riskInfo{PolicyVersion: "v", Order: risk.Order{Book: "btc_mxn", Side: "buy", QtyBTC: 0.001, Price: 3, RefPrice: 1},
				State: risk.State{PositionBTC: 0, OrdersToday: 0}, Allowed: true,
				Findings: []risk.Finding{{Rule: risk.RuleCostWarn, Severity: risk.Warn, Limit: 1, Value: 2, Message: "m"}}}},
	}
	b, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}

	var mirror dailyledger.Record
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&mirror); err != nil {
		t.Fatalf("shared/pkg/dailyledger is missing a field the executor writes: %v", err)
	}
	back, err := json.Marshal(mirror)
	if err != nil {
		t.Fatal(err)
	}
	var want, got map[string]any
	_ = json.Unmarshal(b, &want)
	_ = json.Unmarshal(back, &got)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("ledger JSON differs after a round trip through shared/pkg/dailyledger:\nexecutor: %s\nmirror:   %s", b, back)
	}
}
