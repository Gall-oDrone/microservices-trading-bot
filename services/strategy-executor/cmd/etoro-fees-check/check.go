package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoroledger"
)

// Schema is Report.Schema.
const Schema = "etoro-fees-check/v1"

// Row is one position, closed or still open, with the venue's fee next to
// the frozen model's charge for the same nights.
type Row struct {
	PositionID string  `json:"position_id"`
	Symbol     string  `json:"symbol"`
	Book       string  `json:"book,omitempty"` // from the ledger when the position is the executor's
	Source     string  `json:"source"`         // "ledger" | "venue_only"
	State      string  `json:"state"`          // "closed" | "open"
	OpenedAt   string  `json:"opened_at"`
	ClosedAt   string  `json:"closed_at,omitempty"`
	Nights     int     `json:"calendar_nights"`
	Units      float64 `json:"units"`
	OpenRate   float64 `json:"open_rate"`
	CloseRate  float64 `json:"close_rate,omitempty"`
	Amount     float64 `json:"amount_usd"`
	NetProfit  float64 `json:"net_profit_usd"` // venue's realized P&L before fees (closed only)
	PriceLoss  float64 `json:"price_pnl_usd"`  // units x (close - open), closed only
	// VenueFees is eToro's own figure: history `fees` for a closed position,
	// portfolio `totalFees` for an open one.
	VenueFees float64 `json:"venue_fees_usd"`
	// ModelFees is amount x overnight bps x nights, the frozen model's charge.
	ModelFees float64 `json:"model_fees_usd"`
	Ratio     float64 `json:"venue_over_model"` // 0 when model is 0
}

// Report is what the command writes.
type Report struct {
	Schema       string   `json:"schema"`
	GeneratedAt  string   `json:"generated_at"`
	Env          string   `json:"env"`
	OvernightBps float64  `json:"overnight_bps_per_night"`
	Since        string   `json:"history_since"`
	Rows         []Row    `json:"rows"`
	SumVenue     float64  `json:"sum_venue_fees_usd"`
	SumModel     float64  `json:"sum_model_fees_usd"`
	Verdict      string   `json:"verdict"`
	Notes        []string `json:"notes,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
}

// Verdicts.
const (
	VerdictCharged    = "charged_as_modelled"    // venue fees within 25 % of the model
	VerdictNotCharged = "not_charged"            // venue fees are zero over >= 1 night
	VerdictDiffers    = "charged_differently"    // non-zero but off the model by > 25 %
	VerdictNoData     = "no_positions_in_window" // nothing to judge
)

// ledgerPositions maps position id -> book from the ledger's demo lines
// (executor and console), so a venue row can be labelled.
func ledgerPositions(recs []etoroledger.Record) map[string]string {
	out := map[string]string{}
	for _, r := range recs {
		if r.Demo == nil {
			continue
		}
		for _, p := range []etoroledger.Position{r.Demo.PositionBefore, r.Demo.PositionAfter} {
			if p.PositionID != "" {
				out[p.PositionID] = r.Book
			}
		}
	}
	return out
}

// nights counts the calendar nights between two instants, the way eToro
// charges them (one per 00:00 UTC boundary crossed; weekends included).
func nights(open, closeAt time.Time) int {
	if closeAt.Before(open) {
		return 0
	}
	a := time.Date(open.Year(), open.Month(), open.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(closeAt.Year(), closeAt.Month(), closeAt.Day(), 0, 0, 0, 0, time.UTC)
	return int(math.Round(b.Sub(a).Hours() / 24))
}

// check reads the trading history and the open positions and prices every
// one against the model. It makes read calls only.
func check(ctx context.Context, b broker.Broker, recs []etoroledger.Record, since time.Time, overnightBps float64, now time.Time, only map[string]bool) (Report, error) {
	rep := Report{Schema: Schema, GeneratedAt: now.UTC().Format(time.RFC3339), Env: "demo",
		OvernightBps: overnightBps, Since: since.UTC().Format(time.RFC3339)}
	books := ledgerPositions(recs)
	rate := overnightBps / 1e4

	hr, ok := b.(broker.HistoryReader)
	if !ok {
		return rep, fmt.Errorf("the broker adapter has no trading history reader")
	}
	closed, err := hr.ClosedPositions(ctx, since)
	if err != nil {
		return rep, fmt.Errorf("trading history: %w", err)
	}
	open, err := b.Positions(ctx)
	if err != nil {
		return rep, fmt.Errorf("positions: %w", err)
	}

	keep := func(id string) bool { return len(only) == 0 || only[id] }
	for _, c := range closed {
		if !keep(c.ID) {
			continue
		}
		n := nights(c.OpenedAt, c.ClosedAt)
		r := Row{PositionID: c.ID, Symbol: c.Instrument.Symbol, State: "closed", Source: "venue_only",
			OpenedAt: c.OpenedAt.UTC().Format(time.RFC3339), ClosedAt: c.ClosedAt.UTC().Format(time.RFC3339),
			Nights: n, Units: c.Units, OpenRate: c.OpenRate, CloseRate: c.CloseRate, Amount: c.Amount,
			NetProfit: c.NetProfit, PriceLoss: c.Units * (c.CloseRate - c.OpenRate), VenueFees: c.Fees,
			ModelFees: c.Amount * rate * float64(n)}
		if bk, ok := books[c.ID]; ok {
			r.Book, r.Source = bk, "ledger"
		}
		rep.Rows = append(rep.Rows, r)
	}
	for _, p := range open {
		if !keep(p.ID) {
			continue
		}
		n := nights(p.OpenedAt, now)
		r := Row{PositionID: p.ID, Symbol: p.Instrument.Symbol, State: "open", Source: "venue_only",
			OpenedAt: p.OpenedAt.UTC().Format(time.RFC3339), Nights: n, Units: p.Units, OpenRate: p.OpenRate,
			Amount: p.Amount, VenueFees: p.Fees, ModelFees: p.Amount * rate * float64(n)}
		if bk, ok := books[p.ID]; ok {
			r.Book, r.Source = bk, "ledger"
		}
		rep.Rows = append(rep.Rows, r)
	}
	sort.Slice(rep.Rows, func(i, j int) bool { return rep.Rows[i].OpenedAt < rep.Rows[j].OpenedAt })

	nightsTotal := 0
	for i := range rep.Rows {
		r := &rep.Rows[i]
		if r.ModelFees > 0 {
			r.Ratio = r.VenueFees / r.ModelFees
		}
		rep.SumVenue += r.VenueFees
		rep.SumModel += r.ModelFees
		nightsTotal += r.Nights
	}
	for _, id := range sortedKeys(only) {
		found := false
		for _, r := range rep.Rows {
			if r.PositionID == id {
				found = true
			}
		}
		if !found {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("position %s was asked for but is neither in the history since %s nor open", id, rep.Since))
		}
	}
	switch {
	case len(rep.Rows) == 0 || nightsTotal == 0:
		rep.Verdict = VerdictNoData
	case rep.SumVenue == 0:
		rep.Verdict = VerdictNotCharged
		rep.Notes = append(rep.Notes, fmt.Sprintf("%d position-nights, venue fees 0.00 USD, model %.2f USD: the demo account charges no overnight financing (or books it where neither the history's fees nor the portfolio's totalFees show it)", nightsTotal, rep.SumModel))
	case math.Abs(rep.SumVenue/rep.SumModel-1) <= 0.25:
		rep.Verdict = VerdictCharged
		rep.Notes = append(rep.Notes, fmt.Sprintf("venue %.2f USD vs model %.2f USD over %d nights (ratio %.2f): the frozen %.1f bps/night stands", rep.SumVenue, rep.SumModel, nightsTotal, rep.SumVenue/rep.SumModel, overnightBps))
	default:
		rep.Verdict = VerdictDiffers
		rep.Notes = append(rep.Notes, fmt.Sprintf("venue %.2f USD vs model %.2f USD over %d nights (ratio %.2f): charged, but not at the frozen rate; report both", rep.SumVenue, rep.SumModel, nightsTotal, rep.SumVenue/rep.SumModel))
	}
	rep.Notes = append(rep.Notes, "the frozen costs of the pre-registration decide pass/fail regardless of this verdict; this report turns an assumed cost into a measured one (pre-registration §5, execution note)")
	return rep, nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// markdown renders the report the way the evidence directories read.
func markdown(rep Report) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# eToro demo: overnight fees, venue vs frozen model\n\n")
	fmt.Fprintf(&sb, "Generated %s · history since %s · model **%.1f bps per calendar night** on the position amount\n\n", rep.GeneratedAt, rep.Since, rep.OvernightBps)
	fmt.Fprintf(&sb, "**Verdict: `%s`**\n\n", rep.Verdict)
	for _, n := range rep.Notes {
		fmt.Fprintf(&sb, "- %s\n", n)
	}
	for _, w := range rep.Warnings {
		fmt.Fprintf(&sb, "- ⚠ %s\n", w)
	}
	fmt.Fprintf(&sb, "\n| Position | Book | State | Opened (UTC) | Closed (UTC) | Nights | Amount USD | Price P&L | Net profit | Venue fees | Model fees | Ratio |\n|---|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range rep.Rows {
		book := r.Book
		if book == "" {
			book = "—"
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s | %d | %.2f | %.2f | %.2f | **%.2f** | %.2f | %.2f |\n",
			r.PositionID, book, r.State, r.OpenedAt, orDash(r.ClosedAt), r.Nights, r.Amount, r.PriceLoss, r.NetProfit, r.VenueFees, r.ModelFees, r.Ratio)
	}
	fmt.Fprintf(&sb, "| **Sum** | | | | | | | | | **%.2f** | **%.2f** | |\n", rep.SumVenue, rep.SumModel)
	return sb.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
