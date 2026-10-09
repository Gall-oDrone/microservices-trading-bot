// Package reconcile checks the daily-executor's stage ledger against Bitso,
// the daily trade and position reconciliation a fund's middle office runs
// against its broker (plan §6.4.10).
//
//   - Legs: every recorded leg's trades are fetched again by its two client
//     ids (dailyexec.OriginIDs) and summed; quantity, notional, fees and the
//     base-balance change must match the ledger, and every trade's order id
//     must be one the ledger lists.
//   - Unrecorded fills: a day with a blocked order, or with no stage record
//     at all, must have no trades under that day's client ids (a crash
//     between a fill and the ledger write would leave exactly that).
//   - Ledger positions: each book's position equals the sum of its legs'
//     base changes.
//   - Balance: the stage account's BTC covers both books' positions. Not an
//     equality: the account holds BTC that is not the strategy's.
//
// It only reads. Source has no way to place or cancel an order.
package reconcile

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/strategy-executor/internal/bitsostage"
	"bitso-trading-platform/strategy-executor/internal/dailyexec"
)

// Source is the read-only part of the Bitso stage API reconciliation uses.
type Source interface {
	Balances() (map[string]float64, error)
	TradesByOrigin(originID string) ([]bitsostage.Trade, error)
}

// Statuses of a leg or day check.
const (
	StatusMatched    = "matched"
	StatusMismatch   = "mismatch"
	StatusUnrecorded = "unrecorded_fills" // trades the ledger does not know
	StatusClean      = "clean"            // a day without a leg had no trades, as it should
)

// Amounts is one side of a leg comparison.
type Amounts struct {
	Filled    float64            `json:"filled_btc"`
	Notional  float64            `json:"notional"`
	Fees      map[string]float64 `json:"fees"`
	BaseDelta float64            `json:"base_delta"`
	Trades    int                `json:"trades"`
}

// LegCheck is one recorded leg against Bitso's trades.
type LegCheck struct {
	Book        string   `json:"book"`
	BarDate     string   `json:"bar_date"`
	FillDate    string   `json:"fill_date"`
	Side        string   `json:"side"`
	Status      string   `json:"status"`
	Ledger      Amounts  `json:"ledger"`
	Exchange    Amounts  `json:"exchange"`
	Diffs       []string `json:"diffs"`
	UnknownOids []string `json:"unknown_oids"` // traded orders the leg does not list
}

// DayCheck is a day that must have no trades: a blocked order, or a day
// with no stage record.
type DayCheck struct {
	Book     string   `json:"book"`
	BarDate  string   `json:"bar_date"`
	FillDate string   `json:"fill_date"`
	Reason   string   `json:"reason"` // "blocked" | "no_record"
	Status   string   `json:"status"`
	Origins  []string `json:"origins"`
	Trades   int      `json:"trades"`
	BaseBTC  float64  `json:"base_btc"` // Σ trade quantity found
}

// PositionCheck is a book's recorded position against the sum of its legs.
type PositionCheck struct {
	Book        string  `json:"book"`
	LedgerBTC   float64 `json:"ledger_btc"`    // position after the last stage record
	SumDeltaBTC float64 `json:"sum_delta_btc"` // Σ leg base_delta
	OK          bool    `json:"ok"`
}

// BalanceCheck is the account's balance in one asset against what the
// books hold.
type BalanceCheck struct {
	Currency string  `json:"currency"`
	Balance  float64 `json:"balance"`
	Required float64 `json:"required"` // Σ book positions in this asset
	OK       bool    `json:"ok"`
}

// Report is one reconciliation run.
type Report struct {
	At        time.Time       `json:"at"`
	OK        bool            `json:"ok"`
	Breaks    int             `json:"breaks"` // failed checks of any kind
	Legs      []LegCheck      `json:"legs"`
	Days      []DayCheck      `json:"days"`
	Positions []PositionCheck `json:"positions"`
	Balances  []BalanceCheck  `json:"balances"`
}

// Tolerances: Bitso reports BTC to 1e-8; quote amounts are compared to a
// cent or 1 ppm of the notional, whichever is larger.
const (
	baseTol = 1.5e-8
	ppm     = 1e-6
)

func quoteTol(n float64) float64 { return math.Max(0.01, math.Abs(n)*ppm) }

// Run reconciles the stage records in recs (any order, any mode; only
// mode "stage" is checked) as of now.
func Run(src Source, recs []dailyledger.Record, now time.Time) (Report, error) {
	rep := Report{At: now.UTC(), Legs: []LegCheck{}, Days: []DayCheck{}, Positions: []PositionCheck{}, Balances: []BalanceCheck{}}
	byBook := dailyledger.ByBook(stageOnly(recs))
	books := make([]string, 0, len(byBook))
	for b := range byBook {
		books = append(books, b)
	}
	sort.Strings(books)

	required := map[string]float64{}
	for _, book := range books {
		rs := byBook[book]
		var sumDelta float64
		seen := map[string]bool{}
		for _, r := range rs {
			seen[r.Decision.BarDate] = true
			st := r.Stage
			switch {
			case st.Leg != nil:
				lc, err := checkLeg(src, book, r)
				if err != nil {
					return rep, err
				}
				sumDelta += st.Leg.BaseDelta
				rep.Legs = append(rep.Legs, lc)
			case st.Action == dailyledger.ActionBlocked:
				side := ""
				if st.Risk != nil {
					side = st.Risk.Order.Side
				}
				dc, err := checkDay(src, book, r.Decision.BarDate, r.Decision.FillDate, "blocked", side)
				if err != nil {
					return rep, err
				}
				rep.Days = append(rep.Days, dc)
			}
		}
		for _, bar := range missingDays(rs[0].Decision.BarDate, now, seen) {
			dc, err := checkDay(src, book, bar, nextDay(bar), "no_record", "")
			if err != nil {
				return rep, err
			}
			rep.Days = append(rep.Days, dc)
		}
		pos := dailyledger.LastStagePosition(rs)
		sumDelta = math.Round(sumDelta*1e8) / 1e8
		rep.Positions = append(rep.Positions, PositionCheck{Book: book, LedgerBTC: pos.BTC, SumDeltaBTC: sumDelta,
			OK: math.Abs(pos.BTC-sumDelta) <= baseTol})
		base, _ := dailyledger.BaseQuote(book)
		required[base] += pos.BTC
	}

	if len(books) > 0 {
		bal, err := src.Balances()
		if err != nil {
			return rep, fmt.Errorf("balances: %w", err)
		}
		assets := make([]string, 0, len(required))
		for a := range required {
			assets = append(assets, a)
		}
		sort.Strings(assets)
		for _, a := range assets {
			b := bal[a]
			rep.Balances = append(rep.Balances, BalanceCheck{Currency: a, Balance: b, Required: required[a], OK: b+baseTol >= required[a]})
		}
	}

	for _, l := range rep.Legs {
		if l.Status != StatusMatched {
			rep.Breaks++
		}
	}
	for _, d := range rep.Days {
		if d.Status != StatusClean {
			rep.Breaks++
		}
	}
	for _, p := range rep.Positions {
		if !p.OK {
			rep.Breaks++
		}
	}
	for _, b := range rep.Balances {
		if !b.OK {
			rep.Breaks++
		}
	}
	rep.OK = rep.Breaks == 0
	return rep, nil
}

func stageOnly(recs []dailyledger.Record) []dailyledger.Record {
	out := make([]dailyledger.Record, 0, len(recs))
	for _, r := range recs {
		if r.Mode == "stage" && r.Stage != nil {
			out = append(out, r)
		}
	}
	return out
}

func checkLeg(src Source, book string, r dailyledger.Record) (LegCheck, error) {
	l := r.Stage.Leg
	lc := LegCheck{Book: book, BarDate: r.Decision.BarDate, FillDate: r.Decision.FillDate, Side: l.Side,
		Ledger:   Amounts{Filled: l.Filled, Notional: l.Notional, Fees: nonNil(l.Fees), BaseDelta: l.BaseDelta},
		Exchange: Amounts{Fees: map[string]float64{}}, Diffs: []string{}, UnknownOids: []string{}}
	mk, tk := l.MakerOrigin, l.TakerOrigin
	if mk == "" || tk == "" { // lines written before the ids were recorded
		mk, tk = dailyexec.OriginIDs(book, r.Decision.FillDate, l.Side)
	}
	listed := map[string]bool{}
	for _, o := range l.Oids {
		listed[o] = true
	}
	unknown := map[string]bool{}
	for _, origin := range []string{mk, tk} {
		ts, err := src.TradesByOrigin(origin)
		if err != nil {
			return lc, fmt.Errorf("%s %s trades for %s: %w", book, r.Decision.FillDate, origin, err)
		}
		for _, t := range ts {
			lc.Exchange.Trades++
			lc.Exchange.Filled += t.Major
			lc.Exchange.Notional += t.Minor
			lc.Exchange.Fees[strings.ToLower(t.FeeCurrency)] += t.Fee
			if !listed[t.Oid] {
				unknown[t.Oid] = true
			}
		}
	}
	base, _ := dailyledger.BaseQuote(book)
	delta := lc.Exchange.Filled
	if l.Side == "sell" {
		delta = -delta
	}
	lc.Exchange.BaseDelta = math.Round((delta-lc.Exchange.Fees[base])*1e8) / 1e8
	for o := range unknown {
		lc.UnknownOids = append(lc.UnknownOids, o)
	}
	sort.Strings(lc.UnknownOids)

	if d := lc.Exchange.Filled - l.Filled; math.Abs(d) > baseTol {
		lc.Diffs = append(lc.Diffs, fmt.Sprintf("filled: ledger %.8f, Bitso %.8f BTC", l.Filled, lc.Exchange.Filled))
	}
	if d := lc.Exchange.Notional - l.Notional; math.Abs(d) > quoteTol(l.Notional) {
		lc.Diffs = append(lc.Diffs, fmt.Sprintf("notional: ledger %.2f, Bitso %.2f", l.Notional, lc.Exchange.Notional))
	}
	if d := lc.Exchange.BaseDelta - l.BaseDelta; math.Abs(d) > baseTol {
		lc.Diffs = append(lc.Diffs, fmt.Sprintf("base change: ledger %.8f, Bitso %.8f BTC", l.BaseDelta, lc.Exchange.BaseDelta))
	}
	for _, ccy := range feeCurrencies(lc.Ledger.Fees, lc.Exchange.Fees) {
		tol := quoteTol(lc.Ledger.Fees[ccy])
		if ccy == base {
			tol = baseTol
		}
		if math.Abs(lc.Exchange.Fees[ccy]-lc.Ledger.Fees[ccy]) > tol {
			lc.Diffs = append(lc.Diffs, fmt.Sprintf("fee %s: ledger %.8f, Bitso %.8f", ccy, lc.Ledger.Fees[ccy], lc.Exchange.Fees[ccy]))
		}
	}
	if len(lc.UnknownOids) > 0 {
		lc.Diffs = append(lc.Diffs, "orders not in the ledger: "+strings.Join(lc.UnknownOids, ", "))
	}
	lc.Status = StatusMatched
	if len(lc.Diffs) > 0 {
		lc.Status = StatusMismatch
	}
	return lc, nil
}

// checkDay looks for trades under a day's client ids; side "" checks both.
func checkDay(src Source, book, bar, fill, reason, side string) (DayCheck, error) {
	dc := DayCheck{Book: book, BarDate: bar, FillDate: fill, Reason: reason, Status: StatusClean, Origins: []string{}}
	sides := []string{"buy", "sell"}
	if side == "buy" || side == "sell" {
		sides = []string{side}
	}
	for _, s := range sides {
		mk, tk := dailyexec.OriginIDs(book, fill, s)
		for _, origin := range []string{mk, tk} {
			dc.Origins = append(dc.Origins, origin)
			ts, err := src.TradesByOrigin(origin)
			if err != nil {
				return dc, fmt.Errorf("%s %s trades for %s: %w", book, fill, origin, err)
			}
			for _, t := range ts {
				dc.Trades++
				dc.BaseBTC += t.Major
			}
		}
	}
	if dc.Trades > 0 {
		dc.Status = StatusUnrecorded
	}
	return dc, nil
}

// missingDays are the bar dates from first through the latest closed bar
// (dailyledger.ExpectedLastBar) without a stage record.
func missingDays(first string, now time.Time, seen map[string]bool) []string {
	start, err := time.Parse("2006-01-02", first)
	if err != nil {
		return nil
	}
	end, _ := time.Parse("2006-01-02", dailyledger.ExpectedLastBar(now))
	var out []string
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if s := d.Format("2006-01-02"); !seen[s] {
			out = append(out, s)
		}
	}
	return out
}

func nextDay(s string) string {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return ""
	}
	return d.AddDate(0, 0, 1).Format("2006-01-02")
}

func nonNil(m map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[strings.ToLower(k)] += v
	}
	return out
}

func feeCurrencies(a, b map[string]float64) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
