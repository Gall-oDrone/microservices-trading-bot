package main

// Protection (plan ETORO-ASSISTED-TRADE-PROTECTION-PLAN-2026-10-19.md, phase
// C): the executor owns the safety of every position in its ledger, the
// rule's and the console's alike.
//
//   - C1: a rule open carries the disaster stop, max(4 x ATR%, 8 %) below
//     the ask, no take profit (the rule exits on its signal).
//   - C2: at a run that holds a long and plans no order, the stop is
//     ratcheted: candidate = decision close - k x ATR (k = 4 for a rule
//     position, the recorded profile's multiplier for a console position);
//     if it is above the held stop by at least 0.25 x ATR the position is
//     PATCHed and the day is recorded as action protect. Never lower, never
//     the take profit.
//   - C3: a position the ledger holds and the account does not is looked up
//     in the trading history; found, the day is recorded as action
//     venue_close (stopped_out / took_profit / venue_close by the close
//     rate), position flat, the history's P&L and fees. Not found yet, the
//     run stops without recording (eToro's history lags closes by minutes
//     to hours) and a re-run later resolves it.
//
// Deviation from the plan's C3, stated here so nobody looks for it: after a
// venue close the run does NOT re-open in the same run. The ledger keeps
// one line per instrument per decision day with one action, so the
// re-entry a rule "long" asks for happens at the next trading day's run,
// from flat. One day out of the market after a stop-out is accepted.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/stops"
)

// Protection profiles the executor writes (etoroledger.Protection.Profile).
const (
	profileDisaster = "disaster" // the stop set with a rule open, or first set on an unprotected position
	profileRatchet  = "ratchet"  // a stop raised at a later run
)

// venueCloseTolerance is how close (as a fraction of the rate) the history's
// close rate must be to the stop or the target to be reported as that exact
// fill; a close beyond the rate is the same event through a gap, reported
// with the gap. A close strictly between the two is something else (a
// manual close in the app, a margin close-out).
const venueCloseTolerance = 0.0005

// fallbackBounds stand in when the broker cannot read the venue's rules
// (NSDQ100 x1 long, evidence 2026-10-10).
var fallbackBounds = stops.Bounds{MinStopLossPct: 0.01, MaxStopLossPct: 1, MaxTakeProfitPct: 10, Tick: 0.01}

// volatility is the ATR of the closed bars the rule decided on, or ok false
// when there are not enough bars.
func volatility(bars []dailyrule.Bar) (stops.Volatility, bool) {
	sb := make([]stops.Bar, 0, len(bars))
	for _, b := range bars {
		sb = append(sb, stops.Bar{High: b.High, Low: b.Low, Close: b.Close})
	}
	v, err := stops.Measure(sb, stops.DefaultATRPeriod, stops.DefaultRegimeLookback)
	if err != nil {
		return stops.Volatility{}, false
	}
	return v, true
}

// bounds reads the venue's stop rules for in, or the fallback.
func (e demoEnv) bounds(ctx context.Context, in broker.Instrument) (stops.Bounds, string) {
	lr, ok := e.b.(broker.ProtectionLimitsReader)
	if !ok {
		return fallbackBounds, "fallback (the broker cannot read the venue's bounds)"
	}
	pl, err := lr.ProtectionLimits(ctx, in, broker.Long, 1)
	if err != nil {
		return fallbackBounds, "fallback (venue bounds: " + err.Error() + ")"
	}
	return stops.Bounds{MinStopLossPct: pl.MinStopLossPct, MaxStopLossPct: pl.MaxStopLossPct, MaxTakeProfitPct: pl.MaxTakeProfitPct, Tick: pl.Tick}, "venue"
}

// disasterStop computes the stop a rule open carries (C1): max(4 x ATR%,
// 8 %) below the ask, through the shared guards (noise floor, the policy's
// caps, the venue's bounds, tick rounding). Without enough bars the 8 %
// floor applies. Infeasible levels come back with Feasible false; the
// caller then opens with no stop and lets the risk policy decide.
func (e demoEnv) disasterStop(ctx context.Context, in broker.Instrument, book string, q broker.Quote, equity float64, bars []dailyrule.Bar) (stops.Levels, *stops.Volatility, []string) {
	var notes []string
	bl := e.policy.For(book)
	bnd, src := e.bounds(ctx, in)
	if src != "venue" {
		notes = append(notes, "stop bounds: "+src)
	}
	input := stops.Input{Entry: q.Ask, Mark: q.Bid, Amount: e.amount, SpreadPct: spreadPct(q), Equity: equity, Bounds: bnd,
		Limits: stops.Limits{MinStopLossPct: bl.MinStopLossPct, MaxStopLossPct: bl.MaxStopLossPct, MaxLossPctEquity: bl.MaxLossPctEquity}}
	var vol *stops.Volatility
	pct := stops.DisasterFloorPct
	if v, ok := volatility(bars); ok {
		vol, input.ATR = &v, v.ATR
		pct = stops.DisasterStopPct(v.ATRPct)
	} else {
		notes = append(notes, fmt.Sprintf("no ATR from %d bars: the %.0f%% floor stands in for the disaster stop", len(bars), stops.DisasterFloorPct*100))
	}
	l, err := stops.Build(input, pct, 0)
	if err != nil {
		return stops.Levels{Reason: err.Error()}, vol, append(notes, "disaster stop: "+err.Error())
	}
	l.Profile = profileDisaster
	return l, vol, notes
}

// stopMultiplier is the ATR multiple the ratchet trails pos with: the
// disaster multiple for a rule position (or any position whose opening
// line the ledger does not have), the recorded profile's for a console
// position. ok is false for a custom console stop, which is left alone.
func stopMultiplier(recs []etoroledger.Record, pos etoroledger.Position) (k float64, profile string, ok bool) {
	for _, r := range recs {
		d := r.Demo
		if d == nil || d.Action != etoroledger.ActionOpen || d.PositionAfter.PositionID != pos.PositionID {
			continue
		}
		if !r.IsUI() || d.Protection == nil {
			break
		}
		p, err := stops.ParseProfile(d.Protection.Profile)
		if err != nil || p == stops.Custom {
			return 0, d.Protection.Profile, false
		}
		if m, found := stops.ProfileMultipliers(p); found {
			return m.SL, string(p), true
		}
		return 0, d.Protection.Profile, false
	}
	return stops.DisasterATRMult, profileDisaster, true
}

// ratchet (C2) raises the held long's stop when the decision close minus
// k x ATR is at least 0.25 x ATR above the stop the account holds. It
// records the outcome on d (action protect with the edit, or notes) and
// returns an error the operator must see when the venue refused or did
// not confirm the edit. It never lowers a stop and never touches the take
// profit. The account's resting rates are copied onto PositionAfter in
// every case, so the ledger tells the truth about what the venue holds.
func (e demoEnv) ratchet(ctx context.Context, in broker.Instrument, book string, rec *etoroledger.Record, d *etoroledger.Demo, held broker.Position, bars []dailyrule.Bar) error {
	pos := d.PositionBefore
	after := pos
	// What the venue holds decides, and the ledger follows it.
	current := 0.0
	if stops.HasStop(held.StopLoss) {
		current = held.StopLoss
		after.StopLossRate, after.StopType = held.StopLoss, held.StopType
		if after.StopType == "" {
			after.StopType = broker.StopFixed
		}
		if pos.StopLossRate > 0 && math.Abs(held.StopLoss-pos.StopLossRate) > 1e-9 {
			d.Notes = append(d.Notes, fmt.Sprintf("stop read from the account: %.2f (ledger had %.2f)", held.StopLoss, pos.StopLossRate))
		}
	} else {
		after.StopLossRate, after.StopType = 0, ""
		if pos.StopLossRate > 0 {
			d.Notes = append(d.Notes, fmt.Sprintf("protection drift: the ledger recorded stop %.2f but the account holds none; setting it again", pos.StopLossRate))
		}
	}
	if stops.HasStop(held.TakeProfit) {
		after.TakeProfitRate = held.TakeProfit
	} else {
		after.TakeProfitRate = 0
	}
	d.PositionAfter = after

	k, profile, ok := stopMultiplier(e.led.Records(), pos)
	if !ok {
		d.Notes = append(d.Notes, fmt.Sprintf("ratchet: console position with a %s stop is left as the operator set it", profile))
		return nil
	}
	v, vok := volatility(bars)
	if !vok {
		d.Notes = append(d.Notes, fmt.Sprintf("ratchet: no ATR from %d bars; the stop stays at %.2f", len(bars), current))
		return nil
	}
	q, err := e.b.Quote(ctx, in)
	if err != nil {
		d.Notes = append(d.Notes, "ratchet: quote: "+err.Error()+"; the stop stays")
		return nil
	}
	d.Quote = &etoroledger.Quote{Bid: q.Bid, Ask: q.Ask, At: q.At.UTC().Format(time.RFC3339), SpreadBps: q.SpreadBps()}

	bnd, _ := e.bounds(ctx, in)
	tick := bnd.Tick
	if tick <= 0 {
		tick = 0.01
	}
	candidate := rec.Decision.Close - k*v.ATR
	// The noise floor: never inside the venue's minimum distance or the
	// spread, measured from the mark.
	floor := math.Max(bnd.MinStopLossPct, 2*spreadPct(q)+stops.NoiseFloorPct)
	if q.Bid > 0 && candidate > q.Bid*(1-floor) {
		candidate = q.Bid * (1 - floor)
	}
	candidate = math.Floor(candidate/tick+1e-9) * tick
	newStop, raise := stops.Ratchet(current, candidate, stops.DefaultRatchetStepATR*v.ATR, q.Bid)
	if !raise {
		if current > 0 {
			d.Notes = append(d.Notes, fmt.Sprintf("ratchet: candidate %.2f (%s %.1fxATR below close %.2f) is not %.2fxATR above the held stop %.2f; unchanged",
				candidate, profile, k, rec.Decision.Close, stops.DefaultRatchetStepATR, current))
		} else {
			d.Notes = append(d.Notes, fmt.Sprintf("ratchet: no valid stop below the mark %.2f (candidate %.2f); position stays unprotected", q.Bid, candidate))
		}
		return nil
	}
	pe, ok := e.b.(broker.ProtectionEditor)
	if !ok {
		d.Notes = append(d.Notes, fmt.Sprintf("ratchet: would raise the stop to %.2f but this broker cannot edit protection", newStop))
		return nil
	}
	ref := etoroledger.ClientRef(book, rec.Decision.FillDate, etoroledger.ActionProtect)
	now := e.now().UTC()
	o := &etoroledger.Order{Kind: etoroledger.ActionProtect, ClientRef: ref, RequestID: etoro.RequestIDFor(ref), PositionIDs: []string{pos.PositionID},
		IntentAt: now, Started: now}
	prot := &etoroledger.Protection{Profile: profileRatchet, ATR: v.ATR, ATRPct: v.ATRPct, StopLossBefore: current, TakeProfitBefore: after.TakeProfitRate,
		Reason: fmt.Sprintf("daily ratchet: close %.2f - %.1f x ATR %.2f", rec.Decision.Close, k, v.ATR)}
	if current <= 0 {
		prot.Profile = profileDisaster
		prot.Reason = fmt.Sprintf("first stop on a held position: close %.2f - %.1f x ATR %.2f", rec.Decision.Close, k, v.ATR)
	}
	e.logf("protect: %s position %s stop %.2f -> %.2f (%s, ref %s)", in.Symbol, pos.PositionID, current, newStop, prot.Profile, ref)
	res, err := pe.SetProtection(ctx, broker.ProtectionRequest{Instrument: in, PositionID: pos.PositionID, StopLoss: newStop, StopType: broker.StopFixed, ClientRef: ref})
	o.Finished = e.now().UTC()
	o.OrderID, o.Status, o.Error, o.ErrorCode = res.OperationID, string(res.Status), res.Error, res.ErrorCode
	prot.OperationID = res.OperationID
	d.Order, d.Protection = o, prot
	switch {
	case err == nil && (res.Status == broker.StatusExecuted || res.Status == broker.StatusPending):
		d.Action = etoroledger.ActionProtect
		after.StopLossRate, after.StopType = newStop, broker.StopFixed
		if res.Status == broker.StatusExecuted && stops.HasStop(res.Position.StopLoss) {
			after.StopLossRate = res.Position.StopLoss
		}
		d.PositionAfter = after
		prot.MaxLossUSD = after.Units * (after.OpenRate - after.StopLossRate)
		if res.Status == broker.StatusPending {
			d.Notes = append(d.Notes, "the venue accepted the stop but the position did not show it before the adapter's wait ran out; the next run reads it back")
		}
		return nil
	case errors.Is(err, broker.ErrPositionNotOpen):
		// Closed between the portfolio read and the PATCH: the next run
		// finds it in the history (C3).
		if o.Error == "" {
			o.Error = err.Error()
		}
		if o.Status == "" {
			o.Status = string(broker.StatusRejected)
		}
		d.Notes = append(d.Notes, "ratchet: the position was no longer open when the stop was sent; the next run reads the trading history")
		return fmt.Errorf("protect %s: %v (recorded; the position closed meanwhile)", ref, err)
	default:
		if o.Error == "" && err != nil {
			o.Error = err.Error()
		}
		if o.Status == "" {
			o.Status = string(broker.StatusUnknown)
		}
		return fmt.Errorf("protect %s: stop %.2f not confirmed: %v (recorded; the position keeps its previous stop %.2f)", ref, newStop, errOrStatus(err, res), current)
	}
}

func errOrStatus(err error, res broker.ProtectionResult) string {
	if err != nil {
		return err.Error()
	}
	if res.Error != "" {
		return string(res.Status) + ": " + res.Error
	}
	return string(res.Status)
}

// venueClose (C3) explains a position the ledger holds and the account does
// not: it is looked up in the trading history and, when found, recorded as
// action venue_close with the fill, the kind and the history's P&L. ok is
// false when the history does not list it yet (nothing recorded; re-run).
func (e demoEnv) venueClose(ctx context.Context, d *etoroledger.Demo, pos etoroledger.Position) (ok bool, err error) {
	hr, isHR := e.b.(broker.HistoryReader)
	if !isHR {
		return false, nil
	}
	since := e.now().Add(-300 * 24 * time.Hour)
	if t, perr := time.Parse(time.RFC3339, pos.OpenedAt); perr == nil && t.After(since) {
		since = t.Add(-24 * time.Hour)
	}
	cps, err := hr.ClosedPositions(ctx, since)
	if err != nil {
		return false, fmt.Errorf("trading history: %w", err)
	}
	var c *broker.ClosedPosition
	for i := range cps {
		if cps[i].ID == pos.PositionID {
			c = &cps[i]
			break
		}
	}
	if c == nil {
		return false, nil
	}
	sl, tp := c.StopLoss, c.TakeProfit
	if !stops.HasStop(sl) {
		sl = pos.StopLossRate
	}
	if !stops.HasStop(tp) {
		tp = pos.TakeProfitRate
	}
	vc := &etoroledger.VenueClose{Kind: etoroledger.VenueCloseOther, StopLossRate: sl, TakeProfitRate: tp, CloseRate: c.CloseRate,
		ClosedAt: c.ClosedAt.UTC().Format(time.RFC3339), NetProfit: c.NetProfit, Fees: c.Fees}
	var why string
	switch {
	case sl > 0 && c.CloseRate <= sl*(1+venueCloseTolerance):
		vc.Kind = etoroledger.VenueCloseStoppedOut
		vc.GapBps = (sl - c.CloseRate) / sl * 1e4
		why = fmt.Sprintf("close rate %.2f at the stop %.2f", c.CloseRate, sl)
		if vc.GapBps > venueCloseTolerance*1e4 {
			why = fmt.Sprintf("close rate %.2f is %.1f bps through the stop %.2f (gap)", c.CloseRate, vc.GapBps, sl)
		}
	case tp > 0 && c.CloseRate >= tp*(1-venueCloseTolerance):
		vc.Kind = etoroledger.VenueCloseTookProfit
		vc.GapBps = (tp - c.CloseRate) / tp * 1e4
		why = fmt.Sprintf("close rate %.2f at the target %.2f", c.CloseRate, tp)
		if -vc.GapBps > venueCloseTolerance*1e4 {
			why = fmt.Sprintf("close rate %.2f is %.1f bps beyond the target %.2f (gap)", c.CloseRate, -vc.GapBps, tp)
		}
	default:
		why = fmt.Sprintf("close rate %.2f is between the stop %.2f and the target %.2f: not ours and not a protective order (closed in the app, or a margin close-out)", c.CloseRate, sl, tp)
	}
	d.Action = etoroledger.ActionVenueClose
	d.VenueClose = vc
	d.PositionAfter = etoroledger.Flat()
	d.Order = &etoroledger.Order{Kind: etoroledger.ActionVenueClose, Status: string(broker.StatusExecuted), PositionIDs: []string{c.ID},
		AvgPrice: c.CloseRate, Units: c.Units, Amount: c.Amount, Fees: c.Fees, Started: e.now().UTC(), Finished: c.ClosedAt.UTC()}
	d.Notes = append(d.Notes, "closed by eToro: "+why+" -> "+vc.Kind)
	e.logf("demo: position %s was closed by eToro at %.2f (%s): %s; net %.2f, fees %.2f", c.ID, c.CloseRate, c.ClosedAt.UTC().Format(time.RFC3339), vc.Kind, c.NetProfit, c.Fees)
	return true, nil
}

func spreadPct(q broker.Quote) float64 {
	if m := q.Mid(); m > 0 && q.Ask >= q.Bid {
		return (q.Ask - q.Bid) / m
	}
	return 0
}

// describeLevels is the one-line summary of a stop the run logs.
func describeLevels(l stops.Levels) string {
	s := fmt.Sprintf("stop %.2f (%.2f%% below entry, max loss %.2f USD)", l.SLRate, l.SLPct*100, l.MaxLossUSD)
	if l.SLATR > 0 {
		s += fmt.Sprintf(", %.1f x ATR", l.SLATR)
	}
	if len(l.Clamps) > 0 {
		s += "; clamps: " + strings.Join(l.Clamps, "; ")
	}
	return s
}
