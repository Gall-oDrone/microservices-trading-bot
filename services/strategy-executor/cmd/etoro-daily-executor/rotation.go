package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/broker/etorobroker"
	"bitso-trading-platform/shared/pkg/cfdsim"
	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/etorodaily"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/shared/pkg/rotation"
)

// rotationSpec is the frozen configuration of a dual-momentum cell
// (FORWARD-TEST-PREREGISTRATION-DUAL-MOMENTUM-ETF-2026-10-21.md): one
// position at a time across Books, decided on month ends only. The per-book
// demo machinery (runDemo: reconcile, idempotent order, risk check, halt,
// ledger line) is reused unchanged; what differs is that the per-book
// signal comes from one joint target, that the books are run in a fixed
// order (closes before opens), and that the paper account is the joint
// account, recorded identically on every book's line.
type rotationSpec struct {
	Name         string     // rule name recorded in Decision.Rule ("dm_12_1")
	Books        []instSpec // in asset order; rotation targets index this slice
	Lookback     int
	Skip         int
	ForwardStart string
	Prereg       string
	Draft        bool
}

// frozenRotations by -strategy name.
var frozenRotations = map[string]rotationSpec{
	// Frozen 2026-10-21: 12-1 dual momentum between QQQ and SPY, cash
	// otherwise, 0 % hurdle; costs are the ETF cell's (same capture).
	"dm_12_1": {
		Name:         "dm_12_1",
		Books:        []instSpec{withPrereg(frozenSpecs["QQQ"], rotPrereg), withPrereg(frozenSpecs["SPY"], rotPrereg)},
		Lookback:     252,
		Skip:         21,
		ForwardStart: "2026-11-02",
		Prereg:       rotPrereg,
	},
}

const rotPrereg = "FORWARD-TEST-PREREGISTRATION-DUAL-MOMENTUM-ETF-2026-10-21.md"

func withPrereg(s instSpec, prereg string) instSpec {
	s.Prereg = prereg
	s.ForwardStart = "2026-11-02"
	return s
}

func rotationNames() []string {
	out := make([]string, 0, len(frozenRotations))
	for k := range frozenRotations {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// rotationDecision is the joint rule's state at the latest closed bar.
type rotationDecision struct {
	barDate, fillDate string
	target, prev      int // asset index or rotation.Flat
	decisionDay       bool
	momentum          map[string]float64
	bars              [][]dailyrule.Bar // aligned
	want              []int
	// preWindow: the fill date is before ForwardStart. The paper account is
	// in cash until the window's first fill (pre-registration §2), so every
	// book's signal is flat and nothing is opened; Target still records the
	// rule's target in force.
	preWindow bool
}

func (rs rotationSpec) targetName(t int) string {
	if t < 0 {
		return "cash"
	}
	return rs.Books[t].Symbol
}

// decideRotation aligns the books' bars, checks freshness on each, and
// applies the frozen rule. The last aligned bar must be every book's last
// bar (a book missing the latest day would silently shift the decision).
func (rs rotationSpec) decideRotation(rowsByBook [][]bitsodaily.Row, now time.Time) (rotationDecision, error) {
	var d rotationDecision
	series := make([][]dailyrule.Bar, len(rowsByBook))
	for k, rows := range rowsByBook {
		if err := etorodaily.CheckFresh(rows, now); err != nil {
			return d, fmt.Errorf("%s: %w", rs.Books[k].Symbol, err)
		}
		series[k] = etorodaily.Bars(rows)
	}
	al := rotation.Align(series...)
	n := len(al[0])
	need := rs.Lookback + rs.Skip + 2
	if n < need {
		return d, fmt.Errorf("need at least %d aligned bars, have %d", need, n)
	}
	last := al[0][n-1].Date
	for k, s := range series {
		if !s[len(s)-1].Date.Equal(last) {
			return d, fmt.Errorf("%s's last bar %s is not the aligned last bar %s", rs.Books[k].Symbol, s[len(s)-1].Date.Format("2006-01-02"), last.Format("2006-01-02"))
		}
	}
	pts := rotation.Evaluate(al, rotation.Params{Lookback: rs.Lookback, Skip: rs.Skip})
	if !pts[n-1].Warm || !pts[n-2].Warm {
		return d, fmt.Errorf("rule not warm at %s", last.Format("2006-01-02"))
	}
	d.bars = al
	d.want = make([]int, n)
	for i := range pts {
		d.want[i] = pts[i].Target
	}
	d.barDate = last.Format("2006-01-02")
	d.fillDate = mktcal.NextTradingDay(last).Format("2006-01-02")
	d.target, d.prev = pts[n-1].Target, pts[n-2].Target
	d.decisionDay = pts[n-1].Decision
	d.preWindow = d.fillDate < rs.ForwardStart
	// Momentum at the latest decision bar (today's if it decided).
	for i := n - 1; i >= 0; i-- {
		if pts[i].Decision {
			d.momentum = map[string]float64{}
			for k, m := range pts[i].Momentum {
				d.momentum[rs.Books[k].Symbol] = m
			}
			break
		}
	}
	return d, nil
}

// bookDecision is the per-book Decision derived from the joint target.
func (rs rotationSpec) bookDecision(d rotationDecision, k int) etoroledger.Decision {
	long, was := d.target == k, d.prev == k
	if d.preWindow {
		long, was = false, false
	}
	bd := etoroledger.Decision{
		BarDate: d.barDate, FillDate: d.fillDate, Close: d.bars[k][len(d.bars[k])-1].Close,
		Signal: sig(long), PrevSignal: sig(was), Action: "hold",
		Rule: rs.Name, DecisionDay: d.decisionDay, Momentum: d.momentum, Target: rs.targetName(d.target),
	}
	switch {
	case long && !was:
		bd.Action = "buy"
	case !long && was:
		bd.Action = "sell"
	}
	return bd
}

// paperRotation is the joint paper account from ForwardStart: the rule
// against holding the last book (SPY, the H2 benchmark), with the ETF costs.
func (rs rotationSpec) paperRotation(d rotationDecision) (etoroledger.Paper, error) {
	start, err := time.Parse("2006-01-02", rs.ForwardStart)
	if err != nil {
		return etoroledger.Paper{}, err
	}
	costs := make([]cfdsim.Costs, len(rs.Books))
	for k, b := range rs.Books {
		costs[k] = b.costs()
	}
	p := etoroledger.Paper{ForwardStart: rs.ForwardStart, Product: KindETF, FeeBps: rs.Books[0].FeeBps,
		SpreadRTBps: rs.Books[0].SpreadRTBps, CostsStatus: rs.Books[0].CostsStatus, Equity: 1, HoldEquity: 1, Position: "flat"}
	bars := d.bars[0]
	last := len(bars) - 1
	lo := sort.Search(len(bars), func(i int) bool { return !bars[i].Date.Before(start) })
	cur := rs.targetName(d.want[last])
	if lo > last {
		p.PendingAction = "hold"
		if d.want[last] >= 0 {
			p.PendingAction = "buy " + cur
		}
		return p, nil
	}
	if lo == 0 {
		return etoroledger.Paper{}, fmt.Errorf("no bar before forward start %s", rs.ForwardStart)
	}
	// The paper account is in cash before its first in-window fill (§2):
	// targets before the window are masked to cash.
	want := make([]int, len(d.want))
	for i := range want {
		want[i] = rotation.Flat
		if i >= lo-1 {
			want[i] = d.want[i]
		}
	}
	bench := len(rs.Books) - 1
	hold := make([]int, len(bars))
	for i := range hold {
		hold[i] = bench
	}
	tr := cfdsim.SimulateRotation(d.bars, want, lo, last, costs)
	hr := cfdsim.SimulateRotation(d.bars, hold, lo, last, costs)
	held := want[last-1]
	p.Started = true
	p.Days = last - lo + 1
	p.Position = rs.targetName(held)
	if held < 0 {
		p.Position = "flat"
	}
	p.RoundTrips = tr.RoundTrips
	p.Equity = 1 + tr.ReturnPct/100
	p.HoldEquity = 1 + hr.ReturnPct/100
	p.MaxDrawdown = tr.MaxDDPct / 100
	switch {
	case want[last] == held:
		p.PendingAction = "hold"
	case want[last] < 0:
		p.PendingAction = "sell " + rs.targetName(held)
	case held < 0:
		p.PendingAction = "buy " + cur
	default:
		p.PendingAction = "switch " + rs.targetName(held) + " -> " + cur
	}
	return p, nil
}

// runRotation decides the joint rule once and runs every book through the
// usual per-book tail, closes first, then the open, so a switch frees the
// cash before it is reused. One failed book does not stop the others: the
// next run re-plans each book from its ledger position (the rule's target
// is idempotent), which is also how a missed day is caught up.
func runRotation(ctx context.Context, o options, de demoEnv, rs rotationSpec, fetchFor func(instSpec) func() ([]bitsodaily.Row, etorodaily.Report, error), now time.Time, version, mode string) error {
	if rs.Draft && o.demo {
		return fmt.Errorf("%s is a draft rotation (%s): demo orders are not allowed until its pre-registration is frozen", rs.Name, rs.Prereg)
	}
	data := make([]bookData, len(rs.Books))
	rows, rows2 := make([][]bitsodaily.Row, len(rs.Books)), make([][]bitsodaily.Row, len(rs.Books))
	for k, s := range rs.Books {
		b, err := loadBook(o, s, fetchFor(s))
		if err != nil {
			return fmt.Errorf("%s: %w", s.Symbol, err)
		}
		data[k], rows[k], rows2[k] = b, b.rows, b.rows2
	}
	d, err := rs.decideRotation(rows, now)
	if err != nil {
		return err
	}
	d2, err := rs.decideRotation(rows2, now)
	if err != nil {
		return err
	}
	if d2.target != d.target || d2.prev != d.prev {
		return fmt.Errorf("the two reads of eToro's bars disagree on the rotation target: %s/%s vs %s/%s: not acting on it",
			rs.targetName(d.target), rs.targetName(d.prev), rs.targetName(d2.target), rs.targetName(d2.prev))
	}
	p, err := rs.paperRotation(d)
	if err != nil {
		return err
	}
	fmt.Printf("\n[%s] rotation %s: %s target %s (was %s)", rs.Name, d.barDate, map[bool]string{true: "DECISION DAY,", false: "no decision,"}[d.decisionDay],
		strings.ToUpper(rs.targetName(d.target)), rs.targetName(d.prev))
	if d.preWindow {
		fmt.Printf(" | before the forward window (%s): every book stays flat", rs.ForwardStart)
	}
	if d.momentum != nil {
		keys := make([]string, 0, len(d.momentum))
		for k := range d.momentum {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Printf(" | momentum")
		for _, k := range keys {
			fmt.Printf(" %s %+.2f%%", k, d.momentum[k]*100)
		}
	}
	fmt.Println()

	// Order: books the rule leaves (sell) first, then holds, then the buy.
	order := make([]int, len(rs.Books))
	for i := range order {
		order[i] = i
	}
	rank := func(k int) int {
		switch rs.bookDecision(d, k).Action {
		case "sell":
			return 0
		case "buy":
			return 2
		}
		return 1
	}
	sort.SliceStable(order, func(i, j int) bool { return rank(order[i]) < rank(order[j]) })

	var failed []string
	for _, k := range order {
		s := rs.Books[k]
		bd := rs.bookDecision(d, k)
		rec := &etoroledger.Record{
			Schema: etoroledger.Schema, CodeVersion: version, Mode: mode, Venue: etorobroker.Venue, Book: s.Book,
			Instrument: etoroledger.Instrument{Symbol: s.Symbol, ID: s.ID}, Prereg: rs.Prereg, Decision: bd, Paper: p,
			Candles: data[k].candleInfo(s),
		}
		printRecord(*rec, data[k].csvPath, s)
		if err := checkLedgerPrereg(de.led, s.Book, rs.Prereg); err != nil {
			return err
		}
		book := s.Book
		de.logf = func(format string, args ...any) {
			fmt.Printf("[%s] %s "+format+"\n", append([]any{book, time.Now().UTC().Format("15:04:05Z")}, args...)...)
		}
		if err := finishBook(ctx, o, de, s, rec, rows[k], d.bars[k], bd, mode); err != nil {
			fmt.Printf("\n[%s] FAILED: %v\n", s.Book, err)
			failed = append(failed, s.Book)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("rotation %s: %s failed (see above); the next run re-plans from the ledger", rs.Name, strings.Join(failed, ", "))
	}
	return nil
}
