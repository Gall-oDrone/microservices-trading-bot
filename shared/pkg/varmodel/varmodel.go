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
//
// The constants above are the Bitso model, frozen since 2026-10-08 and
// pinned by the tests. Instruments that trade on an exchange calendar (the
// eToro index CFDs, shared/pkg/cfdrisk, 2026-10-13) use the same arithmetic
// with other Params and another notion of "next day": ForecastsWith,
// EstimateWith and LogReturnsOn take them explicitly, and the original
// functions are wrappers over them with DefaultParams and the calendar day.
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

// Params are the model's settings. DefaultParams is the frozen Bitso model;
// an exchange-traded instrument uses LongWindow 252 (one trading year) and
// forms returns across trading days (shared/pkg/cfdrisk).
type Params struct {
	Lambda         float64 // EWMA decay
	SeedReturns    int     // returns averaged to seed the EWMA
	LongWindow     int     // equal-weighted window, in returns
	MinReturns     int     // fewer: no estimate
	BacktestWindow int     // Basel observation window, in returns
	Confidence     float64 // one-sided VaR level
}

// DefaultParams is the frozen Bitso model: the package constants.
func DefaultParams() Params {
	return Params{Lambda: Lambda, SeedReturns: SeedReturns, LongWindow: LongWindow,
		MinReturns: MinReturns, BacktestWindow: BacktestWindow, Confidence: Confidence}
}

// Z is the normal quantile at p.Confidence. At the default confidence it is
// the constant Z, so the frozen model's arithmetic is unchanged to the bit.
func (p Params) Z() float64 {
	if p.Confidence == Confidence {
		return Z
	}
	return math.Sqrt2 * math.Erfinv(2*p.Confidence-1)
}

// Close is one daily close, labelled by its calendar date (UTC midnight or
// any time on that date; only the date is used).
type Close struct {
	Date  time.Time
	Price float64
}

// Return is the log return from the previous day's close to Date's, where
// "previous day" is the calendar day (LogReturns) or whatever the chain
// given to LogReturnsOn says.
type Return struct {
	Date time.Time
	R    float64
}

func day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// NextCalendarDay is the chain of the Bitso model: crypto trades every day.
func NextCalendarDay(d time.Time) time.Time { return day(d).AddDate(0, 0, 1) }

// LogReturns returns the log returns between closes on consecutive calendar
// days. closes must be sorted by date; non-positive prices break the chain.
func LogReturns(closes []Close) []Return {
	return LogReturnsOn(closes, NextCalendarDay)
}

// LogReturnsOn returns the log returns between consecutive closes a, b for
// which b's date is next(a's date): the next calendar day, the next trading
// day, or any other chain. Anything else (a hole, a weekend bar on an
// exchange calendar) breaks the chain, exactly as a missing calendar day
// does in LogReturns. closes must be sorted by date; non-positive prices
// break the chain.
func LogReturnsOn(closes []Close, next func(time.Time) time.Time) []Return {
	var out []Return
	for i := 1; i < len(closes); i++ {
		a, b := closes[i-1], closes[i]
		if !(a.Price > 0) || !(b.Price > 0) {
			continue
		}
		if !day(b.Date).Equal(day(next(day(a.Date)))) {
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
// rets[0..i]), with the frozen windows and the given decay.
func Forecasts(rets []Return, lambda float64) []Forecast {
	p := DefaultParams()
	p.Lambda = lambda
	return ForecastsWith(rets, p)
}

// ForecastsWith is Forecasts with explicit Params. The recursion is the
// one documented in the package comment; only the windows are parameters.
func ForecastsWith(rets []Return, p Params) []Forecast {
	f := make([]Forecast, len(rets))
	var v, sumSq float64
	for i, r := range rets {
		sq := r.R * r.R
		sumSq += sq
		switch {
		case i < p.SeedReturns-1:
		case i == p.SeedReturns-1:
			v = sumSq / float64(p.SeedReturns)
		default:
			v = p.Lambda*v + (1-p.Lambda)*sq
		}
		if i >= p.LongWindow {
			old := rets[i-p.LongWindow].R
			sumSq -= old * old
		}
		n := i + 1
		if n > p.LongWindow {
			n = p.LongWindow
		}
		if i+1 < p.MinReturns {
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
// historical-simulation window from daily closes (the frozen model).
func EstimateFromCloses(closes []Close, lambda float64) (Estimate, error) {
	p := DefaultParams()
	p.Lambda = lambda
	return EstimateWith(closes, NextCalendarDay, p)
}

// EstimateWith is EstimateFromCloses with an explicit chain (see
// LogReturnsOn) and Params: the forecast after the last return, the
// backtest over the last p.BacktestWindow returns and the last
// p.LongWindow returns for historical simulation.
func EstimateWith(closes []Close, next func(time.Time) time.Time, p Params) (Estimate, error) {
	rets := LogReturnsOn(closes, next)
	if len(rets) < p.MinReturns {
		return Estimate{Returns: len(rets)}, ErrInsufficient
	}
	f := ForecastsWith(rets, p)
	e := Estimate{
		Forecast: f[len(f)-1],
		Returns:  len(rets),
		LastDate: rets[len(rets)-1].Date,
		Backtest: BacktestForecastsWith(rets, f, p),
	}
	from := len(rets) - p.LongWindow
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
	return BacktestForecastsWith(rets, f, DefaultParams())
}

// BacktestForecastsWith is BacktestForecasts with explicit Params. The zone
// thresholds are Basel's for 250 observations at 99 % (BaselZone) and are
// not rescaled for other settings.
func BacktestForecastsWith(rets []Return, f []Forecast, p Params) Backtest {
	var b Backtest
	from := len(rets) - p.BacktestWindow
	if from < 1 {
		from = 1
	}
	z := p.Z()
	for i := from; i < len(rets); i++ {
		prev := f[i-1]
		if !prev.Ready {
			continue
		}
		b.Observations++
		v := z * prev.Vol
		simple := math.Expm1(rets[i].R)
		if simple < -v {
			b.ExceptionsLong++
		}
		if simple > v {
			b.ExceptionsShort++
		}
	}
	b.KupiecLong = KupiecPOF(b.Observations, b.ExceptionsLong, 1-p.Confidence)
	b.KupiecShort = KupiecPOF(b.Observations, b.ExceptionsShort, 1-p.Confidence)
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
