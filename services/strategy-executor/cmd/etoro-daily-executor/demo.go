package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/shared/pkg/risk"
)

// demoEnv is what a demo leg needs. Every venue call goes through b, so
// tests drive the whole flow with a fake broker.
type demoEnv struct {
	b       broker.Broker
	led     *etoroledger.Ledger
	intents string // etoroledger.IntentDir(ledger)
	policy  risk.Policy
	halt    *risk.HaltState
	amount  float64
	now     func() time.Time
	logf    func(format string, args ...any)
}

// errNotRecorded marks a failure after which nothing was written, so a
// re-run the same day retries (or resumes) the leg.
type errNotRecorded struct{ err error }

func (e errNotRecorded) Error() string {
	return e.err.Error() + " (nothing recorded; re-run today to retry or resume)"
}
func (e errNotRecorded) Unwrap() error { return e.err }

func notRecorded(format string, args ...any) error {
	return errNotRecorded{fmt.Errorf(format, args...)}
}

// runDemo brings the book's demo position to the rule's target and records
// the day. It returns a non-nil error for anything the operator must see:
// either nothing was recorded (errNotRecorded) or the day was recorded as
// blocked / rejected.
//
// Before acting it reconciles the ledger's position with the account: the
// executor is the only actor on its instruments, so any other position on
// them is an error, unless an intent shows it is this leg's own order from an
// earlier, interrupted run, which is then resumed with the same ClientRef.
func runDemo(ctx context.Context, e demoEnv, s instSpec, rec *etoroledger.Record) error {
	in := broker.Instrument{Venue: e.b.Venue(), Symbol: s.Symbol, ID: s.ID}
	pos := e.led.LastDemoPosition(s.Book)
	target := rec.Decision.Signal
	action := etoroledger.ActionNone
	switch {
	case target == "long" && pos.State == "flat":
		action = etoroledger.ActionOpen
	case target == "flat" && pos.State == "long":
		action = etoroledger.ActionClose
	}
	d := &etoroledger.Demo{Env: "demo", Target: target, Action: action, PositionBefore: pos, PositionAfter: pos}

	all, err := e.b.Positions(ctx)
	if err != nil {
		return notRecorded("positions: %v", err)
	}
	acct, err := e.b.Account(ctx)
	if err != nil {
		return notRecorded("account: %v", err)
	}
	d.Account = &etoroledger.Account{Currency: acct.Currency, Cash: acct.Cash, Equity: acct.Equity, Exposure: exposure(all)}
	mine := onInstrument(all, s.ID)

	ref := ""
	if action != etoroledger.ActionNone {
		ref = etoroledger.ClientRef(s.Book, rec.Decision.FillDate, action)
	}
	intent, started, err := etoroledger.LoadIntent(e.intents, ref)
	if action != etoroledger.ActionNone && err != nil {
		return notRecorded("intent: %v", err)
	}

	// Reconcile the ledger with the account before doing anything.
	switch pos.State {
	case "flat":
		if len(mine) > 0 && !(action == etoroledger.ActionOpen && started) {
			return notRecorded("ledger says flat but the account holds %s on %s: resolve by hand, nothing sent", ids(mine), s.Symbol)
		}
	case "long":
		var other []broker.Position
		var held *broker.Position
		for i := range mine {
			if mine[i].ID == pos.PositionID {
				held = &mine[i]
			} else {
				other = append(other, mine[i])
			}
		}
		if len(other) > 0 {
			return notRecorded("account holds %s on %s besides the executor's position %s: resolve by hand, nothing sent", ids(other), s.Symbol, pos.PositionID)
		}
		if held == nil && !(action == etoroledger.ActionClose && started) {
			return notRecorded("ledger says long %s but the account no longer holds it (closed outside the executor?): resolve by hand", pos.PositionID)
		}
		if held != nil {
			d.FinancingToDate = held.Fees
		}
	}

	if action == etoroledger.ActionNone {
		e.logf("demo: rule says %s, executor holds %s -> nothing to do", target, pos.State)
		return e.record(rec, d)
	}
	if action == etoroledger.ActionClose && started && len(mine) == 0 {
		// An earlier run's close went through: nothing will be sent, so
		// there is nothing to price or risk-check.
		o := &etoroledger.Order{Kind: action, ClientRef: ref, RequestID: intent.RequestID, IntentAt: intent.IntentAt, Started: e.now().UTC(), Resumed: true}
		d.Order = o
		bo, err := e.closedFromHistory(ctx, pos, ref, intent)
		o.Finished = e.now().UTC()
		fill(o, bo)
		if err != nil {
			return notRecorded("close %s: %v", ref, err)
		}
		o.FillVsCloseBps = costBps("sell", bo.AvgPrice, rec.Decision.Close)
		d.PositionAfter = etoroledger.Flat()
		d.Notes = append(d.Notes, "close resumed from an earlier run; fill read from the trading history")
		e.logf("demo: close %s resumed: closed at %.2f per the trading history", ref, bo.AvgPrice)
		return e.record(rec, d)
	}

	q, err := e.b.Quote(ctx, in)
	if err != nil {
		return notRecorded("quote: %v", err)
	}
	d.Quote = &etoroledger.Quote{Bid: q.Bid, Ask: q.Ask, At: q.At.UTC().Format(time.RFC3339), SpreadBps: q.SpreadBps()}
	now := e.now().UTC()
	marketClosed := !mktcal.IsOpen(now) || now.In(mktcal.NewYork).Format("2006-01-02") != rec.Decision.FillDate

	ro := risk.Order{Book: s.Book, RefPrice: rec.Decision.Close, Leverage: 1}
	rs := risk.State{OrdersToday: etoroledger.LegsOn(e.led.Records(), s.Book, rec.Decision.FillDate),
		Equity: acct.Equity, Exposure: d.Account.Exposure, MarketClosed: marketClosed}
	if action == etoroledger.ActionOpen {
		ro.Side, ro.Price = "buy", q.Ask
		if q.Ask > 0 {
			ro.QtyBTC = e.amount / q.Ask
		}
	} else {
		ro.Side, ro.Price, ro.QtyBTC = "sell", q.Bid, pos.Units
		rs.PositionBTC = pos.Units
	}
	dec := risk.Check(e.policy, ro, rs)
	d.Risk = &etoroledger.RiskCheck{PolicyVersion: e.policy.Version, Order: ro, State: rs, Allowed: dec.Allowed, Findings: dec.Findings, Halt: e.halt}
	for _, f := range dec.Findings {
		e.logf("risk: %s %s: %s", f.Severity, f.Rule, f.Message)
	}
	if !dec.Allowed {
		if onlyMarketClosed(dec.Findings) {
			// Not a decision to skip the day: the run was outside the cash
			// session. Nothing is recorded, so an in-session re-run trades
			// (or resumes an interrupted leg).
			return notRecorded("%s needs the NYSE cash session on %s (now %s New York)", action, rec.Decision.FillDate, now.In(mktcal.NewYork).Format("2006-01-02 15:04"))
		}
		if started {
			return notRecorded("%s blocked by risk policy %s, but an earlier run already sent %s: resolve by hand", action, e.policy.Version, ref)
		}
		d.Action = etoroledger.ActionBlocked
		if err := e.record(rec, d); err != nil {
			return err
		}
		return fmt.Errorf("%s on %s blocked by risk policy %s (no order sent; recorded as blocked)", action, s.Symbol, e.policy.Version)
	}

	if action == etoroledger.ActionOpen {
		if c, err := e.b.PreviewOpen(ctx, broker.OpenRequest{Instrument: in, Side: broker.Long, Amount: e.amount, Leverage: 1, ClientRef: ref}); err == nil {
			d.Costs = &etoroledger.Costs{Spread: c.Spread, Markup: c.Markup, TransactionFee: c.TransactionFee, OvernightFee: c.OvernightFee, WeekendFee: c.WeekendFee}
		} else {
			d.Notes = append(d.Notes, "cost preview failed: "+err.Error())
		}
	}

	intent, started, err = etoroledger.RecordIntent(e.intents, etoroledger.Intent{
		ClientRef: ref, RequestID: etoro.RequestIDFor(ref), Kind: action, Book: s.Book, Instrument: s.ID,
		BarDate: rec.Decision.BarDate, FillDate: rec.Decision.FillDate, Amount: e.amount, PositionID: pos.PositionID, IntentAt: now,
	})
	if err != nil {
		return notRecorded("intent: %v", err)
	}
	o := &etoroledger.Order{Kind: action, ClientRef: ref, RequestID: intent.RequestID, IntentAt: intent.IntentAt, Started: now, Resumed: started}
	d.Order = o
	if started {
		e.logf("demo: resuming %s (intent of %s)", ref, intent.IntentAt.Format(time.RFC3339))
	}

	var bo broker.Order
	var oerr error
	if action == etoroledger.ActionOpen {
		e.logf("demo: open %s %.2f USD x1 (ask %.2f, ref %s)", s.Symbol, e.amount, q.Ask, ref)
		bo, oerr = e.b.Open(ctx, broker.OpenRequest{Instrument: in, Side: broker.Long, Amount: e.amount, Leverage: 1, ClientRef: ref, IntentAt: intent.IntentAt})
	} else {
		bo, oerr = e.closeLeg(ctx, in, pos, ref, intent, started, mine)
	}
	o.Finished = e.now().UTC()
	fill(o, bo)

	switch {
	case errors.Is(oerr, broker.ErrOutcomeUnknown):
		return notRecorded("%s %s: outcome unknown: %v", action, ref, oerr)
	case oerr != nil:
		// A definite rejection. Recorded with the position unchanged; the
		// next trading day plans from the target again with a new ref.
		if o.Error == "" {
			o.Error = oerr.Error()
		}
		if err := e.record(rec, d); err != nil {
			return err
		}
		return fmt.Errorf("%s %s rejected: %v (recorded)", action, ref, oerr)
	case bo.Status != broker.StatusExecuted:
		return notRecorded("%s %s is %s at eToro after the adapter's wait", action, ref, bo.Status)
	}

	if action == etoroledger.ActionOpen {
		if len(bo.PositionIDs) != 1 {
			return notRecorded("open %s executed with %d positions %v: resolve by hand", ref, len(bo.PositionIDs), bo.PositionIDs)
		}
		d.PositionAfter = etoroledger.Position{State: "long", PositionID: bo.PositionIDs[0], Units: bo.Units, Amount: bo.Amount,
			OpenRate: bo.AvgPrice, OpenedAt: bo.At.UTC().Format(time.RFC3339)}
		o.FillVsMidBps = costBps("buy", bo.AvgPrice, q.Mid())
		o.FillVsCloseBps = costBps("buy", bo.AvgPrice, rec.Decision.Close)
	} else {
		d.PositionAfter = etoroledger.Flat()
		o.FillVsMidBps = costBps("sell", bo.AvgPrice, q.Mid())
		o.FillVsCloseBps = costBps("sell", bo.AvgPrice, rec.Decision.Close)
	}
	e.logf("demo: %s %s executed: order %s positions %v units %.6f avg %.2f amount %.2f (vs mid %+.1f bps, vs decision close %+.1f bps)",
		action, s.Symbol, o.OrderID, o.PositionIDs, o.Units, o.AvgPrice, o.Amount, o.FillVsMidBps, o.FillVsCloseBps)
	return e.record(rec, d)
}

// closeLeg closes the executor's position. On a resumed close whose position
// is already gone, or a close eToro refuses because the position is not open,
// the earlier attempt closed it: the fill is read from the trading history.
func (e demoEnv) closeLeg(ctx context.Context, in broker.Instrument, pos etoroledger.Position, ref string, intent etoroledger.Intent, started bool, mine []broker.Position) (broker.Order, error) {
	stillOpen := false
	for _, m := range mine {
		if m.ID == pos.PositionID {
			stillOpen = true
		}
	}
	if started && !stillOpen {
		e.logf("demo: %s already closed by the earlier attempt; reading the trading history", pos.PositionID)
		return e.closedFromHistory(ctx, pos, ref, intent)
	}
	e.logf("demo: close %s position %s (%.6f units, ref %s)", in.Symbol, pos.PositionID, pos.Units, ref)
	bo, err := e.b.Close(ctx, broker.CloseRequest{Instrument: in, PositionID: pos.PositionID, ClientRef: ref})
	if errors.Is(err, broker.ErrPositionNotOpen) {
		e.logf("demo: eToro says %s is not open; reading the trading history", pos.PositionID)
		return e.closedFromHistory(ctx, pos, ref, intent)
	}
	if err != nil && started {
		// A resend of a close that eToro may still be processing.
		return bo, errors.Join(broker.ErrOutcomeUnknown, err)
	}
	return bo, err
}

func (e demoEnv) closedFromHistory(ctx context.Context, pos etoroledger.Position, ref string, intent etoroledger.Intent) (broker.Order, error) {
	hr, ok := e.b.(broker.HistoryReader)
	if !ok {
		return broker.Order{ClientRef: ref, Status: broker.StatusUnknown}, fmt.Errorf("%w: %s is closed but the broker has no trading history", broker.ErrOutcomeUnknown, pos.PositionID)
	}
	since := intent.IntentAt.Add(-24 * time.Hour)
	if since.IsZero() || e.now().Sub(since) > 300*24*time.Hour {
		since = e.now().Add(-300 * 24 * time.Hour)
	}
	cps, err := hr.ClosedPositions(ctx, since)
	if err != nil {
		return broker.Order{ClientRef: ref, Status: broker.StatusUnknown}, fmt.Errorf("%w: trading history: %v", broker.ErrOutcomeUnknown, err)
	}
	for _, c := range cps {
		if c.ID == pos.PositionID {
			return broker.Order{ClientRef: ref, Status: broker.StatusExecuted, PositionIDs: []string{c.ID},
				AvgPrice: c.CloseRate, Units: c.Units, Amount: c.Amount, Fees: c.Fees, At: c.ClosedAt}, nil
		}
	}
	return broker.Order{ClientRef: ref, Status: broker.StatusUnknown},
		fmt.Errorf("%w: %s is not open and not in the trading history yet", broker.ErrOutcomeUnknown, pos.PositionID)
}

func (e demoEnv) record(rec *etoroledger.Record, d *etoroledger.Demo) error {
	rec.Demo = d
	rec.RecordedAt = e.now().UTC().Format(time.RFC3339)
	if err := e.led.Append(*rec); err != nil {
		return err
	}
	e.logf("ledger: recorded (demo %s, holds %s)", d.Action, d.PositionAfter.State)
	return nil
}

func fill(o *etoroledger.Order, bo broker.Order) {
	o.OrderID, o.Status, o.PositionIDs = bo.OrderID, string(bo.Status), bo.PositionIDs
	o.AvgPrice, o.Units, o.Amount, o.Fees, o.SpreadCost = bo.AvgPrice, bo.Units, bo.Amount, bo.Fees, bo.SpreadCost
	o.Error, o.ErrorCode = bo.Error, bo.ErrorCode
	if o.Status == "" {
		o.Status = string(broker.StatusUnknown)
	}
}

// costBps is the fill's cost against ref in bps (positive = paid).
func costBps(side string, avg, ref float64) float64 {
	if avg <= 0 || ref <= 0 {
		return 0
	}
	if side == "buy" {
		return (avg - ref) / ref * 1e4
	}
	return (ref - avg) / ref * 1e4
}

func onInstrument(ps []broker.Position, id int64) []broker.Position {
	var out []broker.Position
	for _, p := range ps {
		if p.Instrument.ID == id {
			out = append(out, p)
		}
	}
	return out
}

// exposure is the account's total open notional (account currency).
func exposure(ps []broker.Position) float64 {
	t := 0.0
	for _, p := range ps {
		switch {
		case p.Exposure > 0:
			t += p.Exposure
		case p.Leverage > 1:
			t += p.Amount * float64(p.Leverage)
		default:
			t += p.Amount
		}
	}
	return t
}

func ids(ps []broker.Position) string {
	s := make([]string, len(ps))
	for i, p := range ps {
		s[i] = p.ID
	}
	return "position(s) " + strings.Join(s, ", ")
}

func onlyMarketClosed(fs []risk.Finding) bool {
	blocks := 0
	for _, f := range fs {
		if f.Severity != risk.Block {
			continue
		}
		if f.Rule != risk.RuleMarketClosed {
			return false
		}
		blocks++
	}
	return blocks > 0
}
