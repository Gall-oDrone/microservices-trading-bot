package main

import (
	"fmt"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/strategy-executor/internal/dailyexec"
)

// Pre-trade risk guard (docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §6, step
// R1). Before every stage order the executor runs shared/pkg/risk.Check. It
// guards execution only: the rule's signal and the paper account never depend
// on it, so the frozen pre-registered rule is unchanged.
//
// A blocked order is skipped and recorded, never retried: the day's ledger
// line carries action "blocked", the unchanged position and the findings, and
// the run exits 1 so the operator sees it. The next day plans from the
// recorded position as usual.
//
// One exception: if an earlier, crashed run already started this leg (an
// open order or fills under its client ids), recording it as blocked would
// leave fills the ledger does not know about. Then nothing is sent and
// nothing is recorded, and the run fails for the operator to resolve.

// actionBlocked is stageInfo.Action when the risk check stopped the order.
const actionBlocked = "blocked"

// riskInfo is the risk check recorded with every stage order attempt.
type riskInfo struct {
	PolicyVersion string         `json:"policy_version"`
	Order         risk.Order     `json:"order"`
	State         risk.State     `json:"state"`
	Allowed       bool           `json:"allowed"`
	Findings      []risk.Finding `json:"findings,omitempty"`
	// Halt is the operator halt file in force for this run, if any (R2).
	Halt *risk.HaltState `json:"halt,omitempty"`
}

// loadHalt reads the operator halt file next to the ledger (R2) and merges
// a halt into the policy, so a planned stage order is blocked and recorded
// like any other block. No file means not halted. A file that cannot be read
// or is invalid is an error: the caller must not run at all (fail closed).
func loadHalt(ledgerPath string, p risk.Policy) (risk.Policy, *risk.HaltState, error) {
	h, _, err := risk.LoadHaltState(risk.HaltPath(ledgerPath))
	if err != nil {
		return p, nil, err
	}
	if !h.Halted {
		return p, nil, nil
	}
	return risk.ApplyHalt(p, h), &h, nil
}

// checkRisk prices the planned order at the touch it would trade against
// (best ask for a buy, best bid for a sell) and checks it against the policy,
// with the decision bar's close as the independent reference price. An error
// means no check could be made (no quote); the caller records nothing so a
// re-run can try again, exactly like an order error.
func checkRisk(p risk.Policy, ex dailyexec.Exchange, led *ledger, rec *record, side string, qty float64, pos position) (*riskInfo, error) {
	q, err := ex.Ticker(rec.Book)
	if err != nil {
		return nil, fmt.Errorf("risk check: ticker: %w", err)
	}
	price := q.Ask
	if side == "sell" {
		price = q.Bid
	}
	if price <= 0 {
		return nil, fmt.Errorf("risk check: no usable %s price for %s (bid %v, ask %v)", side, rec.Book, q.Bid, q.Ask)
	}
	o := risk.Order{Book: rec.Book, Side: side, QtyBTC: qty, Price: price, RefPrice: rec.Decision.Close}
	s := risk.State{PositionBTC: pos.BTC, OrdersToday: legsOn(led, rec.Book, rec.Decision.FillDate)}
	d := risk.Check(p, o, s)
	return &riskInfo{PolicyVersion: p.Version, Order: o, State: s, Allowed: d.Allowed, Findings: d.Findings}, nil
}

// legStarted reports whether Bitso already has an open order or trades under
// this leg's client ids, i.e. an earlier run began it and crashed.
func legStarted(ex dailyexec.Exchange, book, fillDate, side string) (bool, error) {
	mk, tk := dailyexec.OriginIDs(book, fillDate, side)
	open, err := ex.OpenOrders(book)
	if err != nil {
		return false, fmt.Errorf("open orders: %w", err)
	}
	for _, oo := range open {
		if oo.OriginID == mk || oo.OriginID == tk {
			return true, nil
		}
	}
	for _, id := range []string{mk, tk} {
		tr, err := ex.TradesByOrigin(id)
		if err != nil {
			return false, fmt.Errorf("trades %s: %w", id, err)
		}
		if len(tr) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// legsOn counts the stage legs already traded for book on fillDate. With one
// ledger line per decision day it is normally 0; it is what the per-day order
// limit is checked against.
func legsOn(l *ledger, book, fillDate string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, r := range l.entries {
		if r.Book == book && r.Stage != nil && r.Stage.Leg != nil && r.Decision.FillDate == fillDate {
			n++
		}
	}
	return n
}
