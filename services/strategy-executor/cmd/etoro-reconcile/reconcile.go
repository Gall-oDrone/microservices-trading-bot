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
)

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
	Schema      string          `json:"schema"`
	GeneratedAt string          `json:"generated_at"`
	Ledger      string          `json:"ledger"`
	Clean       bool            `json:"clean"`
	Account     *broker.Account `json:"account,omitempty"`
	Books       []bookReport    `json:"books"`
	Findings    []finding       `json:"findings"`
}

// reconcile compares the demo records of the ledger with the account
// (open positions), the trading history (closed positions) and the intent
// journal. It never writes anything and never places an order.
func reconcile(ctx context.Context, b broker.Broker, recs []etoroledger.Record, intents []etoroledger.Intent, now time.Time) (report, error) {
	rep := report{Schema: "etoro-reconcile/v1", GeneratedAt: now.UTC().Format(time.RFC3339), Findings: []finding{}}
	add := func(sev, book, code, format string, args ...any) {
		rep.Findings = append(rep.Findings, finding{Severity: sev, Book: book, Code: code, Message: fmt.Sprintf(format, args...)})
	}
	positions, err := b.Positions(ctx)
	if err != nil {
		return rep, fmt.Errorf("positions: %w", err)
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
