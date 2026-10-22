package etoroledger

import (
	"fmt"
	"math"
	"time"
)

// VenueCloseTolerance is how close (as a fraction of the rate) the
// history's close rate must be to the stop or the target to be read as
// that protective order having fired: 5 bps covers the venue's rounding
// and a fill a tick through the level. Further through the stop is still a
// stop-out, with the gap noted. Shared by the executor's phase C
// (cmd/etoro-daily-executor/protect.go) and, since 2026-10-22, by
// etoro-reconcile -record-venue-close for the real-account canary, whose
// ledger no executor ever runs on.
const VenueCloseTolerance = 0.0005

// ClassifyVenueClose explains a close the venue made on its own from the
// rates: at or through the stop -> stopped_out (gap in bps, positive =
// through), at or beyond the target -> took_profit, between the two ->
// venue_close (closed in the app, or a margin close-out). sl / tp are the
// protective rates the position carried (0 = none); why is the sentence
// the ledger line carries. Pure: no I/O, no clock.
func ClassifyVenueClose(sl, tp, closeRate float64) (kind string, gapBps float64, why string) {
	switch {
	case sl > 0 && closeRate <= sl*(1+VenueCloseTolerance):
		gapBps = (sl - closeRate) / sl * 1e4
		why = fmt.Sprintf("close rate %.2f at the stop %.2f", closeRate, sl)
		if gapBps > VenueCloseTolerance*1e4 {
			why = fmt.Sprintf("close rate %.2f is %.1f bps through the stop %.2f (gap)", closeRate, gapBps, sl)
		}
		return VenueCloseStoppedOut, gapBps, why
	case tp > 0 && closeRate >= tp*(1-VenueCloseTolerance):
		gapBps = (tp - closeRate) / tp * 1e4
		why = fmt.Sprintf("close rate %.2f at the target %.2f", closeRate, tp)
		if -gapBps > VenueCloseTolerance*1e4 {
			why = fmt.Sprintf("close rate %.2f is %.1f bps beyond the target %.2f (gap)", closeRate, -gapBps, tp)
		}
		return VenueCloseTookProfit, gapBps, why
	}
	return VenueCloseOther, 0, fmt.Sprintf("close rate %.2f is between the stop %.2f and the target %.2f: not ours and not a protective order (closed in the app, or a margin close-out)", closeRate, sl, tp)
}

// VenueCloseRefPrefix starts the synthetic client ref of a venue_close line
// recorded by hand (etoro-reconcile -record-venue-close): never sent to
// eToro, it only keys the line.
const VenueCloseRefPrefix = "venue-close-"

// VenueCloseRecord builds the ledger line that records a venue close of
// the book's held position from the trading history's row: the executor's
// shape (action venue_close, position_after flat, the history's fill, P&L
// and fees, the kind and the gap), keyed by the New York date of the close
// so it never collides with another line. source is "" for the executor,
// SourceUI-like markers for a by-hand record (etoro-reconcile writes
// "reconcile"). The caller appends it.
type VenueCloseInput struct {
	Book       string
	Instrument Instrument
	Mode       string // ModeDemo | ModeReal
	Env        string // "demo" | "real"
	Position   Position
	// The history's row.
	CloseRate   float64
	ClosedAt    time.Time
	Units       float64
	Amount      float64
	NetProfit   float64
	Fees        float64
	HistorySL   float64 // the rates the history reports (0 = none); the ledger's are the fallback
	HistoryTP   float64
	Now         time.Time
	NYDate      string // the New York date of the close (Decision.BarDate / FillDate)
	CodeVersion string
	Source      string
	Operator    *Operator
	// Account after the close, when read (nil otherwise).
	Account *Account
	// RuleSignal / RuleSMA / RefClose describe the rule's latest record on
	// the book (zero on a ledger without one, like the canary's).
	RuleSignal string
	RuleSMA    float64
	RefClose   float64
}

// NewVenueCloseRecord is the record for in. The stop and target are the
// history's when it reports them, else the ledger's.
func NewVenueCloseRecord(in VenueCloseInput) Record {
	sl, tp := in.HistorySL, in.HistoryTP
	if !(sl > 0.01) {
		sl = in.Position.StopLossRate
	}
	if !(tp > 0.01) {
		tp = in.Position.TakeProfitRate
	}
	kind, gap, why := ClassifyVenueClose(sl, tp, in.CloseRate)
	vc := &VenueClose{Kind: kind, StopLossRate: sl, TakeProfitRate: tp, CloseRate: in.CloseRate, GapBps: gap,
		ClosedAt: in.ClosedAt.UTC().Format(time.RFC3339), NetProfit: in.NetProfit, Fees: in.Fees}
	if kind == VenueCloseOther {
		vc.GapBps = 0
	}
	units := in.Units
	if units == 0 {
		units = in.Position.Units
	}
	amount := in.Amount
	if amount == 0 {
		amount = in.Position.Amount
	}
	order := &Order{Kind: ActionVenueClose, Status: "executed", PositionIDs: []string{in.Position.PositionID},
		AvgPrice: in.CloseRate, Units: units, Amount: amount, Fees: in.Fees, Started: in.Now.UTC(), Finished: in.ClosedAt.UTC()}
	if in.Source == SourceUI {
		// A by-hand record is keyed like a console line (book, bar_date,
		// client_ref); nothing was sent to eToro, so the ref is synthetic
		// and names the position. VenueCloseRefPrefix tells it apart.
		order.ClientRef = VenueCloseRefPrefix + in.Position.PositionID
	}
	d := &Demo{Env: in.Env, Target: "flat", Action: ActionVenueClose, PositionBefore: in.Position, PositionAfter: Flat(), Account: in.Account,
		Order: order, VenueClose: vc, Notes: []string{"closed by eToro: " + why + " -> " + kind}}
	if in.Position.OpenRate > 0 && units > 0 {
		// The gross price P&L from the ledger's open to the history's close,
		// for the reader; the history's net profit is the venue's figure.
		d.Notes = append(d.Notes, fmt.Sprintf("units x (close - open) = %.4f x (%.2f - %.2f) = %.2f; history net profit %.2f, fees %.2f",
			units, in.CloseRate, in.Position.OpenRate, units*(in.CloseRate-in.Position.OpenRate), in.NetProfit, in.Fees))
	}
	mode := in.Mode
	if mode == "" {
		mode = ModeDemo
	}
	return Record{
		Schema: Schema, RecordedAt: in.Now.UTC().Format(time.RFC3339), CodeVersion: in.CodeVersion,
		Mode: mode, Venue: "etoro", Book: in.Book, Instrument: in.Instrument,
		Decision: Decision{BarDate: in.NYDate, FillDate: in.NYDate, Close: in.RefClose, SMA: in.RuleSMA, Signal: SignalManual, PrevSignal: in.RuleSignal, Action: "sell"},
		Demo:     d, Source: in.Source, Operator: in.Operator,
	}
}

// Gap in money: units x |gap rate|, for a reader that wants USD.
func (v VenueClose) GapUSD(units float64) float64 {
	if v.Kind != VenueCloseStoppedOut || v.StopLossRate <= 0 {
		return 0
	}
	return math.Abs(units * (v.StopLossRate - v.CloseRate))
}
