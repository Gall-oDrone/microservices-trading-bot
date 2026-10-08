// Package varmodel estimates daily volatility and Value-at-Risk the way a
// bank or fund risk function does, from daily closes:
//
//   - RiskMetrics EWMA variance (lambda 0.94) of daily log returns, zero
//     mean, seeded with the mean square of the first SeedReturns returns.
//   - An equal-weighted long-window variance (zero mean, the last
//     LongWindow returns, or all available down to MinReturns).
//   - The vol used is max(EWMA, long): a calm month cannot shrink VaR below
//     what the last year supports, and a shock lifts it at once.
//   - Historical-simulation VaR: the k-th worst of n scenario P&Ls with
//     k = ceil(n x (1 - confidence)), so fat tails are not assumed away.
//   - Backtesting (Basel "traffic light"): over the last 250 days, count the
//     days whose loss exceeded the one-day-ahead 99 % VaR forecast, for a
//     long and for a short unit position. 0-4 exceptions green, 5-9 yellow,
//     10+ red; Kupiec's proportion-of-failures test gives a p-value.
//
// Pure functions, no I/O. Returns are computed only between consecutive
// calendar days: a missing day breaks the chain instead of producing a
// multi-day return that would inflate the vol.
package varmodel

import (
	"errors"
	"math"
	"sort"
	"time"
)

const (
	Lambda         = 0.94               // RiskMetrics daily decay
	Confidence     = 0.99               // one-sided
	Z              = 2.3263478740408408 // normal quantile at Confidence
	SeedReturns    = 30                 // EWMA seed
	LongWindow     = 365                // equal-weighted window (crypto trades every day)
	MinReturns     = 60                 // fewer: no estimate
	BacktestWindow = 250                // Basel observation window
)

// Zone is the Basel traffic-light zone of a backtest.
type Zone int

const (
	ZoneInsufficient Zone = -1 // fewer than BacktestWindow observations
	ZoneGreen        Zone = 0
	ZoneYellow       Zone = 1
	ZoneRed          Zone = 2
)

func (z Zone) String() string {
	switch z {
	case ZoneGreen:
		return "green"
	case ZoneYellow:
		return "yellow"
	case ZoneRed:
		return "red"
	}
	return "insufficient"
}

// BaselZone maps exceptions in BacktestWindow observations at 99 % to a zone.
func BaselZone(observations, exceptions int) Zone {
	switch {
	case observations < BacktestWindow:
		return ZoneInsufficient
	case exceptions <= 4:
		return ZoneGreen
	case exceptions <= 9:
		return ZoneYellow
	}
	return ZoneRed
}

// Close is one daily close, labelled by its calendar date (UTC midnight or
// any time on that date; only the date is used).
type Close struct {
	Date  time.Time
	Price float64
}

// Return is the log return from the previous calendar day's close to Date's.
type Return struct {
	Date time.Time
	R    float64
}

func day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// LogReturns returns the log returns between closes on consecutive calendar
// days. closes must be sorted by date; non-positive prices break the chain.
func LogReturns(closes []Close) []Return {
	var out []Return
	for i := 1; i < len(closes); i++ {
		a, b := closes[i-1], closes[i]
		if !(a.Price > 0) || !(b.Price > 0) {
			continue
		}
		if day(b.Date).Sub(day(a.Date)) != 24*time.Hour {
			continue
		}
		out = append(out, Return{Date: day(b.Date), R: math.Log(b.Price / a.Price)})
	}
	return out
}

// Forecast is the one-day-ahead vol after a return: for the day following it.
type Forecast struct {
	EWMA, Long, Vol float64 // daily vol ratios; Vol = max(EWMA, Long)
	Ready           bool    // enough history (MinReturns) for a forecast
}

// Forecasts returns f with f[i] the forecast made after rets[i] (using
// rets[0..i]).
func Forecasts(rets []Return, lambda float64) []Forecast {
	f := make([]Forecast, len(rets))
	var v, sumSq float64
	for i, r := range rets {
		sq := r.R * r.R
		sumSq += sq
		switch {
		case i < SeedReturns-1:
		case i == SeedReturns-1:
			v = sumSq / SeedReturns
		default:
			v = lambda*v + (1-lambda)*sq
		}
		if i >= LongWindow {
			old := rets[i-LongWindow].R
			sumSq -= old * old
		}
		n := i + 1
		if n > LongWindow {
			n = LongWindow
		}
		if i+1 < MinReturns {
			continue
		}
		long := math.Sqrt(math.Max(sumSq, 0) / float64(n))
		ewma := math.Sqrt(v)
		f[i] = Forecast{EWMA: ewma, Long: long, Vol: math.Max(ewma, long), Ready: true}
	}
	return f
}

// Backtest is the result of comparing VaR forecasts with realized returns.
type Backtest struct {
	Observations    int
	ExceptionsLong  int     // days a long unit position lost more than VaR
	ExceptionsShort int     // days a short unit position lost more than VaR
	KupiecLong      float64 // p-value of the long-side exception rate
	KupiecShort     float64
	Zone            Zone // worse of the two sides
}

// Estimate is the model's view of one book.
type Estimate struct {
	Forecast              // for the next day
	Returns     int       // returns available
	LastDate    time.Time // date of the last return
	Backtest    Backtest
	HistReturns []Return // the last LongWindow returns, for historical simulation
}

// ErrInsufficient means fewer than MinReturns returns.
var ErrInsufficient = errors.New("varmodel: not enough daily returns for an estimate")

// EstimateFromCloses computes the next-day forecast, the 250-day backtest and the
// historical-simulation window from daily closes.
func EstimateFromCloses(closes []Close, lambda float64) (Estimate, error) {
	rets := LogReturns(closes)
	if len(rets) < MinReturns {
		return Estimate{Returns: len(rets)}, ErrInsufficient
	}
	f := Forecasts(rets, lambda)
	e := Estimate{
		Forecast: f[len(f)-1],
		Returns:  len(rets),
		LastDate: rets[len(rets)-1].Date,
		Backtest: BacktestForecasts(rets, f),
	}
	from := len(rets) - LongWindow
	if from < 0 {
		from = 0
	}
	e.HistReturns = append([]Return(nil), rets[from:]...)
	return e, nil
}

// BacktestForecasts checks the last BacktestWindow returns against the
// forecast made the day before each. VaR is linear in exposure (the same
// z x vol x exposure the portfolio VaR uses), so a long unit position has an
// exception when its simple return is below -z x vol, a short one when it
// is above +z x vol.
func BacktestForecasts(rets []Return, f []Forecast) Backtest {
	var b Backtest
	from := len(rets) - BacktestWindow
	if from < 1 {
		from = 1
	}
	for i := from; i < len(rets); i++ {
		prev := f[i-1]
		if !prev.Ready {
			continue
		}
		b.Observations++
		v := Z * prev.Vol
		simple := math.Expm1(rets[i].R)
		if simple < -v {
			b.ExceptionsLong++
		}
		if simple > v {
			b.ExceptionsShort++
		}
	}
	b.KupiecLong = KupiecPOF(b.Observations, b.ExceptionsLong, 1-Confidence)
	b.KupiecShort = KupiecPOF(b.Observations, b.ExceptionsShort, 1-Confidence)
	worst := b.ExceptionsLong
	if b.ExceptionsShort > worst {
		worst = b.ExceptionsShort
	}
	b.Zone = BaselZone(b.Observations, worst)
	return b
}

// KupiecPOF is the p-value of Kupiec's proportion-of-failures likelihood
// ratio test that x exceptions in n observations are consistent with an
// exception probability p. Small values reject the model. n == 0 gives 1.
func KupiecPOF(n, x int, p float64) float64 {
	if n <= 0 || x < 0 || x > n {
		return 1
	}
	xlogy := func(x, y float64) float64 {
		if x == 0 {
			return 0
		}
		return x * math.Log(y)
	}
	fn, fx := float64(n), float64(x)
	phat := fx / fn
	lr := -2 * (xlogy(fn-fx, 1-p) + xlogy(fx, p) - xlogy(fn-fx, 1-phat) - xlogy(fx, phat))
	if lr < 0 {
		lr = 0 // rounding
	}
	// Survival function of chi-square with one degree of freedom.
	return math.Erfc(math.Sqrt(lr / 2))
}

// HistoricalVaR is the historical-simulation VaR of scenario P&Ls: the k-th
// worst loss with k = ceil(n x (1 - confidence)), floored at 0. ok is false
// when there are fewer than minScenarios.
func HistoricalVaR(pnl []float64, confidence float64, minScenarios int) (float64, bool) {
	n := len(pnl)
	if n == 0 || n < minScenarios {
		return 0, false
	}
	losses := make([]float64, n)
	for i, v := range pnl {
		losses[i] = -v
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(losses)))
	k := int(math.Ceil(float64(n)*(1-confidence) - 1e-9))
	if k < 1 {
		k = 1
	}
	return math.Max(losses[k-1], 0), true
}
