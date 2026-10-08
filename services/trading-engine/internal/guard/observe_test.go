package guard

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
)

type fakeObs struct {
	checks []string
	rules  [][]string
	util   map[string]float64
	devBps []float64
}

func (f *fakeObs) RecordPolicyCheck(book, result string, rules []string) {
	f.checks = append(f.checks, result)
	f.rules = append(f.rules, rules)
}
func (f *fakeObs) ObserveUtilization(book, limit string, value, max float64) {
	if f.util == nil {
		f.util = map[string]float64{}
	}
	if max > 0 {
		f.util[limit] = value / max
	}
}
func (f *fakeObs) ObservePriceDeviation(book, side string, bps float64) {
	f.devBps = append(f.devBps, bps)
}

func TestCheckObservesEveryOutcome(t *testing.T) {
	ctx := context.Background()
	obs := &fakeObs{}
	g := &PreTrade{Policy: DefaultPolicy()}

	o := buy(0.05, 100_000) // 0.5 of max_order_btc, 5,000 of 10,000 MXN
	o.RefPrice = 100_500
	if err := g.Check(ctx, o, obs); err != nil {
		t.Fatal(err)
	}
	if obs.util[risk.RuleMaxOrderBTC] != 0.5 || obs.util[risk.RuleMaxOrderNotional] != 0.5 {
		t.Fatalf("utilization %v", obs.util)
	}
	if len(obs.devBps) != 1 || obs.devBps[0] < 49 || obs.devBps[0] > 50 {
		t.Fatalf("deviation %v", obs.devBps)
	}

	if err := g.Check(ctx, buy(0.5, 1_000_000), obs); err == nil {
		t.Fatal("oversized order allowed")
	}
	g.Position = func(context.Context, string) (float64, error) { return 0, os.ErrDeadlineExceeded }
	if err := g.Check(ctx, buy(0.001, 1_000_000), obs); err == nil {
		t.Fatal("unknown position allowed")
	}

	if want := []string{ResultAllowed, ResultBlocked, ResultError}; !reflect.DeepEqual(obs.checks, want) {
		t.Fatalf("results %v, want %v", obs.checks, want)
	}
	if !reflect.DeepEqual(obs.rules[1], []string{risk.RuleMaxOrderBTC, risk.RuleMaxOrderNotional}) {
		t.Fatalf("blocked rules %v", obs.rules[1])
	}
	if !reflect.DeepEqual(obs.rules[2], []string{RulePositionUnknown}) {
		t.Fatalf("error rules %v", obs.rules[2])
	}

	// An invalid halt file is an error (fail closed), reported as rule halted.
	bad := filepath.Join(t.TempDir(), "risk-state.json")
	_ = os.WriteFile(bad, []byte(`{"halted":true}`), 0o600)
	g = &PreTrade{Policy: DefaultPolicy(), HaltFiles: []string{bad}}
	_ = g.Check(ctx, buy(0.001, 1_000_000), obs)
	if obs.checks[3] != ResultError || !reflect.DeepEqual(obs.rules[3], []string{risk.RuleHalted}) {
		t.Fatalf("invalid halt: %v %v", obs.checks[3], obs.rules[3])
	}
}

type limitMap map[string]float64

func (m limitMap) SetLimit(book, limit string, v float64) { m[book+"/"+limit] = v }

func TestPublishLimits(t *testing.T) {
	m := limitMap{}
	PublishLimits(DefaultPolicy(), m)
	for k, want := range map[string]float64{
		"btc_mxn/max_order_btc":           0.1,
		"btc_mxn/max_order_notional":      10000,
		"btc_usd/max_order_notional":      600,
		"default/max_order_btc":           0.01,
		"btc_mxn/max_price_deviation_bps": 500,
		"btc_mxn/max_position_btc":        0,
	} {
		if got, ok := m[k]; !ok || got != want {
			t.Errorf("%s = %v (present %v), want %v", k, got, ok, want)
		}
	}
}

func TestWatchHalts(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "risk-state.json")
	type rec struct {
		configured, invalid int
		halted              bool
	}
	var mu sync.Mutex
	var got []rec
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		WatchHalts(ctx, []string{f}, 20*time.Millisecond, func(c, inv int, h bool, _ time.Time, _ []string) {
			mu.Lock()
			got = append(got, rec{c, inv, h})
			mu.Unlock()
		})
		close(done)
	}()
	waitFor := func(want rec) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			last := rec{}
			if len(got) > 0 {
				last = got[len(got)-1]
			}
			mu.Unlock()
			if last == want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("never saw %+v; got %+v", want, got)
	}
	waitFor(rec{1, 0, false}) // missing file: not halted
	_ = os.WriteFile(f, []byte(`{"halted":true,"reason":"drill","by":"diego","at":"2026-10-08T18:00:00Z"}`), 0o600)
	waitFor(rec{1, 0, true})
	_ = os.WriteFile(f, []byte(`{"halted":tru`), 0o600)
	waitFor(rec{1, 1, false})
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WatchHalts did not stop on cancel")
	}
}
