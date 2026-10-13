package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoroledger"
)

// Severities. Only drift makes a report unclean.
const (
	sevDrift = "drift"
	sevWarn  = "warn"
)

// Finding codes.
const (
	codeMissingPosition  = "missing_position"  // ledger long, account does not hold the position
	codeOrphanPosition   = "orphan_position"   // account holds a position on the instrument the ledger does not
	codeUnitsDrift       = "units_drift"       // held position's units differ from the ledger's
	codeUnfinishedIntent = "unfinished_intent" // an order was sent but no ledger line records it
	codeCloseNotInHist   = "close_not_in_history"
	codeClosePriceDiff   = "close_price_differs"
	codeRejectedOrder    = "rejected_order"
	codeBlockedDay       = "blocked_day"
	codeForeignPositions = "other_instruments"
	codePortfolioLag     = "portfolio_lag" // the portfolio needed re-reads before it showed a fresh fill (warn)
)

// settle is how long reconcile waits for the portfolio to show an order the
// executor finished moments ago. eToro's portfolio lags fills: on 2026-10-12
// an open executed at 13:35:09Z was still absent from the portfolio read at
// 13:35:14Z and present later, which was reported as missing_position. The
// wait applies only to books whose newest order finished within Window and
// only while the first read disagrees with the ledger; a run that reads a
// consistent portfolio never waits.
type settle struct {
	Window   time.Duration // orders finished this recently may still be settling
	Timeout  time.Duration // total time to keep re-reading
	Interval time.Duration // pause between reads
	Sleep    func(context.Context, time.Duration) error
}

// defaultSettle covers the executor's own 30 s fill poll plus the gap seen
// on 2026-10-12, without holding up a cron run for long.
var defaultSettle = settle{Window: 10 * time.Minute, Timeout: 60 * time.Second, Interval: 5 * time.Second, Sleep: sleepCtx}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type finding struct {
	Severity string `json:"severity"`
	Book     string `json:"book,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type bookReport struct {
	Book             string   `json:"book"`
	Symbol           string   `json:"symbol"`
	InstrumentID     int64    `json:"instrument_id"`
	LastBarDate      string   `json:"last_bar_date"`
	LedgerState      string   `json:"ledger_state"`
	LedgerPositionID string   `json:"ledger_position_id,omitempty"`
	LedgerUnits      float64  `json:"ledger_units,omitempty"`
	AccountPositions []string `json:"account_positions"`
	FinancingToDate  float64  `json:"financing_to_date,omitempty"`
	OrdersChecked    int      `json:"orders_checked"`
}

type report struct {
	Schema        string          `json:"schema"`
	GeneratedAt   string          `json:"generated_at"`
	Ledger        string          `json:"ledger"`
	Clean         bool            `json:"clean"`
	Account       *broker.Account `json:"account,omitempty"`
	PositionReads int             `json:"position_reads"`            // portfolio reads made (1 unless the portfolio lagged)
	SettleWaitedS float64         `json:"settle_waited_s,omitempty"` // seconds spent waiting for the portfolio
	Books         []bookReport    `json:"books"`
	Findings      []finding       `json:"findings"`
}

// reconcile compares the demo records of the ledger with the account
// (open positions), the trading history (closed positions) and the intent
// journal. It never writes anything and never places an order.
func reconcile(ctx context.Context, b broker.Broker, recs []etoroledger.Record, intents []etoroledger.Intent, now time.Time) (report, error) {
	return reconcileWith(ctx, b, recs, intents, now, defaultSettle)
}

func reconcileWith(ctx context.Context, b broker.Broker, recs []etoroledger.Record, intents []etoroledger.Intent, now time.Time, st settle) (report, error) {
	rep := report{Schema: "etoro-reconcile/v1", GeneratedAt: now.UTC().Format(time.RFC3339), Findings: []finding{}}
	add := func(sev, book, code, format string, args ...any) {
		rep.Findings = append(rep.Findings, finding{Severity: sev, Book: book, Code: code, Message: fmt.Sprintf(format, args...)})
	}
	positions, lag, err := settledPositions(ctx, b, recs, now, st)
	if err != nil {
		return rep, fmt.Errorf("positions: %w", err)
	}
	rep.PositionReads, rep.SettleWaitedS = lag.reads, lag.waited.Seconds()
	for _, book := range lag.lagged {
		add(sevWarn, book, codePortfolioLag, "the portfolio showed the ledger's latest order only after %d reads (%.0f s)", lag.reads, lag.waited.Seconds())
	}
	if acct, err := b.Account(ctx); err == nil {
		rep.Account = &acct
	}

	var demo []etoroledger.Record
	books := map[string]etoroledger.Instrument{}
	refs := map[string]bool{}
	var earliest time.Time
	for _, r := range recs {
		if r.Mode != etoroledger.ModeDemo || r.Demo == nil {
			continue
		}
		demo = append(demo, r)
		books[r.Book] = r.Instrument
		if o := r.Demo.Order; o != nil {
			refs[o.ClientRef] = true
			if !o.IntentAt.IsZero() && (earliest.IsZero() || o.IntentAt.Before(earliest)) {
				earliest = o.IntentAt
			}
		}
	}

	// Closed positions, for the executed closes (eToro: under one year).
	var closed map[string]broker.ClosedPosition
	if hr, ok := b.(broker.HistoryReader); ok && !earliest.IsZero() {
		since := earliest.Add(-24 * time.Hour)
		if min := now.Add(-360 * 24 * time.Hour); since.Before(min) {
			since = min
		}
		cps, err := hr.ClosedPositions(ctx, since)
		if err != nil {
			return rep, fmt.Errorf("trading history: %w", err)
		}
		closed = make(map[string]broker.ClosedPosition, len(cps))
		for _, c := range cps {
			closed[c.ID] = c
		}
	}

	names := make([]string, 0, len(books))
	for k := range books {
		names = append(names, k)
	}
	sort.Strings(names)
	ours := map[int64]bool{}
	for _, book := range names {
		in := books[book]
		ours[in.ID] = true
		br := bookReport{Book: book, Symbol: in.Symbol, InstrumentID: in.ID, AccountPositions: []string{}}
		pos := etoroledger.LastDemoPosition(demo, book)
		br.LedgerState, br.LedgerPositionID, br.LedgerUnits = pos.State, pos.PositionID, pos.Units
		for _, r := range demo {
			if r.Book == book && r.Decision.BarDate > br.LastBarDate {
				br.LastBarDate = r.Decision.BarDate
			}
		}
		var held *broker.Position
		for i, p := range positions {
			if p.Instrument.ID != in.ID {
				continue
			}
			br.AccountPositions = append(br.AccountPositions, p.ID)
			if p.ID == pos.PositionID && pos.State == "long" {
				held = &positions[i]
			} else {
				add(sevDrift, book, codeOrphanPosition, "account holds position %s on %s (%.6f units, opened %s) that the ledger does not",
					p.ID, in.Symbol, p.Units, p.OpenedAt.UTC().Format(time.RFC3339))
			}
		}
		if pos.State == "long" {
			if held == nil {
				add(sevDrift, book, codeMissingPosition, "ledger holds position %s on %s but the account does not", pos.PositionID, in.Symbol)
			} else {
				br.FinancingToDate = held.Fees
				if pos.Units > 0 && math.Abs(held.Units-pos.Units) > 1e-6*math.Max(1, pos.Units) {
					add(sevDrift, book, codeUnitsDrift, "position %s holds %.6f units, ledger says %.6f", pos.PositionID, held.Units, pos.Units)
				}
			}
		}
		for _, r := range demo {
			if r.Book != book {
				continue
			}
			d := r.Demo
			if d.Action == etoroledger.ActionBlocked {
				add(sevWarn, book, codeBlockedDay, "%s: order blocked by the risk check (fill date %s)", r.Decision.BarDate, r.Decision.FillDate)
			}
			o := d.Order
			if o == nil {
				continue
			}
			br.OrdersChecked++
			if o.Status != string(broker.StatusExecuted) {
				add(sevWarn, book, codeRejectedOrder, "%s: %s %s ended %s: %s", r.Decision.BarDate, o.Kind, o.ClientRef, o.Status, o.Error)
				continue
			}
			if o.Kind == etoroledger.ActionClose && closed != nil {
				pid := d.PositionBefore.PositionID
				c, ok := closed[pid]
				switch {
				case !ok:
					add(sevDrift, book, codeCloseNotInHist, "%s: close of position %s is not in the trading history", r.Decision.BarDate, pid)
				case o.AvgPrice > 0 && math.Abs(c.CloseRate/o.AvgPrice-1) > 1e-4:
					add(sevWarn, book, codeClosePriceDiff, "%s: position %s closed at %.2f per the history, ledger says %.2f", r.Decision.BarDate, pid, c.CloseRate, o.AvgPrice)
				}
			}
		}
		rep.Books = append(rep.Books, br)
	}
	var foreign []string
	for _, p := range positions {
		if !ours[p.Instrument.ID] {
			foreign = append(foreign, fmt.Sprintf("%s (instrument %d)", p.ID, p.Instrument.ID))
		}
	}
	if len(foreign) > 0 {
		add(sevWarn, "", codeForeignPositions, "positions on instruments the executor does not trade: %v", foreign)
	}
	for _, in := range intents {
		if !refs[in.ClientRef] {
			add(sevDrift, in.Book, codeUnfinishedIntent, "%s was sent (intent of %s) but no ledger line records it: re-run the executor to resume it",
				in.ClientRef, in.IntentAt.UTC().Format(time.RFC3339))
		}
	}
	rep.Clean = true
	for _, f := range rep.Findings {
		if f.Severity == sevDrift {
			rep.Clean = false
		}
	}
	return rep, nil
}

// lagInfo is what settledPositions did to get a consistent portfolio.
type lagInfo struct {
	reads  int
	waited time.Duration
	lagged []string // books that needed more than one read
}

// freshOrder is an executed order the executor finished within the settle
// window, and the account state the ledger expects for it.
type freshOrder struct {
	book       string
	positionID string
	wantHeld   bool // open: the position must be in the portfolio; close: it must not
}

// freshOrders returns, per book, the newest demo record's order when it was
// executed within st.Window of now (by Finished, else IntentAt).
func freshOrders(recs []etoroledger.Record, now time.Time, st settle) []freshOrder {
	newest := map[string]etoroledger.Record{}
	for _, r := range recs {
		if r.Mode != etoroledger.ModeDemo || r.Demo == nil {
			continue
		}
		if cur, ok := newest[r.Book]; !ok || r.Decision.BarDate > cur.Decision.BarDate {
			newest[r.Book] = r
		}
	}
	books := make([]string, 0, len(newest))
	for b := range newest {
		books = append(books, b)
	}
	sort.Strings(books)
	var out []freshOrder
	for _, b := range books {
		r := newest[b]
		o := r.Demo.Order
		if o == nil || o.Status != string(broker.StatusExecuted) {
			continue
		}
		at := o.Finished
		if at.IsZero() {
			at = o.IntentAt
		}
		if at.IsZero() || now.Sub(at) > st.Window || now.Before(at) {
			continue
		}
		switch o.Kind {
		case etoroledger.ActionOpen:
			if pid := r.Demo.PositionAfter.PositionID; pid != "" {
				out = append(out, freshOrder{book: b, positionID: pid, wantHeld: true})
			}
		case etoroledger.ActionClose:
			if pid := r.Demo.PositionBefore.PositionID; pid != "" {
				out = append(out, freshOrder{book: b, positionID: pid, wantHeld: false})
			}
		}
	}
	return out
}

// settledPositions reads the open positions, re-reading for up to st.Timeout
// while a book with a fresh order still disagrees with the ledger. It gives
// up quietly on timeout: the normal findings then describe the mismatch.
func settledPositions(ctx context.Context, b broker.Broker, recs []etoroledger.Record, now time.Time, st settle) ([]broker.Position, lagInfo, error) {
	var lag lagInfo
	fresh := freshOrders(recs, now, st)
	disagreeing := func(positions []broker.Position) []string {
		held := map[string]bool{}
		for _, p := range positions {
			held[p.ID] = true
		}
		var out []string
		for _, f := range fresh {
			if held[f.positionID] != f.wantHeld {
				out = append(out, f.book)
			}
		}
		return out
	}
	var first []string
	for {
		positions, err := b.Positions(ctx)
		if err != nil {
			return nil, lag, err
		}
		lag.reads++
		bad := disagreeing(positions)
		if lag.reads == 1 {
			first = bad
		}
		if len(bad) == 0 || st.Interval <= 0 || st.Sleep == nil || lag.waited+st.Interval > st.Timeout {
			if len(bad) == 0 {
				lag.lagged = first
			}
			return positions, lag, nil
		}
		if err := st.Sleep(ctx, st.Interval); err != nil {
			return positions, lag, err
		}
		lag.waited += st.Interval
	}
}
