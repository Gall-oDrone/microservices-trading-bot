package cfdrisk

import (
	"math"
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/varmodel"
)

// Value-at-risk and expected shortfall per book and for the portfolio.
//
// Per book this is the Bitso monitor's M1-M3 on the trading-day chain:
// parametric VaR 99 % one day = z x vol x |exposure| with vol =
// max(EWMA, 252-day); historical VaR 99 % and ES 97.5 % on the last 252
// trading-day returns revalued at today's exposure; normal ES; the Basel
// traffic light and Kupiec test over 250 trading days.
//
// For the portfolio the Bitso shortcut (one base asset, correlation 1) does
// not hold: NSDQ100 and SPX500 are different indices. The parametric
// portfolio VaR is z x sqrt(w' Sigma w) with Sigma = D R D, where R is the
// EWMA correlation matrix (same lambda, recursion run on the days both
// books have a return) and D the per-book vols. Using the per-book vol
// (with its long-window floor) on the diagonal rather than the raw EWMA
// variance keeps the portfolio consistent with the per-book figures: with
// R = 1 the portfolio VaR is exactly the sum of the per-book VaRs. The
// historical simulation needs no correlation at all: every aligned day's
// P&L is the sum of the books' P&Ls on that day, so the dependence is the
// observed one, tails included.

// Book is one instrument's closes and current position.
type Book struct {
	Name   string
	Closes []varmodel.Close // sorted by date; non-trading-day bars are dropped
	Units  float64          // signed position; long > 0
}

// BookVaR is one book's risk figures. Money amounts are in the book's quote
// currency (USD) and positive for a loss.
type BookVaR struct {
	Book      string
	Units     float64
	LastClose float64
	LastDate  time.Time
	Exposure  float64 // Units x LastClose, signed
	Returns   int     // trading-day returns available

	EWMA, Long, Vol float64 // daily vol ratios; Vol = max(EWMA, Long)
	VaR             float64 // parametric 99 % one day
	VaRHistorical   float64
	ESHistorical    float64
	ESNormal        float64
	HistOK          bool // at least MinScenarios scenarios
	Scenarios       int
	Backtest        varmodel.Backtest
	Err             error // varmodel.ErrInsufficient: nothing above is set
}

// Portfolio is the books together.
type Portfolio struct {
	Books []BookVaR
	Net   float64 // sum of signed exposures
	Gross float64 // sum of absolute exposures

	// Parametric, with the EWMA correlation.
	VaR              float64
	VaRUndiversified float64 // sum of the per-book VaRs: the correlation-1 figure
	ESNormal         float64
	Correlation      [][]float64 // EWMA correlation on aligned trading days, Books order
	LongCorrelation  [][]float64 // equal-weighted over the aligned window, as a check
	AlignedReturns   int         // aligned returns the correlations rest on
	CorrelationOK    bool        // false: fewer than MinReturns aligned returns, R = 1 assumed

	// Historical simulation on aligned trading days.
	VaRHistorical float64
	ESHistorical  float64
	HistOK        bool
	Scenarios     int
	WorstDate     time.Time // the aligned day with the largest loss
	WorstLoss     float64
}

// VaR computes the per-book and portfolio figures with DefaultParams. A
// book with too little history has Err set and is left out of the
// portfolio figures, which are then marked incomplete through HistOK /
// CorrelationOK only when it had a position.
func VaR(books []Book) Portfolio {
	return VaRWith(books, DefaultParams())
}

// VaRWith is VaR with explicit model parameters.
func VaRWith(books []Book, p varmodel.Params) Portfolio {
	var out Portfolio
	z := p.Z()
	rets := make([][]varmodel.Return, len(books))
	for i, b := range books {
		cs := TradingDayCloses(b.Closes)
		bv := BookVaR{Book: b.Name, Units: b.Units}
		if len(cs) > 0 {
			bv.LastClose, bv.LastDate = cs[len(cs)-1].Price, cs[len(cs)-1].Date
			bv.Exposure = b.Units * bv.LastClose
		}
		e, err := varmodel.EstimateWith(cs, NextTradingDay, p)
		bv.Returns = e.Returns
		if err != nil {
			bv.Err = err
			out.Books = append(out.Books, bv)
			continue
		}
		rets[i] = varmodel.LogReturnsOn(cs, NextTradingDay)
		bv.EWMA, bv.Long, bv.Vol = e.EWMA, e.Long, e.Vol
		bv.Backtest = e.Backtest
		bv.VaR = z * bv.Vol * math.Abs(bv.Exposure)
		bv.ESNormal = varmodel.ZES * bv.Vol * math.Abs(bv.Exposure)
		pnl := make([]float64, len(e.HistReturns))
		for k, r := range e.HistReturns {
			pnl[k] = bv.Exposure * math.Expm1(r.R)
		}
		bv.Scenarios = len(pnl)
		bv.VaRHistorical, bv.HistOK = varmodel.HistoricalVaR(pnl, p.Confidence, MinScenarios)
		bv.ESHistorical, _ = varmodel.ExpectedShortfall(pnl, varmodel.ESConfidence, MinScenarios)
		out.Books = append(out.Books, bv)
	}
	for _, bv := range out.Books {
		out.Net += bv.Exposure
		out.Gross += math.Abs(bv.Exposure)
		out.VaRUndiversified += bv.VaR
	}

	// Books that count: an estimate and a position.
	var idx []int
	for i, bv := range out.Books {
		if bv.Err == nil && bv.Exposure != 0 {
			idx = append(idx, i)
		}
	}
	n := len(out.Books)
	out.Correlation = identity(n)
	out.LongCorrelation = identity(n)
	out.CorrelationOK = true
	if len(idx) == 0 {
		return out
	}

	// Aligned returns: the dates every counting book has a return on.
	aligned := alignReturns(rets, idx)
	out.AlignedReturns = len(aligned)
	if len(idx) > 1 {
		ewma, long, ok := correlations(aligned, len(idx), p)
		out.CorrelationOK = ok
		for a, i := range idx {
			for b, j := range idx {
				out.Correlation[i][j] = ewma[a][b]
				out.LongCorrelation[i][j] = long[a][b]
			}
		}
	}
	var v float64
	for _, i := range idx {
		for _, j := range idx {
			bi, bj := out.Books[i], out.Books[j]
			v += bi.Exposure * bj.Exposure * out.Correlation[i][j] * bi.Vol * bj.Vol
		}
	}
	sd := math.Sqrt(math.Max(v, 0))
	out.VaR = z * sd
	out.ESNormal = varmodel.ZES * sd

	// Historical simulation: today's exposures through the last LongWindow
	// aligned days.
	from := len(aligned) - p.LongWindow
	if from < 0 {
		from = 0
	}
	window := aligned[from:]
	pnl := make([]float64, len(window))
	for k, day := range window {
		for a, i := range idx {
			pnl[k] += out.Books[i].Exposure * math.Expm1(day.r[a])
		}
		if -pnl[k] > out.WorstLoss {
			out.WorstLoss, out.WorstDate = -pnl[k], day.date
		}
	}
	out.Scenarios = len(pnl)
	out.VaRHistorical, out.HistOK = varmodel.HistoricalVaR(pnl, p.Confidence, MinScenarios)
	out.ESHistorical, _ = varmodel.ExpectedShortfall(pnl, varmodel.ESConfidence, MinScenarios)
	return out
}

// alignedDay is one trading day's returns of the counting books, in idx order.
type alignedDay struct {
	date time.Time
	r    []float64
}

// alignReturns intersects the books' return dates, oldest first.
func alignReturns(rets [][]varmodel.Return, idx []int) []alignedDay {
	count := map[time.Time]int{}
	vals := map[time.Time][]float64{}
	for a, i := range idx {
		for _, r := range rets[i] {
			if _, ok := vals[r.Date]; !ok {
				vals[r.Date] = make([]float64, len(idx))
			}
			vals[r.Date][a] = r.R
			count[r.Date]++
		}
	}
	var out []alignedDay
	for d, n := range count {
		if n == len(idx) {
			out = append(out, alignedDay{date: d, r: vals[d]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].date.Before(out[j].date) })
	return out
}

// correlations runs the EWMA covariance recursion (zero mean, seeded with
// the mean cross product of the first SeedReturns aligned days, as the
// variance is) and reads the correlation after the last day; and the
// equal-weighted correlation over the last LongWindow aligned days. ok is
// false, and both are the identity, with fewer than MinReturns aligned
// returns.
func correlations(aligned []alignedDay, m int, p varmodel.Params) (ewma, long [][]float64, ok bool) {
	ewma, long = identity(m), identity(m)
	if len(aligned) < p.MinReturns {
		return ewma, long, false
	}
	cov := make([][]float64, m)
	sum := make([][]float64, m)
	for i := range cov {
		cov[i], sum[i] = make([]float64, m), make([]float64, m)
	}
	for k, d := range aligned {
		for i := 0; i < m; i++ {
			for j := 0; j < m; j++ {
				x := d.r[i] * d.r[j]
				switch {
				case k < p.SeedReturns-1:
					sum[i][j] += x
				case k == p.SeedReturns-1:
					sum[i][j] += x
					cov[i][j] = sum[i][j] / float64(p.SeedReturns)
				default:
					cov[i][j] = p.Lambda*cov[i][j] + (1-p.Lambda)*x
				}
			}
		}
	}
	from := len(aligned) - p.LongWindow
	if from < 0 {
		from = 0
	}
	lsum := make([][]float64, m)
	for i := range lsum {
		lsum[i] = make([]float64, m)
	}
	for _, d := range aligned[from:] {
		for i := 0; i < m; i++ {
			for j := 0; j < m; j++ {
				lsum[i][j] += d.r[i] * d.r[j]
			}
		}
	}
	for i := 0; i < m; i++ {
		for j := 0; j < m; j++ {
			if i == j {
				continue
			}
			if den := math.Sqrt(cov[i][i] * cov[j][j]); den > 0 {
				ewma[i][j] = cov[i][j] / den
			}
			if den := math.Sqrt(lsum[i][i] * lsum[j][j]); den > 0 {
				long[i][j] = lsum[i][j] / den
			}
		}
	}
	return ewma, long, true
}

func identity(n int) [][]float64 {
	m := make([][]float64, n)
	for i := range m {
		m[i] = make([]float64, n)
		m[i][i] = 1
	}
	return m
}

// PairCorrelation is the EWMA correlation between two named books, or 1
// when either is unknown.
func (p Portfolio) PairCorrelation(a, b string) float64 {
	ia, ib := -1, -1
	for i, bv := range p.Books {
		switch bv.Book {
		case a:
			ia = i
		case b:
			ib = i
		}
	}
	if ia < 0 || ib < 0 || len(p.Correlation) <= ia || len(p.Correlation[ia]) <= ib {
		return 1
	}
	return p.Correlation[ia][ib]
}
