package cfdrisk

import (
	"math"
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/etoroledger"
)

// Fill quality of the demo orders (M14): what each executed leg cost
// against the quote it was checked on, against the decision close the
// paper account fills near, and against the day's bar open; how long eToro
// took to execute; and whether the portfolio lagged the fill (the day's
// reconcile report). The aggregate is the cost-model check the
// pre-registration asks for: the measured spread per leg against the
// frozen 7.75 / 7.4 bps (half of 15.5 / 14.8 round trip).
//
// Three readings of "measured spread per leg" are kept, because they
// answer different questions:
//   - spread_cost_bps: eToro's own spread cost on the order (the ledger's
//     spread_cost, 0.04-0.06 USD on 1,100 USD on 2026-10-12) over the
//     notional: what the broker says the leg cost;
//   - half the quoted spread at check time: the pure bid/ask cost;
//   - fill vs mid: the fill against the quote's mid, which also contains
//     the price drift between the quote (13:33:59Z) and the fill (13:35:09Z)
//     and can therefore be negative (favourable), as it was on 2026-10-12.
//
// The cost-model check uses the first; the other two are shown beside it.

// Fill is one executed order.
type Fill struct {
	Book       string `json:"book"`
	BarDate    string `json:"bar_date"`
	FillDate   string `json:"fill_date"`
	Kind       string `json:"kind"`
	ClientRef  string `json:"client_ref"`
	PositionID string `json:"position_id,omitempty"`
	Resumed    bool   `json:"resumed,omitempty"`

	AvgPrice float64 `json:"avg_price"`
	Units    float64 `json:"units"`
	Amount   float64 `json:"amount"`

	QuotedSpreadBps float64  `json:"quoted_spread_bps"`
	FillVsMidBps    float64  `json:"fill_vs_mid_bps"`
	FillVsCloseBps  float64  `json:"fill_vs_close_bps"`
	DayOpen         float64  `json:"day_open,omitempty"`
	FillVsOpenBps   *float64 `json:"fill_vs_open_bps,omitempty"` // nil without the day's bar
	SpreadCostUSD   float64  `json:"spread_cost_usd"`
	SpreadCostBps   float64  `json:"spread_cost_bps"`
	FeesUSD         float64  `json:"fees_usd"`
	LatencyS        float64  `json:"latency_s"` // finished - started

	PortfolioLag   bool     `json:"portfolio_lag"`
	ReconcileClean *bool    `json:"reconcile_clean,omitempty"` // nil without the day's report
	ReconcileCodes []string `json:"reconcile_codes,omitempty"` // findings on this book that day
}

// ReconcileDay is what the fill date's reconcile report said, as far as
// fill quality needs it.
type ReconcileDay struct {
	GeneratedAt   time.Time
	Clean         bool
	PositionReads int
	SettleWaitedS float64
	Findings      []ReconcileFinding
}

// ReconcileFinding is one finding of a reconcile report.
type ReconcileFinding struct {
	Severity, Book, Code string
}

// settleWindow is cmd/etoro-reconcile's window: a missing_position drift
// reported this soon after the fill is the portfolio lag seen on
// 2026-10-12, before the reconcile learned to wait.
const settleWindow = 10 * time.Minute

// FillInput is everything the fill-quality report reads.
type FillInput struct {
	Records []etoroledger.Record
	// Opens: book -> fill date -> the day's bar open (eToro's UTC-day
	// bucket open, from the candles CSV).
	Opens map[string]map[string]float64
	// Reconcile: fill date -> the day's report.
	Reconcile map[string]ReconcileDay
	// FrozenSpreadRTBps: the pre-registered round-trip spread per book.
	FrozenSpreadRTBps map[string]float64
}

// Stat summarises one measure over fills. Worst is the costliest (largest)
// value.
type Stat struct {
	N      int     `json:"n"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	Worst  float64 `json:"worst"`
}

func stat(xs []float64) Stat {
	if len(xs) == 0 {
		return Stat{}
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	var sum float64
	for _, x := range s {
		sum += x
	}
	med := s[len(s)/2]
	if len(s)%2 == 0 {
		med = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	return Stat{N: len(s), Mean: sum / float64(len(s)), Median: med, Worst: s[len(s)-1]}
}

// BookFillQuality aggregates one book's fills (or all books, Book "all").
type BookFillQuality struct {
	Book          string `json:"book"`
	N             int    `json:"n"`
	QuotedSpread  Stat   `json:"quoted_spread_bps"`
	FillVsMid     Stat   `json:"fill_vs_mid_bps"`
	FillVsClose   Stat   `json:"fill_vs_close_bps"`
	FillVsOpen    Stat   `json:"fill_vs_open_bps"`
	SpreadCost    Stat   `json:"spread_cost_bps"`
	Latency       Stat   `json:"latency_s"`
	PortfolioLags int    `json:"portfolio_lags"`
	// The cost-model check.
	FrozenSpreadPerLegBps   float64 `json:"frozen_spread_per_leg_bps"`   // half the frozen round trip (mean over books for "all")
	MeasuredSpreadPerLegBps float64 `json:"measured_spread_per_leg_bps"` // mean spread_cost_bps
	HalfQuotedSpreadBps     float64 `json:"half_quoted_spread_bps"`      // mean quoted / 2
	MeasuredVsFrozen        float64 `json:"measured_vs_frozen"`          // measured / frozen
}

// FillQuality is the report.
type FillQuality struct {
	Fills []Fill            `json:"fills"`
	Books []BookFillQuality `json:"books"`
	All   BookFillQuality   `json:"all"`
}

// MeasureFills builds the report from executed demo orders.
func MeasureFills(in FillInput) FillQuality {
	var out FillQuality
	for _, r := range in.Records {
		if r.Mode != etoroledger.ModeDemo || r.Demo == nil || r.Demo.Order == nil || r.Demo.Order.Status != "executed" {
			continue
		}
		d, o := r.Demo, r.Demo.Order
		f := Fill{Book: r.Book, BarDate: r.Decision.BarDate, FillDate: r.Decision.FillDate, Kind: o.Kind, ClientRef: o.ClientRef,
			Resumed: o.Resumed, AvgPrice: o.AvgPrice, Units: o.Units, Amount: o.Amount,
			FillVsMidBps: o.FillVsMidBps, FillVsCloseBps: o.FillVsCloseBps, SpreadCostUSD: o.SpreadCost, FeesUSD: o.Fees}
		switch o.Kind {
		case etoroledger.ActionOpen:
			f.PositionID = d.PositionAfter.PositionID
		case etoroledger.ActionClose:
			f.PositionID = d.PositionBefore.PositionID
		}
		if d.Quote != nil {
			f.QuotedSpreadBps = d.Quote.SpreadBps
		}
		if f.Amount == 0 && f.AvgPrice > 0 {
			f.Amount = f.AvgPrice * f.Units
		}
		if f.Amount > 0 {
			f.SpreadCostBps = f.SpreadCostUSD / f.Amount * 1e4
		}
		if !o.Started.IsZero() && !o.Finished.IsZero() {
			f.LatencyS = o.Finished.Sub(o.Started).Seconds()
		}
		if open := in.Opens[r.Book][r.Decision.FillDate]; open > 0 && f.AvgPrice > 0 {
			f.DayOpen = open
			v := (f.AvgPrice - open) / open * 1e4
			if o.Kind == etoroledger.ActionClose {
				v = -v
			}
			f.FillVsOpenBps = &v
		}
		if day, ok := in.Reconcile[r.Decision.FillDate]; ok {
			clean := day.Clean
			f.ReconcileClean = &clean
			for _, fd := range day.Findings {
				if fd.Book != r.Book {
					continue
				}
				f.ReconcileCodes = append(f.ReconcileCodes, fd.Code)
				switch fd.Code {
				case "portfolio_lag":
					f.PortfolioLag = true
				case "missing_position", "orphan_position":
					if !o.Finished.IsZero() && !day.GeneratedAt.IsZero() && day.GeneratedAt.Sub(o.Finished) < settleWindow {
						f.PortfolioLag = true
					}
				}
			}
		}
		out.Fills = append(out.Fills, f)
	}
	sort.SliceStable(out.Fills, func(i, j int) bool {
		if out.Fills[i].FillDate != out.Fills[j].FillDate {
			return out.Fills[i].FillDate < out.Fills[j].FillDate
		}
		return out.Fills[i].Book < out.Fills[j].Book
	})
	books := map[string][]Fill{}
	var names []string
	for _, f := range out.Fills {
		if _, ok := books[f.Book]; !ok {
			names = append(names, f.Book)
		}
		books[f.Book] = append(books[f.Book], f)
	}
	sort.Strings(names)
	frozenAll := 0.0
	for _, b := range names {
		frozen := in.FrozenSpreadRTBps[b] / 2
		frozenAll += frozen
		out.Books = append(out.Books, aggregate(b, books[b], frozen))
	}
	if len(names) > 0 {
		frozenAll /= float64(len(names))
	}
	out.All = aggregate("all", out.Fills, frozenAll)
	return out
}

func aggregate(book string, fills []Fill, frozenPerLeg float64) BookFillQuality {
	q := BookFillQuality{Book: book, N: len(fills), FrozenSpreadPerLegBps: frozenPerLeg}
	var quoted, mid, closeV, openV, cost, lat []float64
	for _, f := range fills {
		quoted = append(quoted, f.QuotedSpreadBps)
		mid = append(mid, f.FillVsMidBps)
		closeV = append(closeV, f.FillVsCloseBps)
		cost = append(cost, f.SpreadCostBps)
		lat = append(lat, f.LatencyS)
		if f.FillVsOpenBps != nil {
			openV = append(openV, *f.FillVsOpenBps)
		}
		if f.PortfolioLag {
			q.PortfolioLags++
		}
	}
	q.QuotedSpread, q.FillVsMid, q.FillVsClose, q.FillVsOpen = stat(quoted), stat(mid), stat(closeV), stat(openV)
	q.SpreadCost, q.Latency = stat(cost), stat(lat)
	q.MeasuredSpreadPerLegBps = q.SpreadCost.Mean
	q.HalfQuotedSpreadBps = q.QuotedSpread.Mean / 2
	if frozenPerLeg > 0 && len(fills) > 0 {
		q.MeasuredVsFrozen = q.MeasuredSpreadPerLegBps / frozenPerLeg
	}
	if math.IsNaN(q.MeasuredVsFrozen) {
		q.MeasuredVsFrozen = 0
	}
	return q
}
