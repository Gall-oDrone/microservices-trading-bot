package api

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/montecarlo"
	"bitso-trading-platform/ui-api/internal/store"
)

// Plan §6.4.11: profit and loss, performance statistics, the rule's trade
// distribution and a Monte Carlo of the rule against buy-and-hold. Reporting
// only; none of it feeds the executor or the pre-registered evaluation.

// historyFrom is where each book's historical statistics start: the
// pre-registrations' backtest base-rate windows (calendar years 2018-2026
// for btc_mxn, 2021-2026 for btc_usd). Other books use their whole file.
var historyFrom = map[string]time.Time{
	"btc_mxn": time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC),
	"btc_usd": time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
}

// smaDays is frozen by both pre-registrations (dailyrule).
const smaDays = 50

// StagePnL is the stage position's profit and loss in the quote currency,
// average-cost method. Total = Realized + Unrealized; MarketPnL is what the
// same trades would have made at the fill days' opens with no fees, so
// Total = MarketPnL - Fees - Slippage.
type StagePnL struct {
	Since       string  `json:"since"` // first stage fill
	Legs        int     `json:"legs"`
	PositionBTC float64 `json:"position_btc"`
	AvgCost     float64 `json:"avg_cost"`   // per BTC held, fees included
	CostBasis   float64 `json:"cost_basis"` // of the BTC held
	Invested    float64 `json:"invested"`   // Σ buy notional
	Mark        float64 `json:"mark"`
	MarkDate    string  `json:"mark_date"`
	Realized    float64 `json:"realized"`
	Unrealized  float64 `json:"unrealized"`
	Total       float64 `json:"total"`
	Fees        float64 `json:"fees"`
	Slippage    float64 `json:"slippage"` // vs the fill days' opens; + is a cost
	MarketPnL   float64 `json:"market_pnl"`
	// ReturnOnInvested is Total / Invested; PaperReturn is the paper
	// account's return over the same days (from the close before the first
	// stage fill). Their gap, in bps, is the implementation shortfall.
	ReturnOnInvested float64 `json:"return_on_invested"`
	PaperReturn      float64 `json:"paper_return"`
	ShortfallBps     float64 `json:"shortfall_bps"`
	// Break-even sell prices for the BTC held: the pre-registered primary
	// and pessimistic exit costs.
	BreakEvenPrimary     float64 `json:"break_even_primary"`
	BreakEvenPessimistic float64 `json:"break_even_pessimistic"`
}

// OpenTrade is the rule's trade in progress (backtest view: entry at the
// open after the flip, from the full candle history).
type OpenTrade struct {
	Entry        string  `json:"entry"`
	EntryPrice   float64 `json:"entry_price"`
	Days         int     `json:"days"`
	Return       float64 `json:"return"` // gross, to the last close
	BestClose    float64 `json:"best_close"`
	WorstClose   float64 `json:"worst_close"`
	MaxFavorable float64 `json:"max_favorable"` // best close / entry - 1
	MaxAdverse   float64 `json:"max_adverse"`   // worst close / entry - 1
	ExitBelow    float64 `json:"exit_below"`    // the latest SMA50: a close under it exits
	ExitDistance float64 `json:"exit_distance"` // exit_below / last close - 1
}

// PaperStats are the forward test's paper account statistics (daily, 365-day
// year, no risk-free rate). Below 90 days they are shown but not meaningful.
type PaperStats struct {
	Days       int     `json:"days"`
	Meaningful bool    `json:"meaningful"`
	Return     float64 `json:"return"`
	HoldReturn float64 `json:"hold_return"`
	AnnVol     float64 `json:"ann_vol"`
	Sharpe     float64 `json:"sharpe"`
	Sortino    float64 `json:"sortino"`
	MaxDD      float64 `json:"max_dd"`
	HoldMaxDD  float64 `json:"hold_max_dd"`
	Calmar     float64 `json:"calmar"`
	HoldSharpe float64 `json:"hold_sharpe"`
}

// HistoryStats are the rule's backtest statistics on the book's candles at
// the primary cost.
type HistoryStats struct {
	From     string              `json:"from"`
	LegBps   float64             `json:"leg_bps"`
	Trips    montecarlo.Trips    `json:"trips"`
	Calendar montecarlo.Calendar `json:"calendar"`
}

// PerformanceResponse is GET /api/ui/forward-tests/{book}/performance.
type PerformanceResponse struct {
	Ledger      string        `json:"ledger"`
	Book        string        `json:"book"`
	Quote       string        `json:"quote"`
	GeneratedAt string        `json:"generated_at"`
	CandlesFile string        `json:"candles_file"`
	PnL         *StagePnL     `json:"pnl"` // null without stage fills
	OpenTrade   *OpenTrade    `json:"open_trade"`
	Paper       PaperStats    `json:"paper"`
	History     *HistoryStats `json:"history"` // null without candles
}

func toBars(rows []store.Candle) []dailyrule.Bar {
	out := make([]dailyrule.Bar, 0, len(rows))
	for _, c := range rows {
		d, err := time.Parse("2006-01-02", c.Date)
		if err != nil {
			continue
		}
		out = append(out, dailyrule.Bar{Date: d, Open: c.Open, High: c.High, Low: c.Low, Close: c.Close})
	}
	return out
}

func primaryCosts(book string) (dailyrule.Costs, float64) {
	bps := dailyledger.PreregCosts[book].PrimaryLegBps
	return dailyrule.Costs{Buy: bps / 1e4, Sell: bps / 1e4}, bps
}

// buildStagePnL walks the fills in order (average cost).
func buildStagePnL(book string, recs []dailyledger.Record, fills []Fill, mark float64, markDate string) *StagePnL {
	if len(fills) == 0 {
		return nil
	}
	p := &StagePnL{Since: fills[0].FillDate, Legs: len(fills), Mark: mark, MarkDate: markDate}
	var pos, cost float64
	for _, f := range fills {
		p.Fees += f.FeeQuote
		if f.SlippageBps != nil {
			p.Slippage += *f.SlippageBps / 1e4 * f.Notional
		}
		if f.Side == "buy" {
			pos += f.NetBTC
			cost += f.Notional
			p.Invested += f.Notional
			continue
		}
		q := math.Abs(f.NetBTC)
		if pos <= 0 || q <= 0 {
			continue
		}
		avg := cost / pos
		p.Realized += f.Notional - f.FeeQuote - avg*q
		cost -= avg * math.Min(q, pos)
		pos = math.Max(pos-q, 0)
	}
	if pos < btcDust {
		pos, cost = 0, 0
	}
	p.PositionBTC, p.CostBasis = pos, cost
	if pos > 0 {
		p.AvgCost = cost / pos
		p.Unrealized = pos*mark - cost
		pc := dailyledger.PreregCosts[book]
		if pc.PrimaryLegBps > 0 {
			p.BreakEvenPrimary = cost / pos / (1 - pc.PrimaryLegBps/1e4)
			p.BreakEvenPessimistic = cost / pos / (1 - pc.SecondaryLegBps/1e4)
		}
	}
	p.Total = p.Realized + p.Unrealized
	p.MarketPnL = p.Total + p.Fees + p.Slippage
	if p.Invested > 0 {
		p.ReturnOnInvested = p.Total / p.Invested
	}
	// Paper equity at the close before the first fill, and at the last close.
	base, last := 0.0, 0.0
	for _, r := range recs {
		if r.Decision.FillDate <= p.Since {
			base = r.Paper.Equity
		}
		last = r.Paper.Equity
	}
	if base > 0 && last > 0 {
		p.PaperReturn = last/base - 1
		p.ShortfallBps = (p.ReturnOnInvested - p.PaperReturn) * 1e4
	}
	return p
}

// buildPaperStats computes the paper account's daily statistics.
func buildPaperStats(recs []dailyledger.Record) PaperStats {
	var eq, hold []float64
	for _, r := range recs {
		if r.Paper.Equity > 0 && r.Paper.HoldEquity > 0 {
			eq = append(eq, r.Paper.Equity)
			hold = append(hold, r.Paper.HoldEquity)
		}
	}
	s := PaperStats{}
	if len(eq) == 0 {
		return s
	}
	s.Days = len(eq)
	s.Meaningful = s.Days >= 90
	// Equity starts at 1.0 before the first forward day.
	s.Return, s.HoldReturn = eq[len(eq)-1]-1, hold[len(hold)-1]-1
	rets := func(xs []float64) []float64 {
		out := make([]float64, len(xs))
		prev := 1.0
		for i, x := range xs {
			out[i] = x/prev - 1
			prev = x
		}
		return out
	}
	r, hr := rets(eq), rets(hold)
	m, sd, dsd := moments(r)
	s.AnnVol = sd * math.Sqrt(365)
	if sd > 0 {
		s.Sharpe = m / sd * math.Sqrt(365)
	}
	if dsd > 0 {
		s.Sortino = m / dsd * math.Sqrt(365)
	}
	if hm, hsd, _ := moments(hr); hsd > 0 {
		s.HoldSharpe = hm / hsd * math.Sqrt(365)
	}
	s.MaxDD, s.HoldMaxDD = maxDD(eq), maxDD(hold)
	if s.MaxDD > 0 {
		ann := math.Pow(1+s.Return, 365/float64(s.Days)) - 1
		s.Calmar = ann / s.MaxDD
	}
	return s
}

// moments is the mean, standard deviation and downside deviation (below 0).
func moments(xs []float64) (mean, sd, downside float64) {
	if len(xs) < 2 {
		return 0, 0, 0
	}
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	var v, d float64
	for _, x := range xs {
		v += (x - mean) * (x - mean)
		if x < 0 {
			d += x * x
		}
	}
	return mean, math.Sqrt(v / float64(len(xs)-1)), math.Sqrt(d / float64(len(xs)))
}

func maxDD(eq []float64) float64 {
	peak, dd := 1.0, 0.0
	for _, x := range eq {
		peak = math.Max(peak, x)
		dd = math.Max(dd, 1-x/peak)
	}
	return dd
}

// buildOpenTrade marks the rule's open trade from the history's trip stats.
func buildOpenTrade(bars []dailyrule.Bar, t *montecarlo.Trip, sma float64) *OpenTrade {
	if t == nil || len(bars) == 0 {
		return nil
	}
	i := sort.Search(len(bars), func(i int) bool { return bars[i].Date.Format("2006-01-02") >= t.Entry })
	if i >= len(bars) {
		return nil
	}
	o := &OpenTrade{Entry: t.Entry, EntryPrice: bars[i].Open, Days: t.Days, BestClose: bars[i].Close, WorstClose: bars[i].Close}
	for _, b := range bars[i:] {
		o.BestClose = math.Max(o.BestClose, b.Close)
		o.WorstClose = math.Min(o.WorstClose, b.Close)
	}
	last := bars[len(bars)-1].Close
	o.Return = last/o.EntryPrice - 1
	o.MaxFavorable, o.MaxAdverse = o.BestClose/o.EntryPrice-1, o.WorstClose/o.EntryPrice-1
	if sma > 0 {
		o.ExitBelow, o.ExitDistance = sma, sma/last-1
	}
	return o
}

func (s *Server) performance(w http.ResponseWriter, r *http.Request) {
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
	recs := by[b]
	_, quote := dailyledger.BaseQuote(b)
	resp := PerformanceResponse{Ledger: l.Name, Book: b, Quote: quote, GeneratedAt: s.Now().UTC().Format(time.RFC3339), Paper: buildPaperStats(recs)}
	rows, path, _ := l.Store.Candles(b)
	bars := toBars(rows)
	mark, markDate := 0.0, ""
	if len(bars) > 0 {
		mark, markDate = bars[len(bars)-1].Close, bars[len(bars)-1].Date.Format("2006-01-02")
		resp.CandlesFile = filepath.Base(path)
	} else if len(recs) > 0 {
		mark, markDate = recs[len(recs)-1].Decision.Close, recs[len(recs)-1].Decision.BarDate
	}
	resp.PnL = buildStagePnL(b, recs, buildFills(b, recs, rows), mark, markDate)
	if len(bars) > smaDays {
		from, ok := historyFrom[b]
		if !ok {
			from = bars[0].Date
		}
		c, bps := primaryCosts(b)
		trips := montecarlo.TripStats(bars, smaDays, from, c)
		resp.History = &HistoryStats{From: from.Format("2006-01-02"), LegBps: bps, Trips: trips,
			Calendar: montecarlo.CalendarYears(bars, smaDays, from.Year(), c)}
		sma := 0.0
		if pts := dailyrule.Evaluate(bars, smaDays); pts[len(pts)-1].Warm {
			sma = pts[len(pts)-1].SMA
		}
		resp.OpenTrade = buildOpenTrade(bars, trips.Open, sma)
	}
	writeJSON(w, http.StatusOK, resp)
}

// Monte Carlo cost scenarios.
const (
	CostPrimary     = "primary"
	CostPessimistic = "pessimistic"
	CostRealized    = "realized" // the stage legs' notional-weighted cost (primary until there are legs)
)

// MonteCarloResponse is GET /api/ui/forward-tests/{book}/montecarlo.
type MonteCarloResponse struct {
	Ledger      string              `json:"ledger"`
	Book        string              `json:"book"`
	Cost        string              `json:"cost"`
	LegBps      float64             `json:"leg_bps"`
	CandlesFile string              `json:"candles_file"`
	Summary     montecarlo.Summary  `json:"summary"`
	Calibration montecarlo.Calendar `json:"calibration"` // the same costs, per calendar year
	ElapsedMs   int64               `json:"elapsed_ms"`
	Cached      bool                `json:"cached"`
}

type mcCache struct {
	mu sync.Mutex
	m  map[string]MonteCarloResponse
}

const mcCacheMax = 32

func intParam(r *http.Request, name string, def, lo, hi int) (int, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s must be %d..%d", name, lo, hi)
	}
	return n, nil
}

func (s *Server) monteCarlo(w http.ResponseWriter, r *http.Request) {
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
	paths, err := intParam(r, "paths", montecarlo.DefaultPaths, 100, montecarlo.MaxPaths)
	if err == nil {
		var horizon, block int
		if horizon, err = intParam(r, "horizon", montecarlo.DefaultHorizon, 30, 1095); err == nil {
			if block, err = intParam(r, "block", montecarlo.DefaultMeanBlock, 1, 120); err == nil {
				s.runMonteCarlo(w, r, l, b, by[b], paths, horizon, block)
				return
			}
		}
	}
	writeErr(w, http.StatusBadRequest, err.Error())
}

func (s *Server) runMonteCarlo(w http.ResponseWriter, r *http.Request, l Ledger, b string, recs []dailyledger.Record, paths, horizon, block int) {
	cost := strings.ToLower(r.URL.Query().Get("cost"))
	if cost == "" {
		cost = CostPrimary
	}
	pc, ok := dailyledger.PreregCosts[b]
	if !ok {
		writeErr(w, http.StatusNotFound, "no pre-registered costs for "+b)
		return
	}
	rows, path, err := l.Store.Candles(b)
	if errors.Is(err, os.ErrNotExist) {
		writeErr(w, http.StatusNotFound, "no candle file for "+b)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var bps float64
	switch cost {
	case CostPrimary:
		bps = pc.PrimaryLegBps
	case CostPessimistic:
		bps = pc.SecondaryLegBps
	case CostRealized:
		bps = pc.PrimaryLegBps
		legs := []dailyledger.LegCost{}
		for _, f := range buildFills(b, recs, rows) {
			lc := dailyledger.LegCost{Notional: f.Notional, FeeQuote: f.FeeQuote}
			if f.SlippageBps != nil {
				lc.SlippageBps, lc.SlippageKnown = *f.SlippageBps, true
			}
			legs = append(legs, lc)
		}
		if bg := dailyledger.BudgetFor(b, legs); bg.Notional > 0 {
			bps = math.Round(bg.WeightedBps*10) / 10
		}
	default:
		writeErr(w, http.StatusBadRequest, "cost must be primary, pessimistic or realized")
		return
	}
	bars := toBars(rows)
	key := fmt.Sprintf("%s|%s|%s|%d|%s|%d|%d|%d|%g", l.Name, b, path, len(bars), lastDate(bars), paths, horizon, block, bps)
	s.mc.mu.Lock()
	if v, ok := s.mc.m[key]; ok {
		s.mc.mu.Unlock()
		v.Cached = true
		writeJSON(w, http.StatusOK, v)
		return
	}
	s.mc.mu.Unlock()

	from, ok := historyFrom[b]
	if !ok && len(bars) > 0 {
		from = bars[0].Date
	}
	start := sort.Search(len(bars), func(i int) bool { return !bars[i].Date.Before(from) })
	c := dailyrule.Costs{Buy: bps / 1e4, Sell: bps / 1e4}
	t0 := time.Now()
	sum, err := montecarlo.Run(bars, start, montecarlo.Config{Paths: paths, Horizon: horizon, MeanBlock: block,
		Seed: montecarlo.DefaultSeed, SMA: smaDays, Costs: c})
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	resp := MonteCarloResponse{Ledger: l.Name, Book: b, Cost: cost, LegBps: bps, CandlesFile: filepath.Base(path),
		Summary: sum, Calibration: montecarlo.CalendarYears(bars, smaDays, from.Year(), c), ElapsedMs: time.Since(t0).Milliseconds()}
	s.mc.mu.Lock()
	if s.mc.m == nil || len(s.mc.m) >= mcCacheMax {
		s.mc.m = map[string]MonteCarloResponse{}
	}
	s.mc.m[key] = resp
	s.mc.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

func lastDate(bars []dailyrule.Bar) string {
	if len(bars) == 0 {
		return ""
	}
	return bars[len(bars)-1].Date.Format("2006-01-02")
}
