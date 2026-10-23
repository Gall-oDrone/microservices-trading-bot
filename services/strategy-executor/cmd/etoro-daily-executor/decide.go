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
//
// Since 2026-10-21 a spec also says what product it is (Kind): an x1 index
// CFD pays a spread and overnight financing; an x1 ETF settles as the real
// asset and pays a fixed commission per order and no financing (the ETF
// cell, FORWARD-TEST-PREREGISTRATION-SMA50-ETF-2026-10-21.md, draft).
type instSpec struct {
	Book         string
	Symbol       string
	ID           int64
	Kind         string  // KindCFD | KindETF
	ForwardStart string  // first forward day: the paper's first fill is at its open
	SpreadRTBps  float64 // frozen round-trip spread (§3); the ETF's tiny bid/ask
	OvernightBps float64 // CFD: frozen overnight fee per calendar night at x1 (§3). ETF: 0
	FeeBps       float64 // ETF: commission per order, bps of notional (0.15 % measured 2026-10-21). CFD: 0
	CostsStatus  string
	Prereg       string
	// Draft marks a spec whose pre-registration is not frozen yet: it may
	// be decided and recorded in dry-run mode (-allow-draft) but never
	// traded on the demo account.
	Draft bool
}

// Product kinds.
const (
	KindCFD = "cfd"
	KindETF = "etf"
)

// costs is the cfdsim cost model of the spec's product.
func (s instSpec) costs() cfdsim.Costs {
	c := cfdsim.Costs{SpreadPerLeg: s.SpreadRTBps / 2 / 1e4}
	switch s.Kind {
	case KindETF:
		c.FeePerLeg = s.FeeBps / 1e4
	default:
		c.NightlyRate = cfdsim.FixedNightly(s.OvernightBps / 1e4)
	}
	return c
}

// holdLabel names the buy-and-hold the paper equity is printed against.
func (s instSpec) holdLabel() string {
	if s.Kind == KindETF {
		return "ETF hold"
	}
	return "CFD hold"
}

const prereg = "FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md"

// The cost cells were PROVISIONAL (weekend capture of 2026-10-10) until the
// weekday re-measure of 2026-10-12 confirmed them (pre-registration §3);
// they are frozen since. The label is the one cmd/etoro-risk-report carries.
const costsStatus = "FROZEN (pre-registration §3: the provisional values of 2026-10-10 became final on 2026-10-12)"

var frozenSpecs = map[string]instSpec{
	"NSDQ100": {Book: "nsdq100", Symbol: "NSDQ100", ID: 28, Kind: KindCFD, ForwardStart: "2026-10-13",
		SpreadRTBps: 15.5, OvernightBps: 2.3, CostsStatus: costsStatus, Prereg: prereg},
	"SPX500": {Book: "spx500", Symbol: "SPX500", ID: 27, Kind: KindCFD, ForwardStart: "2026-10-13",
		SpreadRTBps: 14.8, OvernightBps: 2.3, CostsStatus: costsStatus, Prereg: prereg},
	// The ETF cell, FORWARD-TEST-PREREGISTRATION-SMA50-ETF-2026-10-21.md,
	// frozen 2026-10-21 from the in-session capture of the same day
	// (docs/etoro/evidence-2026-10-21/costs.json, rates.json): the commission
	// is proportional, 1.65 USD on 1,100 USD = 15 bps per order (1.50 on
	// 1,000 in the 2026-10-10 capture), bid/ask QQQ 0.28 / SPY 0.26 bps,
	// overnight fee 0. Forward window 2026-10-22 -> 2027-10-21.
	"QQQ": {Book: "qqq", Symbol: "QQQ", ID: 3006, Kind: KindETF, ForwardStart: "2026-10-22",
		SpreadRTBps: 0.28, FeeBps: 15, CostsStatus: etfCostsStatus, Prereg: etfPrereg},
	"SPY": {Book: "spy", Symbol: "SPY", ID: 3000, Kind: KindETF, ForwardStart: "2026-10-22",
		SpreadRTBps: 0.26, FeeBps: 15, CostsStatus: etfCostsStatus, Prereg: etfPrereg},
}

const (
	etfPrereg      = "FORWARD-TEST-PREREGISTRATION-SMA50-ETF-2026-10-21.md"
	etfCostsStatus = "FROZEN (pre-registration §3: in-session capture of 2026-10-21)"
)

// draftSpecs holds instruments whose pre-registration is not frozen yet.
// They can be decided and recorded in dry-run mode with -allow-draft and
// never traded on the demo account; a frozen pre-registration moves them to
// frozenSpecs. Empty since the ETF cell was frozen on 2026-10-21.
var draftSpecs = map[string]instSpec{}

func specNames() []string {
	out := make([]string, 0, len(frozenSpecs))
	for k := range frozenSpecs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// cfdNames is the default of -instruments: the index-CFD cell as
// pre-registered (NSDQ100, SPX500). It must never be "every frozen spec":
// when QQQ / SPY were frozen on 2026-10-21 that default mixed the two
// products, and the CFD cron job (which passes no -instruments) exited 2 on
// 2026-10-22 and 2026-10-23 before fetching a bar. The ETF cell names its
// instruments explicitly.
func cfdNames() []string {
	out := make([]string, 0, len(frozenSpecs))
	for k, s := range frozenSpecs {
		if s.Kind == KindCFD {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func draftNames() []string {
	out := make([]string, 0, len(draftSpecs))
	for k := range draftSpecs {
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
	p := etoroledger.Paper{ForwardStart: s.ForwardStart, Product: s.Kind, SpreadRTBps: s.SpreadRTBps, OvernightBps: s.OvernightBps,
		FeeBps: s.FeeBps, CostsStatus: s.CostsStatus, Equity: 1, HoldEquity: 1, Position: "flat"}
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
	c := s.costs()
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
