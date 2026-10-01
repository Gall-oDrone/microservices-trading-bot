package main

import (
	"fmt"
	"time"

	"bitso-trading-platform/strategy-executor/internal/bitsodaily"
	"bitso-trading-platform/strategy-executor/internal/dailyrule"
)

// bookSpec is the frozen, per-book forward-test configuration. Values come
// from the two pre-registrations in docs/backtest-readiness/ and must not be
// changed while the forward tests run.
type bookSpec struct {
	Book         string
	HistoryFrom  string  // first date fetched (the registered procedure's -from)
	ForwardStart string  // first forward day: the first fill is at this day's open
	LegCostBps   float64 // primary cost per leg: maker commission + 10 bps slippage
	Prereg       string  // pre-registration file name
}

var frozenSpecs = map[string]bookSpec{
	"btc_mxn": {
		Book: "btc_mxn", HistoryFrom: "2017-06-01", ForwardStart: "2026-09-27",
		LegCostBps: 60 + 10, Prereg: "FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md",
	},
	"btc_usd": {
		Book: "btc_usd", HistoryFrom: "2020-04-01", ForwardStart: "2026-09-29",
		LegCostBps: 30 + 10, Prereg: "FORWARD-TEST-PREREGISTRATION-SMA50-BTCUSD-2026-09-29.md",
	},
}

// smaDays is frozen by both pre-registrations.
const smaDays = 50

// decision is the rule's output for one book on one day.
type decision struct {
	BarDate    string  `json:"bar_date"`  // day t whose close produced the decision
	FillDate   string  `json:"fill_date"` // day t+1, whose open executes it
	Close      float64 `json:"close"`
	SMA        float64 `json:"sma50"`
	Signal     string  `json:"signal"`      // "long" | "flat" at t's close
	PrevSignal string  `json:"prev_signal"` // at t-1's close
	Action     string  `json:"action"`      // "buy" | "sell" | "hold" at t+1's open
}

func sig(long bool) string {
	if long {
		return "long"
	}
	return "flat"
}

func toBars(rows []bitsodaily.Row) ([]dailyrule.Bar, error) {
	bars := make([]dailyrule.Bar, len(rows))
	for i, r := range rows {
		day, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			return nil, fmt.Errorf("row %d: date %q: %w", i, r.Date, err)
		}
		bars[i] = dailyrule.Bar{Date: day, Open: r.Open, High: r.High, Low: r.Low, Close: r.Close}
	}
	return bars, nil
}

// expectedLastBar is the latest Mexico City date whose daily candle has
// closed as of now: yesterday, Mexico time.
func expectedLastBar(now time.Time) string {
	return now.In(bitsodaily.Mexico).AddDate(0, 0, -1).Format("2006-01-02")
}

// decide applies the frozen rule to bars. The last bar must be the expected
// one (no stale data), and the rule must be warm on the last two bars.
func decide(bars []dailyrule.Bar, now time.Time) (decision, []dailyrule.Point, error) {
	if len(bars) < 2 {
		return decision{}, nil, fmt.Errorf("need at least 2 bars, have %d", len(bars))
	}
	last := len(bars) - 1
	got := bars[last].Date.Format("2006-01-02")
	if want := expectedLastBar(now); got != want {
		return decision{}, nil, fmt.Errorf("stale candles: last closed bar is %s, expected %s", got, want)
	}
	pts := dailyrule.Evaluate(bars, smaDays)
	if !pts[last].Warm || !pts[last-1].Warm {
		return decision{}, nil, fmt.Errorf("rule not warm at %s (a >%d-day gap in the last %d bars?)", got, dailyrule.MaxGapDays, smaDays)
	}
	d := decision{
		BarDate:    got,
		FillDate:   bars[last].Date.AddDate(0, 0, 1).Format("2006-01-02"),
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
	return d, pts, nil
}

// paperResult is the forward test's paper account, from ForwardStart through
// the last closed bar, starting from 1.0 unit of quote currency.
type paperResult struct {
	ForwardStart  string  `json:"forward_start"`
	Days          int     `json:"days"`     // closed forward days
	Position      string  `json:"position"` // held through the last close
	Fills         int     `json:"fills"`
	LegCostBps    float64 `json:"leg_cost_bps"`
	Equity        float64 `json:"equity"`           // marked at the last close, no exit cost
	EquityClosed  float64 `json:"equity_if_closed"` // as daily-research reports: open position sold at the last close, with cost
	HoldEquity    float64 `json:"hold_equity"`      // bought at ForwardStart's open, marked at the last close
	MaxDrawdown   float64 `json:"max_drawdown"`     // fraction, on marked equity
	PendingAction string  `json:"pending_action"`   // what the next open will do
}

// paper runs the account with exactly the arithmetic of daily-research's
// simulate(): a decision at day t-1's close fills at day t's open, each leg
// pays legCost on its own notional, and equity is marked at each close.
func paper(bars []dailyrule.Bar, pts []dailyrule.Point, forwardStart string, legCostBps float64) (paperResult, error) {
	start, err := time.Parse("2006-01-02", forwardStart)
	if err != nil {
		return paperResult{}, err
	}
	res := paperResult{ForwardStart: forwardStart, LegCostBps: legCostBps, Equity: 1, EquityClosed: 1, HoldEquity: 1, Position: "flat"}
	lo := -1
	for i, b := range bars {
		if !b.Date.Before(start) {
			lo = i
			break
		}
	}
	last := len(bars) - 1
	if lo == -1 { // forward window has not reached a closed bar yet
		res.PendingAction = map[bool]string{true: "buy", false: "hold"}[pts[last].Long]
		return res, nil
	}
	if lo == 0 {
		return paperResult{}, fmt.Errorf("no bar before forward start %s to take the first decision from", forwardStart)
	}
	if got := bars[lo].Date.Format("2006-01-02"); got != forwardStart {
		return paperResult{}, fmt.Errorf("forward start %s is missing from the candles (first bar on/after it is %s)", forwardStart, got)
	}

	c := legCostBps / 1e4
	cash, units, held := 1.0, 0.0, false
	peak := 1.0
	for i := lo; i <= last; i++ {
		desire := pts[i-1].Long
		o := bars[i].Open
		switch {
		case desire && !held:
			fee := cash * c
			units = (cash - fee) / o
			cash, held = 0, true
			res.Fills++
		case !desire && held:
			gross := units * o
			fee := gross * c
			cash = gross - fee
			units, held = 0, false
			res.Fills++
		}
		eq := cash + units*bars[i].Close
		if eq > peak {
			peak = eq
		}
		if dd := (peak - eq) / peak; dd > res.MaxDrawdown {
			res.MaxDrawdown = dd
		}
		res.Equity = eq
	}
	res.Days = last - lo + 1
	res.Position = sig(held)
	res.EquityClosed = cash
	if held {
		gross := units * bars[last].Close
		res.EquityClosed = gross - gross*c
	}
	res.HoldEquity = (1 - c) / bars[lo].Open * bars[last].Close
	switch {
	case pts[last].Long && !held:
		res.PendingAction = "buy"
	case !pts[last].Long && held:
		res.PendingAction = "sell"
	default:
		res.PendingAction = "hold"
	}
	return res, nil
}
