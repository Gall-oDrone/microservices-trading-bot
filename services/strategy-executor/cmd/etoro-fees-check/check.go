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
	// Leverage and Exposure (2026-10-22): x1 on the cells; the canary holds
	// 50 USD at x20 = 1,000 USD of exposure, and financing is on exposure.
	Leverage  int     `json:"leverage,omitempty"`
	Exposure  float64 `json:"exposure_usd"`
	NetProfit float64 `json:"net_profit_usd"` // venue's realized P&L before fees (closed only)
	PriceLoss float64 `json:"price_pnl_usd"`  // units x (close - open), closed only
	// VenueFees is eToro's own figure: history `fees` for a closed position,
	// portfolio `totalFees` for an open one.
	VenueFees float64 `json:"venue_fees_usd"`
	// ModelFees is exposure x overnight bps x nights, the frozen model's charge.
	ModelFees float64 `json:"model_fees_usd"`
	Ratio     float64 `json:"venue_over_model"` // 0 when model is 0
}

// Night is one observed night of one position (from observations.jsonl,
// the canary's measurement series): what the venue charged between two
// observations, what its cost preview quoted for one night at the time,
// and what the frozen model says for the same exposure.
type Night struct {
	PositionID string  `json:"position_id"`
	NYDate     string  `json:"ny_date"`
	ObservedAt string  `json:"observed_at"`
	Nights     int     `json:"nights_held"`
	Exposure   float64 `json:"exposure_usd"`
	// Measured is the night's fees delta as charged (sign as the venue
	// reports it: negative = a charge); Preview the preview's overnight fee
	// on that observation; Frozen exposure x bps / 1e4, positive.
	Measured float64 `json:"measured_usd"`
	Preview  float64 `json:"preview_overnight_usd"`
	Frozen   float64 `json:"frozen_usd"`
	// Weekend flags a delta about three times the position's median night
	// (the triple charge indices carry once a week; measurement M2).
	Weekend bool `json:"weekend,omitempty"`
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
	// Nights (2026-10-22) is the per-night section from observations.jsonl
	// when the ledger has one (the real canary, or its rehearsal); nil
	// otherwise. NightsSource names the file.
	Nights       []Night `json:"nights,omitempty"`
	NightsSource string  `json:"nights_source,omitempty"`
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
// one against the model. It makes read calls only. env labels the report
// ("demo" | "real"): the model is the same, financing is on exposure.
func check(ctx context.Context, b broker.Broker, recs []etoroledger.Record, since time.Time, overnightBps float64, now time.Time, only map[string]bool) (Report, error) {
	return checkEnv(ctx, b, recs, since, overnightBps, now, only, "demo")
}

func checkEnv(ctx context.Context, b broker.Broker, recs []etoroledger.Record, since time.Time, overnightBps float64, now time.Time, only map[string]bool, env string) (Report, error) {
	rep := Report{Schema: Schema, GeneratedAt: now.UTC().Format(time.RFC3339), Env: env,
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
		exp := exposureOf(c.Amount, c.Leverage, 0)
		r := Row{PositionID: c.ID, Symbol: c.Instrument.Symbol, State: "closed", Source: "venue_only",
			OpenedAt: c.OpenedAt.UTC().Format(time.RFC3339), ClosedAt: c.ClosedAt.UTC().Format(time.RFC3339),
			Nights: n, Units: c.Units, OpenRate: c.OpenRate, CloseRate: c.CloseRate, Amount: c.Amount, Leverage: leverageOf(c.Leverage), Exposure: exp,
			NetProfit: c.NetProfit, PriceLoss: c.Units * (c.CloseRate - c.OpenRate), VenueFees: c.Fees,
			ModelFees: exp * rate * float64(n)}
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
		exp := exposureOf(p.Amount, p.Leverage, p.Exposure)
		r := Row{PositionID: p.ID, Symbol: p.Instrument.Symbol, State: "open", Source: "venue_only",
			OpenedAt: p.OpenedAt.UTC().Format(time.RFC3339), Nights: n, Units: p.Units, OpenRate: p.OpenRate,
			Amount: p.Amount, Leverage: leverageOf(p.Leverage), Exposure: exp, VenueFees: p.Fees, ModelFees: exp * rate * float64(n)}
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
		rep.Notes = append(rep.Notes, fmt.Sprintf("%d position-nights, venue fees 0.00 USD, model %.2f USD: the %s account charges no overnight financing (or books it where neither the history's fees nor the portfolio's totalFees show it)", nightsTotal, rep.SumModel, env))
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

func leverageOf(lev int) int {
	if lev < 1 {
		return 1
	}
	return lev
}

// exposureOf is the notional the financing applies to: what the venue
// reports when it does, else amount x leverage (= amount at x1).
func exposureOf(amount float64, lev int, reported float64) float64 {
	if reported > 0 {
		return reported
	}
	return amount * float64(leverageOf(lev))
}

// addNights fills Report.Nights from the observations series (the
// canary's measurement instrument): one row per recorded fees delta, with
// the preview's overnight fee on the same observation and the frozen model
// on the same exposure, and the weekend flag on a delta about three times
// the position's median night. Nothing is read from the venue here.
func addNights(rep *Report, obs []etoroledger.Observation, source string) {
	if len(obs) == 0 {
		return
	}
	rep.NightsSource = source
	rate := rep.OvernightBps / 1e4
	deltas := etoroledger.FeeDeltas(obs)
	perPos := map[string][]float64{}
	for _, d := range deltas {
		perPos[d.PositionID] = append(perPos[d.PositionID], math.Abs(d.Delta))
	}
	median := map[string]float64{}
	for id, xs := range perPos {
		s := append([]float64(nil), xs...)
		sort.Float64s(s)
		median[id] = s[len(s)/2]
	}
	for _, d := range deltas {
		n := Night{PositionID: d.PositionID, NYDate: d.NYDate, ObservedAt: d.At, Nights: d.Nights, Exposure: d.Exposure,
			Measured: d.Delta, Preview: d.PreviewOvernight, Frozen: d.Exposure * rate}
		if m := median[d.PositionID]; m > 0 && math.Abs(d.Delta) >= 2.5*m {
			n.Weekend = true
		}
		rep.Nights = append(rep.Nights, n)
	}
	if len(rep.Nights) == 0 {
		rep.Notes = append(rep.Notes, fmt.Sprintf("%s has %d observation(s) but no night-over-night delta yet (the first observation of a position has none)", source, len(obs)))
		return
	}
	var measured, preview, frozen float64
	weekends := 0
	for _, n := range rep.Nights {
		measured += n.Measured
		preview += n.Preview
		frozen += n.Frozen
		if n.Weekend {
			weekends++
		}
	}
	rep.Notes = append(rep.Notes, fmt.Sprintf("as observed over %d night(s) in %s: measured %.4f USD, the preview quoted %.4f USD, the frozen model %.4f USD (%d night(s) flagged as a weekend charge)",
		len(rep.Nights), source, measured, preview, frozen, weekends))
}

// markdown renders the report the way the evidence directories read.
func markdown(rep Report) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# eToro %s: overnight fees, venue vs frozen model\n\n", rep.Env)
	fmt.Fprintf(&sb, "Generated %s · history since %s · model **%.1f bps per calendar night** on the position's exposure (= the amount at x1)\n\n", rep.GeneratedAt, rep.Since, rep.OvernightBps)
	fmt.Fprintf(&sb, "**Verdict: `%s`**\n\n", rep.Verdict)
	for _, n := range rep.Notes {
		fmt.Fprintf(&sb, "- %s\n", n)
	}
	for _, w := range rep.Warnings {
		fmt.Fprintf(&sb, "- ⚠ %s\n", w)
	}
	fmt.Fprintf(&sb, "\n| Position | Book | State | Opened (UTC) | Closed (UTC) | Nights | Amount USD | Lev | Exposure USD | Price P&L | Net profit | Venue fees | Model fees | Ratio |\n|---|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range rep.Rows {
		book := r.Book
		if book == "" {
			book = "—"
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s | %d | %.2f | x%d | %.2f | %.2f | %.2f | **%.2f** | %.2f | %.2f |\n",
			r.PositionID, book, r.State, r.OpenedAt, orDash(r.ClosedAt), r.Nights, r.Amount, leverageOf(r.Leverage), r.Exposure, r.PriceLoss, r.NetProfit, r.VenueFees, r.ModelFees, r.Ratio)
	}
	fmt.Fprintf(&sb, "| **Sum** | | | | | | | | | | | **%.2f** | **%.2f** | |\n", rep.SumVenue, rep.SumModel)
	if len(rep.Nights) > 0 {
		fmt.Fprintf(&sb, "\n## Per night, as observed (%s)\n\n", rep.NightsSource)
		fmt.Fprintf(&sb, "Each row is the change in the position's `totalFees` between two daily observations (18:00 New York), next to what eToro's cost preview quoted for one night at the time and what the frozen model says for the same exposure. Measured and preview carry the venue's sign (negative = a charge); the model is positive.\n\n")
		fmt.Fprintf(&sb, "| NY date | Position | Nights held | Exposure USD | Measured | Preview (1 night) | Frozen | Weekend? |\n|---|---|---:|---:|---:|---:|---:|---|\n")
		for _, n := range rep.Nights {
			wk := ""
			if n.Weekend {
				wk = "yes"
			}
			fmt.Fprintf(&sb, "| %s | %s | %d | %.2f | **%.4f** | %.4f | %.4f | %s |\n", n.NYDate, n.PositionID, n.Nights, n.Exposure, n.Measured, n.Preview, n.Frozen, wk)
		}
	}
	return sb.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
