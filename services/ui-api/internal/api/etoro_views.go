package api

// Views of the eToro index-CFD executor's ledger (shared/pkg/etoroledger)
// behind the /api/ui/etoro/ routes: USD, instrument units, New York time.
// These are pure functions over records; the handlers and file readers are
// in etoro.go. The Bitso views in views.go are untouched: the two ledgers
// have different schemas and units, and the Bitso JSON is pinned by the web
// fixtures.

import (
	"fmt"
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/etorohealth"
)

// etoroRunAt is the executor's scheduled start in New York local time
// (scripts/install-ops-cron.sh runs it at 09:35 on NYSE trading days; the
// run is due 25 minutes later, etorohealth.Options).
const etoroRunAt = 9*time.Hour + 35*time.Minute

const dateFmt = "2006-01-02"

// EtoroSession is the NYSE cash session around now (the executor only acts
// inside it; shared/pkg/mktcal).
type EtoroSession struct {
	NowNewYork string `json:"now_new_york"` // RFC 3339 with the New York offset
	Date       string `json:"date"`         // New York calendar date
	TradingDay bool   `json:"trading_day"`
	Open       bool   `json:"open"` // the cash session is open now
	OpensAt    string `json:"opens_at,omitempty"`
	ClosesAt   string `json:"closes_at,omitempty"`
	EarlyClose bool   `json:"early_close,omitempty"`
	Holiday    string `json:"holiday,omitempty"`
}

func etoroSession(now time.Time) EtoroSession {
	ny := now.In(mktcal.NewYork)
	today := mktcal.Date(ny.Year(), ny.Month(), ny.Day())
	s := EtoroSession{NowNewYork: ny.Format(time.RFC3339), Date: today.Format(dateFmt),
		TradingDay: mktcal.IsTradingDay(today), Open: mktcal.IsOpen(now)}
	if open, close, ok := mktcal.Session(today); ok {
		s.OpensAt, s.ClosesAt = open.Format(time.RFC3339), close.Format(time.RFC3339)
		s.EarlyClose = mktcal.IsEarlyClose(today)
	} else if ny.Weekday() != time.Saturday && ny.Weekday() != time.Sunday {
		s.Holiday, _ = mktcal.Holiday(today)
	}
	return s
}

// EtoroRunStatus says whether the 09:35 New York run has recorded a book
// for every due trading day, and when the next run is.
type EtoroRunStatus struct {
	Status string `json:"status"` // "ok" | "pending" | "missed" | "no_data"
	// DueDate is the latest trading day whose run is already due
	// (etorohealth.Options.DueDate: 10:00 New York).
	DueDate      string   `json:"due_date"`
	LastFillDate string   `json:"last_fill_date"`
	LastBarDate  string   `json:"last_bar_date"`
	MissingDays  []string `json:"missing_days"`
	TodayDue     bool     `json:"today_due"`  // a run is scheduled today (New York trading day)
	TodayDone    bool     `json:"today_done"` // today's run recorded this book
	NextRunAt    string   `json:"next_run_at"`
	NextRunDate  string   `json:"next_run_date"`
	Message      string   `json:"message"`
}

// nyStart is the instant of New York local time d + offset on date.
func nyStart(date time.Time, offset time.Duration) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, mktcal.NewYork).Add(offset)
}

// etoroRunStatus is the run coverage of one book's records (sorted by bar
// date). A record counts for its fill date: the day the run happened.
func etoroRunStatus(recs []etoroledger.Record, now time.Time) EtoroRunStatus {
	var opts etorohealth.Options
	ny := now.In(mktcal.NewYork)
	today := mktcal.Date(ny.Year(), ny.Month(), ny.Day())
	due := opts.DueDate(now)
	rs := EtoroRunStatus{DueDate: due.Format(dateFmt), MissingDays: []string{}, TodayDue: mktcal.IsTradingDay(today)}
	next := nyStart(today, etoroRunAt)
	if !rs.TodayDue || !ny.Before(next) {
		next = nyStart(mktcal.NextTradingDay(today), etoroRunAt)
	}
	rs.NextRunAt, rs.NextRunDate = next.Format(time.RFC3339), next.Format(dateFmt)
	if len(recs) == 0 {
		rs.Status, rs.Message = "no_data", "no ledger records for this book yet"
		return rs
	}
	last := recs[len(recs)-1]
	rs.LastFillDate, rs.LastBarDate = last.Decision.FillDate, last.Decision.BarDate
	rs.TodayDone = rs.LastFillDate == today.Format(dateFmt)
	for d, i := due, 0; d.Format(dateFmt) > rs.LastFillDate && i < 60; d, i = mktcal.PrevTradingDay(d), i+1 {
		rs.MissingDays = append(rs.MissingDays, d.Format(dateFmt))
	}
	sort.Strings(rs.MissingDays)
	dueToday := rs.TodayDue && ny.Before(nyStart(today, etoroRunAt).Add(25*time.Minute))
	switch {
	case len(rs.MissingDays) > 0:
		rs.Status = "missed"
		rs.Message = "the 09:35 New York run has not recorded every trading day: check the run logs and reconcile reports"
	case rs.TodayDue && !rs.TodayDone && dueToday:
		rs.Status = "pending"
		rs.Message = "today's run (09:35 New York) is not recorded yet; it is due by 10:00"
	default:
		rs.Status, rs.Message = "ok", "up to date"
	}
	return rs
}

// EtoroOrderView is a demo order with the day it was for. Source is "ui"
// for an assisted trade from the console (P5d), empty for the executor's.
type EtoroOrderView struct {
	BarDate  string `json:"bar_date"`
	FillDate string `json:"fill_date"`
	Source   string `json:"source,omitempty"`
	By       string `json:"by,omitempty"`
	etoroledger.Order
}

// EtoroLastCheck is the executor's most recent recorded pre-trade check.
type EtoroLastCheck struct {
	BarDate  string `json:"bar_date"`
	FillDate string `json:"fill_date"`
	etoroledger.RiskCheck
}

// EtoroForwardTest is one book's card on the Forward tests page for the
// eToro ledger.
type EtoroForwardTest struct {
	Ledger      string                 `json:"ledger"`
	Venue       string                 `json:"venue"`
	Book        string                 `json:"book"`
	Instrument  etoroledger.Instrument `json:"instrument"`
	Quote       string                 `json:"quote"` // account currency
	Prereg      string                 `json:"prereg"`
	Mode        string                 `json:"mode"`
	CodeVersion string                 `json:"code_version"`
	RecordedAt  string                 `json:"recorded_at"`
	Decision    etoroledger.Decision   `json:"decision"`
	Paper       etoroledger.Paper      `json:"paper"`
	Candles     etoroledger.CandleInfo `json:"candles"`
	// DemoPosition is the position after the newest demo record (null for
	// a ledger without demo records); DemoAction that record's action.
	DemoPosition *etoroledger.Position `json:"demo_position"`
	DemoAction   string                `json:"demo_action"`
	// Account is the demo account as the newest run saw it, before its
	// order.
	Account          *etoroledger.Account `json:"account"`
	LastOrder        *EtoroOrderView      `json:"last_order"`
	DistanceToSMAPct float64              `json:"distance_to_sma_pct"` // (close-sma)/close, %
	ExcessVsHoldPct  float64              `json:"excess_vs_hold_pct"`  // equity/hold - 1, %
	Run              EtoroRunStatus       `json:"run"`
	RiskWarnings     int                  `json:"risk_warnings"`
	RiskBlocks       int                  `json:"risk_blocks"`
	RiskFindings     []risk.Finding       `json:"risk_findings"`
}

// newestDemo is the newest record with a demo section, if any (the records
// are sorted oldest first by etoroledger; a console trade counts).
func newestDemo(recs []etoroledger.Record) *etoroledger.Record {
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Demo != nil {
			return &recs[i]
		}
	}
	return nil
}

func lastEtoroOrder(recs []etoroledger.Record) *EtoroOrderView {
	for i := len(recs) - 1; i >= 0; i-- {
		if d := recs[i].Demo; d != nil && d.Order != nil {
			v := &EtoroOrderView{BarDate: recs[i].Decision.BarDate, FillDate: recs[i].Decision.FillDate, Source: recs[i].Source, Order: *d.Order}
			if op := recs[i].Operator; op != nil {
				v.By = op.By
			}
			return v
		}
	}
	return nil
}

// buildEtoroForwardTest describes the rule from the executor's records
// (decision, paper, candles, run coverage) and the demo state from the
// newest record of any source, console trades included.
func buildEtoroForwardTest(book string, recs []etoroledger.Record, now time.Time, br EtoroBookRisk) EtoroForwardTest {
	ft := EtoroForwardTest{Venue: VenueEtoro, Book: book, Quote: "USD", RiskFindings: []risk.Finding{}}
	rule := etoroledger.RuleRecords(recs)
	ft.Run = etoroRunStatus(rule, now)
	if len(rule) == 0 {
		return ft
	}
	last := rule[len(rule)-1]
	ft.Instrument, ft.Prereg, ft.Mode, ft.CodeVersion, ft.RecordedAt = last.Instrument, last.Prereg, last.Mode, last.CodeVersion, last.RecordedAt
	ft.Decision, ft.Paper, ft.Candles = last.Decision, last.Paper, last.Candles
	if d := newestDemo(recs); d != nil {
		pos := d.Demo.PositionAfter
		ft.DemoPosition, ft.DemoAction, ft.Account = &pos, d.Demo.Action, d.Demo.Account
	}
	ft.LastOrder = lastEtoroOrder(recs)
	ft.DistanceToSMAPct = pct(last.Decision.Close-last.Decision.SMA, last.Decision.Close)
	if last.Paper.HoldEquity > 0 {
		ft.ExcessVsHoldPct = (last.Paper.Equity/last.Paper.HoldEquity - 1) * 100
	}
	ft.RiskFindings = br.Findings
	for _, f := range br.Findings {
		if f.Severity == risk.Block {
			ft.RiskBlocks++
		} else {
			ft.RiskWarnings++
		}
	}
	return ft
}

// EtoroEquityPoint is one decision day: the pre-registered paper account
// and, when the record has one, the demo account's equity.
type EtoroEquityPoint struct {
	Date        string  `json:"date"`
	Equity      float64 `json:"equity"` // paper, closed at the close with its spread
	HoldEquity  float64 `json:"hold_equity"`
	MaxDrawdown float64 `json:"max_drawdown"`
	Signal      string  `json:"signal"`
	Close       float64 `json:"close"`
	SMA         float64 `json:"sma50"`
	// DemoEquity and DemoExposure are the demo account before that run's
	// order (USD); null on dry-run records.
	DemoEquity   *float64 `json:"demo_equity"`
	DemoExposure *float64 `json:"demo_exposure"`
}

// buildEtoroEquity is the rule's series: console trades (no paper account
// of their own) are left out.
func buildEtoroEquity(recs []etoroledger.Record) []EtoroEquityPoint {
	out := make([]EtoroEquityPoint, 0, len(recs))
	for _, r := range etoroledger.RuleRecords(recs) {
		p := EtoroEquityPoint{Date: r.Decision.BarDate, Equity: r.Paper.Equity, HoldEquity: r.Paper.HoldEquity,
			MaxDrawdown: r.Paper.MaxDrawdown, Signal: r.Decision.Signal, Close: r.Decision.Close, SMA: r.Decision.SMA}
		if r.Demo != nil && r.Demo.Account != nil {
			eq, ex := r.Demo.Account.Equity, r.Demo.Account.Exposure
			p.DemoEquity, p.DemoExposure = &eq, &ex
		}
		out = append(out, p)
	}
	return out
}

// EtoroUtilization of the limits that apply to a held CFD position, 0..1+
// (0 when the limit is disabled).
type EtoroUtilization struct {
	Exposure float64 `json:"exposure"` // exposure / (equity x max_exposure_pct_equity)
	Notional float64 `json:"notional"` // position amount / max_order_notional
	Drawdown float64 `json:"drawdown"`
}

// EtoroBookRisk is one book's section of the Risk page for the eToro ledger.
type EtoroBookRisk struct {
	Book       string                 `json:"book"`
	Instrument etoroledger.Instrument `json:"instrument"`
	Quote      string                 `json:"quote"`
	Mode       string                 `json:"mode"`
	Limits     risk.BookLimits        `json:"limits"`
	// State and Position are the demo position after the newest demo
	// record (etoroledger.LastDemoPosition).
	State     string               `json:"state"` // "long" | "flat"
	Position  etoroledger.Position `json:"position"`
	LastClose float64              `json:"last_close"`
	// ExposureUSD is units x last close; ExposurePctEquity that against
	// the account equity the newest run saw.
	ExposureUSD       float64          `json:"exposure_usd"`
	ExposurePctEquity float64          `json:"exposure_pct_equity"`
	PaperDrawdown     float64          `json:"paper_max_drawdown"`
	Utilization       EtoroUtilization `json:"utilization"`
	// OrdersToday counts the book's demo orders on today's New York date.
	OrdersToday int             `json:"orders_today"`
	LastOrder   *EtoroOrderView `json:"last_order"`
	LastCheck   *EtoroLastCheck `json:"last_check"`
	// BlockedDays are bar dates whose demo order the executor blocked.
	BlockedDays []string       `json:"blocked_days"`
	Findings    []risk.Finding `json:"findings"`
}

// RuleCandlesMissing: the executor's candle history has holes on trading
// days (CandleInfo.Missing); the SMA50 is computed over fewer bars.
const RuleCandlesMissing = "candles_missing"

func buildEtoroBookRisk(p risk.Policy, book string, recs []etoroledger.Record, now time.Time) EtoroBookRisk {
	l := p.For(book)
	br := EtoroBookRisk{Book: book, Quote: "USD", Limits: l, State: "flat", Position: etoroledger.Flat(),
		BlockedDays: []string{}, Findings: []risk.Finding{}}
	rule := etoroledger.RuleRecords(recs)
	if len(recs) == 0 {
		return br
	}
	// The rule's newest line gives the close and the paper drawdown; the
	// newest line of any source gives the position (a console trade counts).
	last := recs[len(recs)-1]
	if len(rule) > 0 {
		last = rule[len(rule)-1]
	}
	br.Instrument, br.Mode, br.LastClose, br.PaperDrawdown = last.Instrument, last.Mode, last.Decision.Close, last.Paper.MaxDrawdown
	pos := etoroledger.LastDemoPosition(recs, book)
	br.Position = pos
	if pos.State != "" {
		br.State = pos.State
	}
	br.ExposureUSD = pos.Units * last.Decision.Close
	var equity float64
	if d := newestDemo(recs); d != nil && d.Demo.Account != nil {
		equity = d.Demo.Account.Equity
	}
	br.ExposurePctEquity = pct(br.ExposureUSD, equity)
	if equity > 0 {
		br.Utilization.Exposure = risk.Utilization(br.ExposureUSD, equity*l.MaxExposurePctEquity)
	}
	br.Utilization.Notional = risk.Utilization(pos.Amount, l.MaxOrderNotional)
	br.Utilization.Drawdown = risk.Utilization(last.Paper.MaxDrawdown, l.DrawdownWarn)
	ny := now.In(mktcal.NewYork)
	br.OrdersToday = etoroledger.LegsOn(recs, book, ny.Format(dateFmt))
	br.LastOrder = lastEtoroOrder(recs)

	// What the executor enforced (recorded in the ledger): the newest
	// check's findings are shown as they were.
	for _, r := range recs {
		if r.Demo == nil {
			continue
		}
		if r.Demo.Risk != nil {
			br.LastCheck = &EtoroLastCheck{BarDate: r.Decision.BarDate, FillDate: r.Decision.FillDate, RiskCheck: *r.Demo.Risk}
		}
		if r.Demo.Action == etoroledger.ActionBlocked {
			br.BlockedDays = append(br.BlockedDays, r.Decision.BarDate)
		}
	}
	if d := newestDemo(recs); d != nil && d.Demo.Risk != nil {
		br.Findings = append(br.Findings, d.Demo.Risk.Findings...)
	}
	if p.Halted {
		br.Findings = append(br.Findings, risk.Finding{Rule: risk.RuleHalted, Severity: risk.Block,
			Message: "halted: the next run records its planned order as blocked instead of sending it (" + p.HaltReason + ")"})
	}
	if n := len(br.BlockedDays); n > 0 {
		br.Findings = append(br.Findings, risk.Finding{Rule: RuleOrderBlocked, Severity: risk.Warn, Value: float64(n),
			Message: "the executor blocked the demo order for: " + join(br.BlockedDays) + " (recorded, not retried; paper account unaffected)"})
	}
	if lc := br.LastCheck; lc != nil && lc.PolicyVersion != p.Version {
		br.Findings = append(br.Findings, risk.Finding{Rule: RulePolicyMismatch, Severity: risk.Warn,
			Message: "the executor's last check (" + lc.BarDate + ") used policy " + lc.PolicyVersion + ", this page shows " + p.Version})
	}
	if f := risk.AssessDrawdown(p, book, "paper", last.Paper.MaxDrawdown); f != nil {
		br.Findings = append(br.Findings, *f)
	}
	if l.MaxExposurePctEquity > 0 && equity > 0 && br.ExposureUSD/equity > l.MaxExposurePctEquity {
		br.Findings = append(br.Findings, risk.Finding{Rule: risk.RuleMaxExposurePctEquity, Severity: risk.Warn,
			Limit: l.MaxExposurePctEquity, Value: br.ExposureUSD / equity,
			Message: fmt.Sprintf("open exposure %.0f USD is %.1f%% of equity, above the %.0f%% cap (no new open passes the check)",
				br.ExposureUSD, br.ExposurePctEquity, l.MaxExposurePctEquity*100)})
	}
	if rs := etoroRunStatus(rule, now); rs.Status == "missed" {
		br.Findings = append(br.Findings, risk.Finding{Rule: RuleRunMissed, Severity: risk.Warn,
			Value: float64(len(rs.MissingDays)), Message: "missing ledger days: " + join(rs.MissingDays)})
	}
	if last.Candles.Missing > 0 {
		br.Findings = append(br.Findings, risk.Finding{Rule: RuleCandlesMissing, Severity: risk.Warn, Value: float64(last.Candles.Missing),
			Message: fmt.Sprintf("%d NYSE trading day(s) missing from the candle history %s..%s", last.Candles.Missing, last.Candles.First, last.Candles.Last)})
	}
	sort.SliceStable(br.Findings, func(i, j int) bool {
		return br.Findings[i].Severity == risk.Block && br.Findings[j].Severity != risk.Block
	})
	return br
}
