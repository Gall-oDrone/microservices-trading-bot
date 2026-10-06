package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/strategy-executor/internal/bitsostage"
	"bitso-trading-platform/strategy-executor/internal/dailyexec"
)

// riskExchange is a fake stage exchange for the risk gate. Any order placed
// or cancelled fails the test unless allowOrders is set.
type riskExchange struct {
	t           *testing.T
	quote       bitsostage.Quote
	tickerErr   error
	open        []bitsostage.OpenOrder
	trades      map[string][]bitsostage.Trade
	allowOrders bool
	placed      int
}

func (f *riskExchange) Ticker(string) (bitsostage.Quote, error) { return f.quote, f.tickerErr }
func (f *riskExchange) Balances() (map[string]float64, error) {
	return map[string]float64{"btc": 1, "usd": 1e6, "mxn": 1e7}, nil
}
func (f *riskExchange) PlaceOrder(o bitsostage.OrderRequest) (string, error) {
	f.placed++
	if !f.allowOrders {
		f.t.Fatalf("an order was placed despite the risk check: %+v", o)
	}
	return "", errors.New("fake: orders not supported")
}
func (f *riskExchange) OpenOrders(string) ([]bitsostage.OpenOrder, error) { return f.open, nil }
func (f *riskExchange) CancelOrder(string) error {
	f.t.Fatal("unexpected cancel")
	return nil
}
func (f *riskExchange) TradesByOrigin(id string) ([]bitsostage.Trade, error) {
	return f.trades[id], nil
}

func stageOpts(p risk.Policy) options {
	return options{stage: true, size: 0.001, exec: dailyexec.DefaultConfig(), riskPolicy: p}
}

func usdRecord(signal string) *record {
	return &record{Mode: "stage", Book: "btc_usd",
		Decision: decision{BarDate: "2026-10-04", FillDate: "2026-10-05", Close: 85000, Signal: signal}}
}

func TestRiskBlockSkipsAndRecords(t *testing.T) {
	cases := []struct {
		name   string
		policy func() risk.Policy
		quote  bitsostage.Quote
		rule   string
	}{
		{"halted", func() risk.Policy {
			p := risk.DefaultPolicy()
			p.Halted, p.HaltReason = true, "operator review"
			return p
		}, bitsostage.Quote{Bid: 84990, Ask: 85010}, risk.RuleHalted},
		{"fat finger price", risk.DefaultPolicy, bitsostage.Quote{Bid: 99990, Ask: 100000}, risk.RulePriceDeviation},
		{"notional", func() risk.Policy {
			p := risk.DefaultPolicy()
			l := p.Books["btc_usd"]
			l.MaxOrderNotional = 50
			p.Books["btc_usd"] = l
			return p
		}, bitsostage.Quote{Bid: 84990, Ask: 85010}, risk.RuleMaxOrderNotional},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			led, _ := openLedger(filepath.Join(t.TempDir(), "l.jsonl"))
			ex := &riskExchange{t: t, quote: c.quote}
			err := finishBook(stageOpts(c.policy()), ex, led, usdRecord("long"))
			if err == nil || !strings.Contains(err.Error(), "blocked") {
				t.Fatalf("a blocked order must fail the run (exit 1), got %v", err)
			}
			if ex.placed != 0 {
				t.Fatalf("%d orders placed", ex.placed)
			}
			r, ok := led.get("btc_usd", "2026-10-04")
			if !ok {
				t.Fatal("a blocked day must still be recorded (skip and record, no retry)")
			}
			st := r.Stage
			if st == nil || st.Action != actionBlocked || st.Leg != nil || st.PositionAfter != st.PositionBefore || st.PositionAfter != flatPos() {
				t.Fatalf("stage part: %+v", st)
			}
			if st.Risk == nil || st.Risk.Allowed || st.Risk.Order.Side != "buy" || st.Risk.Order.Price != c.quote.Ask || st.Risk.Order.RefPrice != 85000 {
				t.Fatalf("risk part: %+v", st.Risk)
			}
			found := false
			for _, f := range st.Risk.Findings {
				found = found || (f.Rule == c.rule && f.Severity == risk.Block)
			}
			if !found {
				t.Fatalf("want a %s block finding, got %+v", c.rule, st.Risk.Findings)
			}
			if lastStagePosition(led, "btc_usd") != flatPos() {
				t.Fatal("the next day must plan from the unchanged position")
			}
		})
	}
}

// R2: the operator halt file blocks a planned stage order, which is recorded
// with the halt; a day with no order is recorded normally; a bad file fails.
func TestHaltFileBlocksAndIsRecorded(t *testing.T) {
	dir := t.TempDir()
	ledPath := filepath.Join(dir, "ledger.jsonl")
	if p, h, err := loadHalt(ledPath, risk.DefaultPolicy()); err != nil || h != nil || p.Halted {
		t.Fatalf("no file: %+v %+v %v", p, h, err)
	}
	write := func(body string) {
		if err := os.WriteFile(risk.HaltPath(ledPath), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"halted":true,"reason":"exchange incident"}`)
	if _, _, err := loadHalt(ledPath, risk.DefaultPolicy()); err == nil {
		t.Fatal("a halt without by/at must be rejected (exit 2)")
	}
	write(`{"halted":false}`)
	if _, h, err := loadHalt(ledPath, risk.DefaultPolicy()); err != nil || h != nil {
		t.Fatalf("halted=false: %+v %v", h, err)
	}
	write(`{"halted":true,"reason":"exchange incident","by":"diego","at":"2026-10-06T01:00:00Z"}`)
	p, h, err := loadHalt(ledPath, risk.DefaultPolicy())
	if err != nil || h == nil || !p.Halted {
		t.Fatalf("halted: %+v %+v %v", p, h, err)
	}
	o := stageOpts(p)
	o.halt = h

	led, _ := openLedger(ledPath)
	ex := &riskExchange{t: t, quote: bitsostage.Quote{Bid: 84990, Ask: 85010}}
	err = finishBook(o, ex, led, usdRecord("long"))
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("want blocked, got %v", err)
	}
	r, ok := led.get("btc_usd", "2026-10-04")
	if !ok || r.Stage == nil || r.Stage.Action != actionBlocked || r.Stage.Risk == nil {
		t.Fatalf("record: %+v", r.Stage)
	}
	if r.Stage.Risk.Halt == nil || r.Stage.Risk.Halt.By != "diego" || r.Stage.Risk.Findings[0].Rule != risk.RuleHalted ||
		!strings.Contains(r.Stage.Risk.Findings[0].Message, "exchange incident") {
		t.Fatalf("risk: %+v", r.Stage.Risk)
	}

	// Flat signal with a flat position: no order, so nothing to block.
	rec := usdRecord("flat")
	rec.Decision.BarDate, rec.Decision.FillDate = "2026-10-05", "2026-10-06"
	if err := finishBook(o, ex, led, rec); err != nil {
		t.Fatalf("a day without an order must record normally: %v", err)
	}
	if r, _ := led.get("btc_usd", "2026-10-05"); r.Stage == nil || r.Stage.Action != "none" || r.Stage.Risk != nil {
		t.Fatalf("no-order day: %+v", r.Stage)
	}
}

// If an earlier crashed run already traded part of this leg, recording it as
// blocked would lose those fills: stop without sending or recording.
func TestRiskBlockAfterPartialLegRecordsNothing(t *testing.T) {
	led, _ := openLedger(filepath.Join(t.TempDir(), "l.jsonl"))
	mk, _ := dailyexec.OriginIDs("btc_usd", "2026-10-05", "buy")
	ex := &riskExchange{t: t, quote: bitsostage.Quote{Bid: 99990, Ask: 100000},
		trades: map[string][]bitsostage.Trade{mk: {{OriginID: mk, Book: "btc_usd"}}}}
	err := finishBook(stageOpts(risk.DefaultPolicy()), ex, led, usdRecord("long"))
	if err == nil || !strings.Contains(err.Error(), "already started") {
		t.Fatalf("got %v", err)
	}
	if _, ok := led.get("btc_usd", "2026-10-04"); ok {
		t.Fatal("nothing may be recorded when a partial leg exists")
	}
	if ex.placed != 0 {
		t.Fatal("no order may be sent")
	}
}

func TestRiskTickerErrorRecordsNothing(t *testing.T) {
	led, _ := openLedger(filepath.Join(t.TempDir(), "l.jsonl"))
	ex := &riskExchange{t: t, tickerErr: errors.New("boom")}
	if err := finishBook(stageOpts(risk.DefaultPolicy()), ex, led, usdRecord("long")); err == nil {
		t.Fatal("want an error")
	}
	if _, ok := led.get("btc_usd", "2026-10-04"); ok {
		t.Fatal("no check, no record: a re-run must be able to retry")
	}
}

func TestCheckRiskPricesAtTheTouch(t *testing.T) {
	led, _ := openLedger(filepath.Join(t.TempDir(), "l.jsonl"))
	ex := &riskExchange{t: t, quote: bitsostage.Quote{Bid: 84900, Ask: 85100}}
	p := risk.DefaultPolicy()

	buy, err := checkRisk(p, ex, led, usdRecord("long"), "buy", 0.001, flatPos())
	if err != nil || !buy.Allowed || buy.Order.Price != 85100 || buy.PolicyVersion != p.Version {
		t.Fatalf("buy: %+v %v", buy, err)
	}
	// A reducing sell larger than the order cap is still allowed: a limit
	// must never trap the executor in a position.
	sell, err := checkRisk(p, ex, led, usdRecord("flat"), "sell", 0.02, position{"long", 0.02})
	if err != nil || !sell.Allowed || sell.Order.Price != 84900 {
		t.Fatalf("sell: %+v %v", sell, err)
	}
	ex.quote = bitsostage.Quote{Bid: 0, Ask: 85100}
	if _, err := checkRisk(p, ex, led, usdRecord("flat"), "sell", 0.001, position{"long", 0.001}); err == nil {
		t.Fatal("a missing bid must be an error, not a check at price 0")
	}
}

func TestLegsOnCountsTradedLegsForTheFillDate(t *testing.T) {
	led, _ := openLedger(filepath.Join(t.TempDir(), "l.jsonl"))
	add := func(book, bar, fill string, leg bool) {
		r := record{Book: book, Mode: "stage", Decision: decision{BarDate: bar, FillDate: fill}, Stage: &stageInfo{}}
		if leg {
			r.Stage.Leg = &dailyexec.Result{}
		}
		if err := led.append(r); err != nil {
			t.Fatal(err)
		}
	}
	add("btc_usd", "2026-10-03", "2026-10-04", true)
	add("btc_usd", "2026-10-04", "2026-10-05", false) // blocked or none: no leg
	add("btc_mxn", "2026-10-03", "2026-10-04", true)
	if n := legsOn(led, "btc_usd", "2026-10-04"); n != 1 {
		t.Fatalf("got %d", n)
	}
	if n := legsOn(led, "btc_usd", "2026-10-05"); n != 0 {
		t.Fatalf("got %d", n)
	}
}
