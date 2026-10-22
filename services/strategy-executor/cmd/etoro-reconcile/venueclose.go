package main

// Recording a venue close by hand (2026-10-22, the real-account canary,
// docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md §8.4, measurement M6).
//
// On the demo cells the executor's 09:35 run records a position eToro
// closed on its own (stop, target, margin call) as an action venue_close
// line (protect.go C3). No executor ever runs on the canary ledger, so the
// same line is written here, on request: -record-venue-close looks at every
// book whose ledger says long while the account does not hold the
// position, finds the close in the trading history and appends the line
// through the shared builder (etoroledger.NewVenueCloseRecord: same
// classification, same words as the executor's). Not listed yet: nothing
// is written, the finding stays missing_position and the operator re-runs
// later (the history lagged a close by up to 3 h on 2026-10-16). This is
// the only write reconcile ever makes, and only with the flag; it never
// touches the venue.

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
)

// venueCloseOutcome is what happened for one book.
type venueCloseOutcome struct {
	Book       string  `json:"book"`
	PositionID string  `json:"position_id"`
	Recorded   bool    `json:"recorded"`
	Kind       string  `json:"kind,omitempty"`
	GapBps     float64 `json:"gap_bps,omitempty"`
	Note       string  `json:"note"`
}

// recordVenueCloses writes a venue_close line for every book the ledger
// holds long and the account does not, when the history lists the close.
// env is "demo" | "real"; mode follows it. by / reason go on the line as
// the operator (the record is a by-hand act).
func recordVenueCloses(ctx context.Context, b broker.Broker, ledgerPath string, positions []broker.Position, now time.Time, env, codeVersion, by, reason string) ([]venueCloseOutcome, error) {
	hr, ok := b.(broker.HistoryReader)
	if !ok {
		return nil, fmt.Errorf("the broker adapter has no trading history reader")
	}
	led, err := etoroledger.Open(ledgerPath)
	if err != nil {
		return nil, err
	}
	recs := led.Records()
	held := map[string]bool{}
	for _, p := range positions {
		held[p.ID] = true
	}
	books := map[string]etoroledger.Instrument{}
	var order []string
	for _, r := range recs {
		if _, seen := books[r.Book]; !seen {
			order = append(order, r.Book)
		}
		books[r.Book] = r.Instrument
	}
	var out []venueCloseOutcome
	var closed []broker.ClosedPosition
	fetched := false
	for _, book := range order {
		pos := etoroledger.LastDemoPosition(recs, book)
		if pos.State != "long" || held[pos.PositionID] {
			continue
		}
		o := venueCloseOutcome{Book: book, PositionID: pos.PositionID}
		if !fetched {
			since := now.Add(-300 * 24 * time.Hour)
			if t, perr := time.Parse(time.RFC3339, pos.OpenedAt); perr == nil && t.After(since) {
				since = t.Add(-24 * time.Hour)
			}
			if closed, err = hr.ClosedPositions(ctx, since); err != nil {
				return out, fmt.Errorf("trading history: %w", err)
			}
			fetched = true
		}
		var c *broker.ClosedPosition
		for i := range closed {
			if closed[i].ID == pos.PositionID {
				c = &closed[i]
				break
			}
		}
		if c == nil {
			o.Note = "the trading history does not list the close yet: nothing recorded, re-run later"
			out = append(out, o)
			continue
		}
		var ruleSignal string
		var ruleSMA, refClose float64
		for _, r := range recs {
			if r.Book == book && !r.IsUI() {
				ruleSignal, ruleSMA, refClose = r.Decision.Signal, r.Decision.SMA, r.Decision.Close
			}
		}
		var account *etoroledger.Account
		if acct, aerr := b.Account(ctx); aerr == nil {
			account = &etoroledger.Account{Currency: acct.Currency, Cash: acct.Cash, Equity: acct.Equity}
		}
		mode := etoroledger.ModeDemo
		if env == "real" {
			mode = etoroledger.ModeReal
		}
		rec := etoroledger.NewVenueCloseRecord(etoroledger.VenueCloseInput{
			Book: book, Instrument: books[book], Mode: mode, Env: env, Position: pos,
			CloseRate: c.CloseRate, ClosedAt: c.ClosedAt, Units: c.Units, Amount: c.Amount, NetProfit: c.NetProfit, Fees: c.Fees,
			HistorySL: c.StopLoss, HistoryTP: c.TakeProfit, Now: now, NYDate: c.ClosedAt.In(mktcal.NewYork).Format("2006-01-02"),
			CodeVersion: codeVersion, Source: etoroledger.SourceUI, Operator: &etoroledger.Operator{By: by, Reason: reason},
			Account: account, RuleSignal: ruleSignal, RuleSMA: ruleSMA, RefClose: refClose,
		})
		if err := led.Append(rec); err != nil {
			return out, fmt.Errorf("%s: append venue_close: %w", book, err)
		}
		o.Recorded, o.Kind, o.GapBps = true, rec.Demo.VenueClose.Kind, rec.Demo.VenueClose.GapBps
		o.Note = rec.Demo.Notes[0]
		out = append(out, o)
	}
	return out, nil
}

// canaryLedger reports whether path lives under a directory named
// real-canary (the only ledger a real env file may be used with).
func canaryLedger(path string) bool {
	return filepath.Base(filepath.Dir(path)) == "real-canary"
}

// envTag marks a real-account report in the first printed line.
func envTag(env string) string {
	if env == "real" {
		return " | REAL ACCOUNT"
	}
	return ""
}
