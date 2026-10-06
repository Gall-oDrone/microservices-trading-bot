package api

import (
	"math"
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/store"
)

// Milestones are the frozen dates from the pre-registrations (§2).
type Milestones struct {
	ForwardStart     string  `json:"forward_start"`
	Interim          string  `json:"interim"`
	Evaluation       string  `json:"evaluation"`
	DaysElapsed      int     `json:"days_elapsed"`
	DaysToInterim    int     `json:"days_to_interim"`
	DaysToEvaluation int     `json:"days_to_evaluation"`
	WindowProgress   float64 `json:"window_progress"` // 0..1
}

// PreregDates per book; the interim and evaluation dates are frozen.
var PreregDates = map[string]struct{ Interim, Evaluation string }{
	"btc_mxn": {"2027-03-26", "2027-09-26"},
	"btc_usd": {"2027-03-26", "2027-09-26"},
}

// RunStatus says whether the executor has recorded every closed day.
type RunStatus struct {
	Status          string   `json:"status"` // "ok" | "pending" | "missed" | "no_data"
	ExpectedBarDate string   `json:"expected_bar_date"`
	LastBarDate     string   `json:"last_bar_date"`
	MissingDays     []string `json:"missing_days"`
	Message         string   `json:"message"`
}

// ForwardTest is one book's card on the Forward tests page.
type ForwardTest struct {
	Ledger           string                 `json:"ledger"`
	Book             string                 `json:"book"`
	Base             string                 `json:"base"`
	Quote            string                 `json:"quote"`
	Prereg           string                 `json:"prereg"`
	Mode             string                 `json:"mode"`
	CodeVersion      string                 `json:"code_version"`
	RecordedAt       string                 `json:"recorded_at"`
	Decision         dailyledger.Decision   `json:"decision"`
	Paper            dailyledger.Paper      `json:"paper"`
	Candles          dailyledger.CandleInfo `json:"candles"`
	StagePosition    *dailyledger.Position  `json:"stage_position"`
	DistanceToSMAPct float64                `json:"distance_to_sma_pct"` // (close-sma)/close, %
	ExcessVsHoldPct  float64                `json:"excess_vs_hold_pct"`  // equity/hold - 1, %
	Milestones       Milestones             `json:"milestones"`
	Run              RunStatus              `json:"run"`
	RiskWarnings     int                    `json:"risk_warnings"`
	RiskBlocks       int                    `json:"risk_blocks"`
}

func daysBetween(a, b string) int {
	ta, err1 := time.Parse("2006-01-02", a)
	tb, err2 := time.Parse("2006-01-02", b)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(math.Round(tb.Sub(ta).Hours() / 24))
}

func milestones(book, forwardStart, today string) Milestones {
	d := PreregDates[book]
	m := Milestones{ForwardStart: forwardStart, Interim: d.Interim, Evaluation: d.Evaluation}
	if forwardStart == "" || d.Evaluation == "" {
		return m
	}
	m.DaysElapsed = max(0, daysBetween(forwardStart, today))
	m.DaysToInterim = max(0, daysBetween(today, d.Interim))
	m.DaysToEvaluation = max(0, daysBetween(today, d.Evaluation))
	if total := daysBetween(forwardStart, d.Evaluation); total > 0 {
		m.WindowProgress = math.Min(1, float64(m.DaysElapsed)/float64(total))
	}
	return m
}

func runStatus(last string, now time.Time) RunStatus {
	rs := RunStatus{ExpectedBarDate: dailyledger.ExpectedLastBar(now), LastBarDate: last, MissingDays: []string{}}
	if last == "" {
		rs.Status, rs.Message = "no_data", "no ledger records for this book yet"
		return rs
	}
	if m := dailyledger.MissingDays(last, now); len(m) > 0 {
		rs.MissingDays = m
	}
	mx := now.In(dailyledger.Mexico)
	switch {
	case len(rs.MissingDays) == 0:
		rs.Status, rs.Message = "ok", "up to date"
	case len(rs.MissingDays) == 1 && mx.Hour() < 6:
		rs.Status, rs.Message = "pending", "no run today yet: the executor runs after 00:05 Mexico City"
	default:
		rs.Status, rs.Message = "missed", "the executor has not recorded every closed day: check the run logs"
	}
	return rs
}

func pct(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b * 100
}

func buildForwardTest(book string, recs []dailyledger.Record, now time.Time, br BookRisk) ForwardTest {
	base, quote := dailyledger.BaseQuote(book)
	ft := ForwardTest{Book: book, Base: base, Quote: quote}
	today := now.In(dailyledger.Mexico).Format("2006-01-02")
	if len(recs) == 0 {
		ft.Run = runStatus("", now)
		ft.Milestones = milestones(book, "", today)
		return ft
	}
	last := recs[len(recs)-1]
	ft.Prereg, ft.Mode, ft.CodeVersion, ft.RecordedAt = last.Prereg, last.Mode, last.CodeVersion, last.RecordedAt
	ft.Decision, ft.Paper, ft.Candles = last.Decision, last.Paper, last.Candles
	if last.Mode == "stage" {
		p := dailyledger.LastStagePosition(recs)
		ft.StagePosition = &p
	}
	ft.DistanceToSMAPct = pct(last.Decision.Close-last.Decision.SMA, last.Decision.Close)
	if last.Paper.HoldEquity > 0 {
		ft.ExcessVsHoldPct = (last.Paper.Equity/last.Paper.HoldEquity - 1) * 100
	}
	ft.Milestones = milestones(book, last.Paper.ForwardStart, today)
	ft.Run = runStatus(last.Decision.BarDate, now)
	for _, f := range br.Findings {
		if f.Severity == risk.Block {
			ft.RiskBlocks++
		} else {
			ft.RiskWarnings++
		}
	}
	return ft
}

// Fill is one stage leg with realized costs.
type Fill struct {
	BarDate       string   `json:"bar_date"`
	FillDate      string   `json:"fill_date"`
	Side          string   `json:"side"`
	TargetBTC     float64  `json:"target_btc"`
	FilledBTC     float64  `json:"filled_btc"`
	MakerBTC      float64  `json:"maker_btc"`
	TakerBTC      float64  `json:"taker_btc"`
	NetBTC        float64  `json:"net_btc"`
	AvgPrice      float64  `json:"avg_price"`
	Notional      float64  `json:"notional"`
	FeeQuote      float64  `json:"fee_quote"`
	FeeBps        float64  `json:"fee_bps"`
	RefOpen       *float64 `json:"ref_open"`     // fill day's open, from the candles
	SlippageBps   *float64 `json:"slippage_bps"` // vs ref_open, + is adverse
	TotalCostBps  float64  `json:"total_cost_bps"`
	AssumedLegBps float64  `json:"assumed_leg_bps"`
	Fallback      bool     `json:"market_fallback"`
	Started       string   `json:"started"`
	Finished      string   `json:"finished"`
	Notes         []string `json:"notes"`
}

func buildFills(book string, recs []dailyledger.Record, candles []store.Candle) []Fill {
	openOn := make(map[string]float64, len(candles))
	for _, c := range candles {
		openOn[c.Date] = c.Open
	}
	out := []Fill{}
	for _, r := range recs {
		if r.Stage == nil || r.Stage.Leg == nil {
			continue
		}
		l := *r.Stage.Leg
		fee, _ := dailyledger.FeeQuote(book, l)
		f := Fill{
			BarDate: r.Decision.BarDate, FillDate: r.Decision.FillDate, Side: l.Side,
			TargetBTC: l.Target, FilledBTC: l.Filled, MakerBTC: l.MakerFilled, TakerBTC: l.TakerFilled,
			NetBTC: l.BaseDelta, AvgPrice: l.AvgPrice, Notional: l.Notional, FeeQuote: fee,
			FeeBps: dailyledger.FeeBps(book, l), AssumedLegBps: r.Paper.LegCostBps, Fallback: l.Fallback,
			Started: l.Started.UTC().Format(time.RFC3339), Finished: l.Finished.UTC().Format(time.RFC3339),
			Notes: l.Notes,
		}
		if f.Notes == nil {
			f.Notes = []string{}
		}
		f.TotalCostBps = f.FeeBps
		// The leg may start after midnight UTC of the fill date; the open of
		// the Mexico City fill day is the paper account's price.
		if o, ok := openOn[r.Decision.FillDate]; ok {
			o := o
			f.RefOpen = &o
			if s, ok := dailyledger.SlippageBps(l, o); ok {
				f.SlippageBps = &s
				f.TotalCostBps += s
			}
		}
		out = append(out, f)
	}
	return out
}

// EquityPoint is one day of the forward test.
type EquityPoint struct {
	Date        string  `json:"date"`
	Equity      float64 `json:"equity"`
	HoldEquity  float64 `json:"hold_equity"`
	MaxDrawdown float64 `json:"max_drawdown"`
	Signal      string  `json:"signal"`
	Close       float64 `json:"close"`
	SMA         float64 `json:"sma50"`
}

func buildEquity(recs []dailyledger.Record) []EquityPoint {
	out := make([]EquityPoint, 0, len(recs))
	for _, r := range recs {
		out = append(out, EquityPoint{Date: r.Decision.BarDate, Equity: r.Paper.Equity, HoldEquity: r.Paper.HoldEquity,
			MaxDrawdown: r.Paper.MaxDrawdown, Signal: r.Decision.Signal, Close: r.Decision.Close, SMA: r.Decision.SMA})
	}
	return out
}

// CandlePoint is a candle with the SMA50 and 20-day volume ratio.
type CandlePoint struct {
	store.Candle
	SMA50       *float64 `json:"sma50"`
	VolumeRatio *float64 `json:"volume_ratio_20d"` // volume / mean of the previous 20 days
	Long        *bool    `json:"long"`
}

func buildCandles(rows []store.Candle, days int) []CandlePoint {
	out := make([]CandlePoint, len(rows))
	var sum float64
	for i, c := range rows {
		out[i].Candle = c
		sum += c.Close
		if i >= 50 {
			sum -= rows[i-50].Close
		}
		if i >= 49 {
			s := sum / 50
			l := c.Close > s
			out[i].SMA50, out[i].Long = &s, &l
		}
		if i >= 20 {
			var v float64
			for _, p := range rows[i-20 : i] {
				v += p.Volume
			}
			if v > 0 {
				r := c.Volume / (v / 20)
				out[i].VolumeRatio = &r
			}
		}
	}
	if days > 0 && len(out) > days {
		out = out[len(out)-days:]
	}
	return out
}

// RealizedCost summarizes the stage legs of one book.
type RealizedCost struct {
	Legs           int     `json:"legs"`
	AvgFeeBps      float64 `json:"avg_fee_bps"`
	AvgSlippageBps float64 `json:"avg_slippage_bps"`
	AvgTotalBps    float64 `json:"avg_total_bps"`
	AssumedLegBps  float64 `json:"assumed_leg_bps"`
	FallbackLegs   int     `json:"fallback_legs"`
}

// NextOrder previews the order the next stage run would send, and what the
// risk check says about it today.
type NextOrder struct {
	Action   string        `json:"action"` // "buy" | "sell" | "none"
	QtyBTC   float64       `json:"qty_btc"`
	RefPrice float64       `json:"ref_price"`
	FillDate string        `json:"fill_date"`
	Decision risk.Decision `json:"decision"`
}

// Utilization of each limit, 0..1+ (0 when the limit is disabled).
type Utilization struct {
	Position  float64 `json:"position"`
	OrderSize float64 `json:"order_size"`
	Notional  float64 `json:"notional"`
	Drawdown  float64 `json:"drawdown"`
}

// BookRisk is one book's section of the Risk page.
type BookRisk struct {
	Book             string          `json:"book"`
	Quote            string          `json:"quote"`
	Mode             string          `json:"mode"`
	Limits           risk.BookLimits `json:"limits"`
	PositionBTC      float64         `json:"position_btc"`
	PositionNotional float64         `json:"position_notional"`
	LastClose        float64         `json:"last_close"`
	PaperDrawdown    float64         `json:"paper_max_drawdown"`
	Utilization      Utilization     `json:"utilization"`
	NextOrder        *NextOrder      `json:"next_order"`
	Cost             RealizedCost    `json:"realized_cost"`
	// LastCheck is the executor's most recent recorded pre-trade check
	// (null before the executor enforced shared/pkg/risk).
	LastCheck *LastCheck `json:"last_check"`
	// BlockedDays are bar dates whose stage order the executor blocked.
	BlockedDays []string       `json:"blocked_days"`
	Findings    []risk.Finding `json:"findings"`
}

// LastCheck is a recorded executor risk check and the day it ran for.
type LastCheck struct {
	BarDate  string `json:"bar_date"`
	FillDate string `json:"fill_date"`
	dailyledger.RiskCheck
}

// Operational rules reported by ui-api (not limits in the policy).
const (
	RuleRunMissed      = "run_missed"
	RuleDataGap        = "data_gap"
	RuleOrderBlocked   = "order_blocked"   // the executor blocked a stage order (recorded, not retried)
	RulePolicyMismatch = "policy_mismatch" // the executor's last check used another policy version
)

const btcDust = 1e-8

func buildBookRisk(p risk.Policy, book string, recs []dailyledger.Record, fills []Fill, now time.Time, stageSize float64) BookRisk {
	_, quote := dailyledger.BaseQuote(book)
	l := p.For(book)
	br := BookRisk{Book: book, Quote: quote, Limits: l, BlockedDays: []string{}, Findings: []risk.Finding{}}
	if len(recs) == 0 {
		return br
	}
	last := recs[len(recs)-1]
	br.Mode = last.Mode
	br.LastClose = last.Decision.Close
	br.PaperDrawdown = last.Paper.MaxDrawdown
	pos := dailyledger.LastStagePosition(recs)
	br.PositionBTC = pos.BTC
	br.PositionNotional = pos.BTC * last.Decision.Close

	// Next stage order, mirroring the executor's planAction.
	if last.Mode == "stage" {
		no := &NextOrder{Action: "none", RefPrice: last.Decision.Close, FillDate: last.Decision.FillDate}
		switch {
		case last.Decision.Signal == "long" && pos.BTC <= btcDust:
			no.Action, no.QtyBTC = "buy", stageSize
		case last.Decision.Signal == "flat" && pos.BTC > btcDust:
			no.Action, no.QtyBTC = "sell", pos.BTC
		}
		if no.Action != "none" {
			o := risk.Order{Book: book, Side: no.Action, QtyBTC: no.QtyBTC, Price: last.Decision.Close, RefPrice: last.Decision.Close}
			st := risk.State{PositionBTC: pos.BTC, OrdersToday: dailyledger.LegsOn(recs, last.Decision.FillDate)}
			no.Decision = risk.Check(p, o, st)
			br.Findings = append(br.Findings, no.Decision.Findings...)
		} else {
			no.Decision = risk.Decision{Allowed: true, Findings: []risk.Finding{}}
		}
		if no.Decision.Findings == nil {
			no.Decision.Findings = []risk.Finding{}
		}
		br.NextOrder = no
		br.Utilization.OrderSize = risk.Utilization(max(no.QtyBTC, stageSize), l.MaxOrderBTC)
		br.Utilization.Notional = risk.Utilization(max(no.QtyBTC, stageSize)*last.Decision.Close, l.MaxOrderNotional)
	}
	br.Utilization.Position = risk.Utilization(pos.BTC, l.MaxPositionBTC)
	br.Utilization.Drawdown = risk.Utilization(last.Paper.MaxDrawdown, l.DrawdownWarn)

	if f := risk.AssessDrawdown(p, book, "paper", last.Paper.MaxDrawdown); f != nil {
		br.Findings = append(br.Findings, *f)
	}

	// What the executor actually enforced (recorded in the ledger).
	for _, r := range recs {
		if r.Stage == nil || r.Stage.Risk == nil {
			continue
		}
		br.LastCheck = &LastCheck{BarDate: r.Decision.BarDate, FillDate: r.Decision.FillDate, RiskCheck: *r.Stage.Risk}
		if r.Stage.Action == dailyledger.ActionBlocked {
			br.BlockedDays = append(br.BlockedDays, r.Decision.BarDate)
		}
	}
	if n := len(br.BlockedDays); n > 0 {
		br.Findings = append(br.Findings, risk.Finding{Rule: RuleOrderBlocked, Severity: risk.Warn, Value: float64(n),
			Message: "the executor blocked the stage order for: " + join(br.BlockedDays) + " (skipped and recorded; paper account unaffected)"})
	}
	if lc := br.LastCheck; lc != nil && lc.PolicyVersion != p.Version {
		br.Findings = append(br.Findings, risk.Finding{Rule: RulePolicyMismatch, Severity: risk.Warn,
			Message: "the executor's last check (" + lc.BarDate + ") used policy " + lc.PolicyVersion + ", this page shows " + p.Version})
	}
	var fee, slip, tot float64
	slipN := 0
	for _, f := range fills {
		br.Cost.Legs++
		fee += f.FeeBps
		tot += f.TotalCostBps
		if f.SlippageBps != nil {
			slip += *f.SlippageBps
			slipN++
		}
		if f.Fallback {
			br.Cost.FallbackLegs++
		}
		if w := risk.AssessCost(p, book, "leg "+f.FillDate+" "+f.Side, f.TotalCostBps); w != nil {
			br.Findings = append(br.Findings, *w)
		}
	}
	br.Cost.AssumedLegBps = last.Paper.LegCostBps
	if br.Cost.Legs > 0 {
		br.Cost.AvgFeeBps = fee / float64(br.Cost.Legs)
		br.Cost.AvgTotalBps = tot / float64(br.Cost.Legs)
	}
	if slipN > 0 {
		br.Cost.AvgSlippageBps = slip / float64(slipN)
	}

	if rs := runStatus(last.Decision.BarDate, now); rs.Status == "missed" {
		br.Findings = append(br.Findings, risk.Finding{Rule: RuleRunMissed, Severity: risk.Warn,
			Value: float64(len(rs.MissingDays)), Message: "missing ledger days: " + join(rs.MissingDays)})
	}
	if last.Candles.RecentGaps != "" {
		br.Findings = append(br.Findings, risk.Finding{Rule: RuleDataGap, Severity: risk.Warn,
			Message: "candle gaps in the last 60 bars: " + last.Candles.RecentGaps})
	}
	sort.SliceStable(br.Findings, func(i, j int) bool {
		return br.Findings[i].Severity == risk.Block && br.Findings[j].Severity != risk.Block
	})
	return br
}

func join(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}
