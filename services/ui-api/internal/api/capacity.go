package api

import (
	"math"
	"net/http"
	"time"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/shared/pkg/execcost"
)

// Plan §6.4.12: capacity and market impact. How large can an order get
// before execution costs exceed what the pre-registration assumed? The
// registered costs are a fee plus 10 bps of slippage per leg, so the
// slippage budget is 10 bps; this endpoint measures order sizes against it.
// Reporting only.

// capacitySizes are the order sizes studied (BTC), from today's stage leg
// up to production-like sizes.
var capacitySizes = []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5}

const (
	slippageBudgetBps = 10 // the pre-registrations' slippage per leg
	advDays           = 30
	volDays           = 90
	bookMaxAge        = 5 * time.Minute
	historyDays       = 30 // book-sample window
)

// CapacityRow is one order size.
type CapacityRow struct {
	QtyBTC   float64 `json:"qty_btc"`
	Notional float64 `json:"notional"` // quote, at mid (else last close)
	PctADV   float64 `json:"pct_adv"`  // of the 30-day mean daily volume
	// Market order walked through the live book (null without a fresh book).
	BuyWalkBps  *float64 `json:"buy_walk_bps"`
	SellWalkBps *float64 `json:"sell_walk_bps"`
	BookFills   bool     `json:"book_fills"` // the visible depth covers the size on both sides
	// Square-root law, Y = 0.5 and 1.0.
	SqrtLoBps float64 `json:"sqrt_lo_bps"`
	SqrtHiBps float64 `json:"sqrt_hi_bps"`
	// TakerTotalBps: taker fee + the walk's worse side when the book covers
	// the size, else + the square-root upper estimate.
	TakerTotalBps float64 `json:"taker_total_bps"`
	WithinBudget  bool    `json:"within_budget"` // slippage part <= 10 bps
}

// CapacityResponse is GET /api/ui/forward-tests/{book}/capacity.
type CapacityResponse struct {
	Ledger      string `json:"ledger"`
	Book        string `json:"book"`
	Quote       string `json:"quote"`
	GeneratedAt string `json:"generated_at"`
	// Live book (Bitso production, public feed): "live", "stale" or "none".
	BookStatus  string  `json:"book_status"`
	DepthAt     string  `json:"depth_at"`
	Mid         float64 `json:"mid"`
	SpreadBps   float64 `json:"spread_bps"`
	BidDepthBTC float64 `json:"bid_depth_btc"` // visible (top 20 levels)
	AskDepthBTC float64 `json:"ask_depth_btc"`
	// From the daily candles.
	LastClose  float64 `json:"last_close"`
	ADVBTC     float64 `json:"adv_btc"`        // 30-day mean volume
	ADVMedBTC  float64 `json:"adv_median_btc"` // 30-day median
	DailyVol   float64 `json:"daily_vol"`      // 90-day std of daily log returns
	CandleDate string  `json:"candle_date"`
	// Costs (pre-registered).
	MakerFeeBps       float64 `json:"maker_fee_bps"`
	TakerFeeBps       float64 `json:"taker_fee_bps"`
	SlippageBudgetBps float64 `json:"slippage_budget_bps"`
	// Capacity at the slippage budget.
	WalkCapacityBTC *float64 `json:"walk_capacity_btc"` // largest size whose walk costs <= budget, both sides
	SqrtCapacityLo  float64  `json:"sqrt_capacity_lo_btc"`
	SqrtCapacityHi  float64  `json:"sqrt_capacity_hi_btc"` // Y = 0.5 (the larger) .. Y = 1.0
	// Today's sizing.
	StageSizeBTC float64       `json:"stage_size_btc"`
	PolicyMaxBTC float64       `json:"policy_max_order_btc"`
	Rows         []CapacityRow `json:"rows"`
	Note         string        `json:"note"`
	// History is the distribution of the hourly book samples over the last
	// HistoryDays (§6.4.13); null without samples.
	History     *execcost.SampleSummary `json:"history"`
	HistoryDays int                     `json:"history_days"`
}

func (s *Server) capacity(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pick(w, r)
	if !ok {
		return
	}
	by, ok := s.load(w, l)
	if !ok {
		return
	}
	b, ok := s.bookParam(w, r, by)
	if !ok {
		return
	}
	now := s.Now()
	_, quote := dailyledger.BaseQuote(b)
	pc := dailyledger.PreregCosts[b]
	resp := CapacityResponse{Ledger: l.Name, Book: b, Quote: quote, GeneratedAt: now.UTC().Format(time.RFC3339), BookStatus: "none",
		SlippageBudgetBps: slippageBudgetBps, StageSizeBTC: s.StageSize, PolicyMaxBTC: s.Policy.For(b).MaxOrderBTC, Rows: []CapacityRow{}}
	if pc.PrimaryLegBps > 0 {
		resp.MakerFeeBps, resp.TakerFeeBps = pc.PrimaryLegBps-slippageBudgetBps, pc.SecondaryLegBps-slippageBudgetBps
	}

	rows, _, _ := l.Store.Candles(b)
	if len(rows) > 0 {
		closes, vols := make([]float64, len(rows)), make([]float64, len(rows))
		for i, c := range rows {
			closes[i], vols[i] = c.Close, c.Volume
		}
		resp.ADVBTC, resp.ADVMedBTC, _ = execcost.DailyStats(closes, vols, advDays)
		_, _, resp.DailyVol = execcost.DailyStats(closes, vols, volDays)
		resp.LastClose, resp.CandleDate = rows[len(rows)-1].Close, rows[len(rows)-1].Date
	}

	var bids, asks []execcost.Level
	if s.Live != nil && s.Live.Has(b) {
		snap := s.Live.SnapshotWith([]string{b}, true)
		if len(snap.Markets) == 1 && snap.Markets[0].DepthAt != "" && snap.Markets[0].Mid > 0 {
			m := snap.Markets[0]
			resp.DepthAt, resp.Mid, resp.SpreadBps = m.DepthAt, m.Mid, m.SpreadBps
			for _, x := range m.Bids {
				bids = append(bids, execcost.Level{Price: x.Price, Amount: x.Amount})
			}
			for _, x := range m.Asks {
				asks = append(asks, execcost.Level{Price: x.Price, Amount: x.Amount})
			}
			resp.BidDepthBTC, resp.AskDepthBTC = execcost.Depth(bids), execcost.Depth(asks)
			resp.BookStatus = "live"
			if at, err := time.Parse(time.RFC3339Nano, m.DepthAt); err == nil && now.Sub(at) > bookMaxAge {
				resp.BookStatus = "stale"
			}
		}
	}
	useBook := resp.BookStatus == "live"
	price := resp.Mid
	if !useBook || price <= 0 {
		price = resp.LastClose
	}
	if useBook {
		c := execcost.MaxQtyWithin(bids, asks, resp.Mid, slippageBudgetBps)
		resp.WalkCapacityBTC = &c
	}
	resp.SqrtCapacityLo = execcost.SqrtCapacity(resp.ADVBTC, resp.DailyVol, 1.0, slippageBudgetBps)
	resp.SqrtCapacityHi = execcost.SqrtCapacity(resp.ADVBTC, resp.DailyVol, 0.5, slippageBudgetBps)

	for _, q := range capacitySizes {
		row := CapacityRow{QtyBTC: q, Notional: q * price,
			SqrtLoBps: execcost.SqrtImpactBps(q, resp.ADVBTC, resp.DailyVol, 0.5),
			SqrtHiBps: execcost.SqrtImpactBps(q, resp.ADVBTC, resp.DailyVol, 1.0)}
		if resp.ADVBTC > 0 {
			row.PctADV = q / resp.ADVBTC
		}
		slip := row.SqrtHiBps
		if useBook {
			bw, sw := execcost.Walk(asks, q, resp.Mid, true), execcost.Walk(bids, q, resp.Mid, false)
			bb, ss := bw.CostBps, sw.CostBps
			row.BuyWalkBps, row.SellWalkBps = &bb, &ss
			row.BookFills = bw.Complete && sw.Complete
			if row.BookFills {
				slip = math.Max(bb, ss)
			}
		}
		row.TakerTotalBps = resp.TakerFeeBps + slip
		row.WithinBudget = slip <= slippageBudgetBps
		resp.Rows = append(resp.Rows, row)
	}
	resp.HistoryDays = historyDays
	if s.BookSamplesDir != "" {
		samples, bad, err := execcost.ReadSamples(execcost.SamplePath(s.BookSamplesDir, b))
		if err != nil {
			s.Log.Printf("capacity: book samples for %s: %v", b, err)
		}
		if sum := execcost.SummarizeSamples(samples, now.AddDate(0, 0, -historyDays)); sum.Samples > 0 {
			sum.Bad = bad
			resp.History = &sum
		}
	}
	switch resp.BookStatus {
	case "live":
		resp.Note = "Book: Bitso production public feed (top 20 levels per side), mid-priced. Stage fills are thinner."
	case "stale":
		resp.Note = "The live book is older than 5 minutes; walk costs are not used."
	default:
		resp.Note = "No live book (ui-api -live=false or the book is not streamed); square-root estimates only."
	}
	writeJSON(w, http.StatusOK, resp)
}
