package main

import (
	"fmt"
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/cfdsim"
	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/etorodaily"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
)

// instSpec is the frozen per-instrument configuration of
// docs/backtest-readiness/FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md.
// The costs only drive the paper account recorded next to each decision;
// the forward test is judged by cmd/index-research -forward (§5), not by
// this executor.
type instSpec struct {
	Book         string
	Symbol       string
	ID           int64
	ForwardStart string  // first forward day: the paper's first fill is at its open
	SpreadRTBps  float64 // frozen round-trip spread (§3)
	OvernightBps float64 // frozen overnight fee per calendar night at x1 (§3)
	CostsStatus  string
	Prereg       string
}

const prereg = "FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md"

// The cost cells are PROVISIONAL until the weekday re-measure of 2026-10-12
// (pre-registration §3); they are replaced with the frozen values then.
var frozenSpecs = map[string]instSpec{
	"NSDQ100": {Book: "nsdq100", Symbol: "NSDQ100", ID: 28, ForwardStart: "2026-10-13",
		SpreadRTBps: 15.5, OvernightBps: 2.3, CostsStatus: "provisional (weekend capture 2026-10-10)", Prereg: prereg},
	"SPX500": {Book: "spx500", Symbol: "SPX500", ID: 27, ForwardStart: "2026-10-13",
		SpreadRTBps: 14.8, OvernightBps: 2.3, CostsStatus: "provisional (weekend capture 2026-10-10)", Prereg: prereg},
}

func specNames() []string {
	out := make([]string, 0, len(frozenSpecs))
	for k := range frozenSpecs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// smaDays is frozen by the pre-registration (§1).
const smaDays = 50

func sig(long bool) string {
	if long {
		return "long"
	}
	return "flat"
}

// decide applies the frozen rule to the closed NYSE-trading-day bars. The
// last bar must be the latest closed trading day at now (no stale data),
// and the rule must be warm on the last two bars.
func decide(rows []bitsodaily.Row, now time.Time) (etoroledger.Decision, []dailyrule.Bar, []dailyrule.Point, error) {
	if err := etorodaily.CheckFresh(rows, now); err != nil {
		return etoroledger.Decision{}, nil, nil, err
	}
	bars := etorodaily.Bars(rows)
	if len(bars) < smaDays+1 {
		return etoroledger.Decision{}, nil, nil, fmt.Errorf("need at least %d bars, have %d", smaDays+1, len(bars))
	}
	pts := dailyrule.Evaluate(bars, smaDays)
	last := len(bars) - 1
	if !pts[last].Warm || !pts[last-1].Warm {
		return etoroledger.Decision{}, nil, nil, fmt.Errorf("rule not warm at %s (a >%d-day gap in the last %d bars?)",
			bars[last].Date.Format("2006-01-02"), dailyrule.MaxGapDays, smaDays)
	}
	d := etoroledger.Decision{
		BarDate:    bars[last].Date.Format("2006-01-02"),
		FillDate:   mktcal.NextTradingDay(bars[last].Date).Format("2006-01-02"),
		Close:      bars[last].Close,
		SMA:        pts[last].SMA,
		Signal:     sig(pts[last].Long),
		PrevSignal: sig(pts[last-1].Long),
		Action:     "hold",
	}
	switch {
	case pts[last].Long && !pts[last-1].Long:
		d.Action = "buy"
	case !pts[last].Long && pts[last-1].Long:
		d.Action = "sell"
	}
	return d, bars, pts, nil
}

// paper runs the pre-registered paper account with cfdsim and the frozen
// costs, over the same window cmd/index-research -forward uses (the first
// bar on or after ForwardStart through the last closed bar).
func paper(bars []dailyrule.Bar, pts []dailyrule.Point, s instSpec) (etoroledger.Paper, error) {
	start, err := time.Parse("2006-01-02", s.ForwardStart)
	if err != nil {
		return etoroledger.Paper{}, err
	}
	p := etoroledger.Paper{ForwardStart: s.ForwardStart, SpreadRTBps: s.SpreadRTBps, OvernightBps: s.OvernightBps,
		CostsStatus: s.CostsStatus, Equity: 1, HoldEquity: 1, Position: "flat"}
	last := len(bars) - 1
	want := make([]bool, len(pts))
	for i := range pts {
		want[i] = pts[i].Long
	}
	lo := sort.Search(len(bars), func(i int) bool { return !bars[i].Date.Before(start) })
	if lo > last { // the window has no closed bar yet
		p.PendingAction = map[bool]string{true: "buy", false: "hold"}[want[last]]
		return p, nil
	}
	if lo == 0 {
		return etoroledger.Paper{}, fmt.Errorf("no bar before forward start %s to take the first decision from", s.ForwardStart)
	}
	c := cfdsim.Costs{SpreadPerLeg: s.SpreadRTBps / 2 / 1e4, NightlyRate: cfdsim.FixedNightly(s.OvernightBps / 1e4)}
	hold := make([]bool, len(bars))
	for i := range hold {
		hold[i] = true
	}
	tr := cfdsim.Simulate(bars, want, lo, last, c)
	hr := cfdsim.Simulate(bars, hold, lo, last, c)
	held := want[last-1]
	p.Started = true
	p.Days = last - lo + 1
	p.Position = sig(held)
	p.RoundTrips = tr.RoundTrips
	p.Equity = 1 + tr.ReturnPct/100
	p.HoldEquity = 1 + hr.ReturnPct/100
	p.MaxDrawdown = tr.MaxDDPct / 100
	p.FinancingPct = tr.FinancePct
	switch {
	case want[last] && !held:
		p.PendingAction = "buy"
	case !want[last] && held:
		p.PendingAction = "sell"
	default:
		p.PendingAction = "hold"
	}
	return p, nil
}
