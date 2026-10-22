// Package stops recommends stop-loss and take-profit levels for a position
// from the instrument's daily volatility, and applies the guards that keep
// a level honest: never inside the spread, never wider than the policy's
// loss caps, never outside what the venue accepts.
//
// Scope. This package is pure (stdlib only, no I/O, no clock) so ui-api,
// the eToro daily executor and the reconcile can share one definition of a
// "protected" position. It decides nothing about entries or exits of the
// pre-registered rule (shared/pkg/risk's scope note applies): a stop set
// from here is a risk overlay on a position the rule or the operator opened.
//
// Model (docs/etoro/ETORO-ASSISTED-TRADE-PROTECTION-PLAN-2026-10-19.md §2):
//   - the volatility unit is ATR(14), Wilder smoothing, on closed daily bars;
//   - a risk-aversion profile is a pair of ATR multipliers, SL and TP:
//     Conservative 1.5 / 2.25, Moderate 2.0 / 4.0 (default), Aggressive 3.0 / 6.0;
//   - guards, in order: noise floor, policy loss caps, venue bounds, tick
//     rounding (stop down, target up). Each adjustment is reported in
//     Levels.Clamps so the operator sees what moved and why;
//   - the executor's own rule positions carry a disaster stop only,
//     DisasterStopPct = max(4 x ATR%, 8%), and the daily Ratchet raises a
//     stop, never lowers it.
//
// Long positions only, the only side the policy allows (x1 long or flat).
package stops

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// Profile is a risk-aversion level.
type Profile string

// Profiles. Custom is operator-typed distances that still go through the
// guards.
const (
	Conservative Profile = "conservative"
	Moderate     Profile = "moderate"
	Aggressive   Profile = "aggressive"
	Custom       Profile = "custom"
)

// DefaultProfile is what the console pre-selects.
const DefaultProfile = Moderate

// Profiles lists the named profiles in order of increasing risk.
var Profiles = []Profile{Conservative, Moderate, Aggressive}

// Multipliers are a profile's distances in ATRs. TP 0 means no take profit.
type Multipliers struct {
	SL float64 `json:"sl_atr"`
	TP float64 `json:"tp_atr"`
}

var multipliers = map[Profile]Multipliers{
	Conservative: {SL: 1.5, TP: 2.25},
	Moderate:     {SL: 2.0, TP: 4.0},
	Aggressive:   {SL: 3.0, TP: 6.0},
}

// ProfileMultipliers returns a named profile's multipliers; false for Custom
// or an unknown name.
func ProfileMultipliers(p Profile) (Multipliers, bool) {
	m, ok := multipliers[p]
	return m, ok
}

// ParseProfile accepts the profile names (case-sensitive, as sent by the
// console); "" is DefaultProfile.
func ParseProfile(s string) (Profile, error) {
	switch Profile(s) {
	case "":
		return DefaultProfile, nil
	case Conservative, Moderate, Aggressive, Custom:
		return Profile(s), nil
	}
	return "", fmt.Errorf("stops: unknown profile %q (conservative, moderate, aggressive, custom)", s)
}

// Disaster-stop parameters for the executor's rule positions (plan §2):
// the wider of DisasterATRMult x ATR and DisasterFloorPct of the entry.
const (
	DisasterATRMult  = 4.0
	DisasterFloorPct = 0.08
	// NoiseFloorPct is added to twice the quoted spread for the tightest
	// stop the guards allow.
	NoiseFloorPct = 0.0025
	// DefaultRatchetStepATR is how much higher (in ATRs) a candidate stop
	// must be before the ratchet moves it.
	DefaultRatchetStepATR = 0.25
	// DefaultATRPeriod is the Wilder period.
	DefaultATRPeriod = 14
	// DefaultRegimeLookback is how many ATR% observations the regime median
	// uses (one trading year).
	DefaultRegimeLookback = 252
)

// Bar is one closed daily candle (only the range matters here).
type Bar struct {
	High, Low, Close float64
}

// ATR is Wilder's average true range over the last n bars: the first value
// is the simple mean of the first n true ranges, then
// atr = (prev*(n-1) + tr)/n. It needs n+1 bars (the first true range uses
// the previous close); ok is false otherwise or when a bar is malformed.
func ATR(bars []Bar, n int) (atr float64, ok bool) {
	s := ATRSeries(bars, n)
	if len(s) == 0 {
		return 0, false
	}
	return s[len(s)-1], true
}

// ATRSeries returns the ATR at every bar from index n on (len(bars)-n
// values), or nil when there are not enough valid bars.
func ATRSeries(bars []Bar, n int) []float64 {
	if n <= 0 || len(bars) < n+1 {
		return nil
	}
	trs := make([]float64, 0, len(bars)-1)
	for i := 1; i < len(bars); i++ {
		b, prev := bars[i], bars[i-1]
		if !(b.High > 0) || !(b.Low > 0) || !(prev.Close > 0) || b.High < b.Low {
			return nil
		}
		tr := math.Max(b.High-b.Low, math.Max(math.Abs(b.High-prev.Close), math.Abs(b.Low-prev.Close)))
		trs = append(trs, tr)
	}
	out := make([]float64, 0, len(trs)-n+1)
	sum := 0.0
	for i := 0; i < n; i++ {
		sum += trs[i]
	}
	atr := sum / float64(n)
	out = append(out, atr)
	for i := n; i < len(trs); i++ {
		atr = (atr*float64(n-1) + trs[i]) / float64(n)
		out = append(out, atr)
	}
	return out
}

// Regime labels today's volatility against its own history.
type Regime string

// Regimes, by the ratio of today's ATR% to the lookback median of ATR%:
// below 0.75 calm, below 1.25 normal, below 1.75 elevated, else extreme.
const (
	RegimeCalm     Regime = "calm"
	RegimeNormal   Regime = "normal"
	RegimeElevated Regime = "elevated"
	RegimeExtreme  Regime = "extreme"
	RegimeUnknown  Regime = "unknown" // not enough history
)

// Volatility is what ATR and the regime say about an instrument today.
type Volatility struct {
	Period int     `json:"period"`
	ATR    float64 `json:"atr"`
	ATRPct float64 `json:"atr_pct"` // ATR / last close, fraction
	Close  float64 `json:"close"`   // last close the ATR% uses
	Bars   int     `json:"bars"`
	// Regime compares ATRPct with MedianATRPct over the lookback; Ratio is
	// ATRPct / MedianATRPct (0 when unknown).
	Regime       Regime  `json:"regime"`
	Ratio        float64 `json:"regime_ratio,omitempty"`
	MedianATRPct float64 `json:"median_atr_pct,omitempty"`
	Lookback     int     `json:"regime_lookback,omitempty"`
}

// Measure computes ATR(n) and the regime over lookback ATR% observations.
// It errors when fewer than n+1 valid bars are given; the regime alone
// degrades to RegimeUnknown when the lookback is short (fewer than n
// observations).
func Measure(bars []Bar, n, lookback int) (Volatility, error) {
	if n <= 0 {
		n = DefaultATRPeriod
	}
	if lookback <= 0 {
		lookback = DefaultRegimeLookback
	}
	series := ATRSeries(bars, n)
	if len(series) == 0 {
		return Volatility{}, fmt.Errorf("stops: ATR(%d) needs %d valid daily bars, got %d", n, n+1, len(bars))
	}
	last := bars[len(bars)-1]
	v := Volatility{Period: n, ATR: series[len(series)-1], Close: last.Close, Bars: len(bars), Regime: RegimeUnknown}
	if last.Close > 0 {
		v.ATRPct = v.ATR / last.Close
	}
	// ATR% history: series[i] belongs to bars[i+n].
	pcts := make([]float64, 0, len(series))
	for i, a := range series {
		if c := bars[i+n].Close; c > 0 {
			pcts = append(pcts, a/c)
		}
	}
	if len(pcts) > lookback {
		pcts = pcts[len(pcts)-lookback:]
	}
	if len(pcts) >= n {
		v.Lookback = len(pcts)
		v.MedianATRPct = median(pcts)
		if v.MedianATRPct > 0 {
			v.Ratio = v.ATRPct / v.MedianATRPct
			switch {
			case v.Ratio < 0.75:
				v.Regime = RegimeCalm
			case v.Ratio < 1.25:
				v.Regime = RegimeNormal
			case v.Ratio < 1.75:
				v.Regime = RegimeElevated
			default:
				v.Regime = RegimeExtreme
			}
		}
	}
	return v, nil
}

func median(xs []float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	m := len(s) / 2
	if len(s)%2 == 1 {
		return s[m]
	}
	return (s[m-1] + s[m]) / 2
}

// Bounds are the venue's rules for the instrument and direction (eToro
// eligibility leverageConfigs[]), as fractions of the entry. Zero disables
// a bound except Tick, which defaults to 0.01.
type Bounds struct {
	MinStopLossPct   float64 `json:"min_sl_pct"`
	MaxStopLossPct   float64 `json:"max_sl_pct"`
	MaxTakeProfitPct float64 `json:"max_tp_pct"`
	Tick             float64 `json:"tick"`
}

// Limits are the risk policy's caps (risk.BookLimits), as fractions. Zero
// disables.
type Limits struct {
	MinStopLossPct   float64 `json:"min_sl_pct"`
	MaxStopLossPct   float64 `json:"max_sl_pct"`
	MaxLossPctEquity float64 `json:"max_loss_pct_equity"`
}

// Input describes the position (or the one about to be opened).
type Input struct {
	// Entry is the price the percentages are measured from: the ask for an
	// open, the open rate for an existing position. Required.
	Entry float64
	// Mark is the current price the ATR distances are measured from (the
	// bid for a long). Zero means Entry. A stop must sit below Mark and a
	// target above it.
	Mark float64
	// Units sizes the loss and gain in account currency; when 0, Amount
	// (notional in account currency) / Entry is used.
	Units  float64
	Amount float64
	// SpreadPct is (ask - bid) / mid, a fraction; the noise floor uses it.
	SpreadPct float64
	// Equity is the account equity (for Limits.MaxLossPctEquity); 0 skips it.
	Equity float64
	// ATR is the absolute average true range (Measure or ATR).
	ATR    float64
	Bounds Bounds
	Limits Limits
}

func (in Input) units() float64 {
	if in.Units > 0 {
		return in.Units
	}
	if in.Entry > 0 && in.Amount > 0 {
		return in.Amount / in.Entry
	}
	return 0
}

func (in Input) tick() float64 {
	if in.Bounds.Tick > 0 {
		return in.Bounds.Tick
	}
	return 0.01
}

// Validate rejects an input the guards cannot work with.
func (in Input) Validate() error {
	switch {
	case !(in.Entry > 0) || math.IsInf(in.Entry, 0):
		return errors.New("stops: entry price must be positive")
	case in.Mark < 0 || math.IsInf(in.Mark, 0):
		return errors.New("stops: mark price must be >= 0")
	case !(in.ATR >= 0) || math.IsInf(in.ATR, 0):
		return errors.New("stops: ATR must be >= 0")
	case in.SpreadPct < 0 || in.Equity < 0 || in.Units < 0 || in.Amount < 0:
		return errors.New("stops: spread, equity, units and amount must be >= 0")
	}
	return nil
}

// Levels is one recommendation: the stop and target as rates and as
// fractions of Entry, what they mean in money, and what the guards changed.
type Levels struct {
	Profile Profile `json:"profile"`
	// SL is always set when Feasible; TP is 0 when no take profit.
	SLRate float64 `json:"sl_rate"`
	SLPct  float64 `json:"sl_pct"`
	TPRate float64 `json:"tp_rate,omitempty"`
	TPPct  float64 `json:"tp_pct,omitempty"`
	// SLATR / TPATR are the distances in ATRs after the guards (0 without an ATR).
	SLATR float64 `json:"sl_atr,omitempty"`
	TPATR float64 `json:"tp_atr,omitempty"`
	// MaxLossUSD is units x (entry - stop); TargetUSD units x (target - entry);
	// RR their ratio (0 without a target). Account currency.
	MaxLossUSD float64 `json:"max_loss_usd"`
	TargetUSD  float64 `json:"target_usd,omitempty"`
	RR         float64 `json:"rr,omitempty"`
	// Clamps lists each guard that moved a level, oldest first.
	Clamps []string `json:"clamps"`
	// Feasible is false when the guards contradict each other (the floor is
	// above the cap, or the stop would sit at or above the mark); Reason
	// says why. Nothing should be sent from an infeasible Levels.
	Feasible bool   `json:"feasible"`
	Reason   string `json:"reason,omitempty"`
}

// Recommend builds the named profiles from in. It errors only on a bad
// input; a profile the guards cannot satisfy is returned with Feasible
// false.
func Recommend(in Input) (map[Profile]Levels, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	if !(in.ATR > 0) {
		return nil, errors.New("stops: the named profiles need an ATR > 0")
	}
	out := make(map[Profile]Levels, len(Profiles))
	mark := in.markOrEntry()
	for _, p := range Profiles {
		m := multipliers[p]
		// Distances are measured from the mark and expressed against the entry.
		sl := (in.Entry - (mark - m.SL*in.ATR)) / in.Entry
		tp := 0.0
		if m.TP > 0 {
			tp = (mark + m.TP*in.ATR - in.Entry) / in.Entry
		}
		out[p] = in.build(p, sl, tp)
	}
	return out, nil
}

// Build applies the guards to operator-chosen distances (fractions of
// Entry; tpPct 0 = no take profit) under the Custom profile.
func Build(in Input, slPct, tpPct float64) (Levels, error) {
	if err := in.Validate(); err != nil {
		return Levels{}, err
	}
	switch {
	case math.IsNaN(slPct) || math.IsInf(slPct, 0) || slPct <= 0:
		return Levels{}, errors.New("stops: stop-loss distance must be a positive fraction of the entry")
	case math.IsNaN(tpPct) || math.IsInf(tpPct, 0) || tpPct < 0:
		return Levels{}, errors.New("stops: take-profit distance must be >= 0")
	}
	return in.build(Custom, slPct, tpPct), nil
}

func (in Input) markOrEntry() float64 {
	if in.Mark > 0 {
		return in.Mark
	}
	return in.Entry
}

// build runs the guards in the documented order.
func (in Input) build(p Profile, slPct, tpPct float64) Levels {
	l := Levels{Profile: p, Clamps: []string{}, Feasible: true}
	clamp := func(format string, args ...any) { l.Clamps = append(l.Clamps, fmt.Sprintf(format, args...)) }
	units := in.units()
	mark := in.markOrEntry()

	// 1. Noise floor.
	floor := math.Max(in.Bounds.MinStopLossPct, 2*in.SpreadPct+NoiseFloorPct)
	if in.Limits.MinStopLossPct > floor {
		floor = in.Limits.MinStopLossPct
	}
	if slPct < floor {
		clamp("stop widened from %.2f%% to the %.2f%% floor (twice the spread + %.2f%%, the venue's minimum, or the policy's minimum)", slPct*100, floor*100, NoiseFloorPct*100)
		slPct = floor
	}

	// 2. Policy loss caps.
	ceiling := math.Inf(1)
	ceilingWhy := ""
	if in.Limits.MaxStopLossPct > 0 && in.Limits.MaxStopLossPct < ceiling {
		ceiling, ceilingWhy = in.Limits.MaxStopLossPct, fmt.Sprintf("the policy's %.1f%% maximum stop distance", in.Limits.MaxStopLossPct*100)
	}
	if in.Limits.MaxLossPctEquity > 0 && in.Equity > 0 && units > 0 {
		maxLoss := in.Limits.MaxLossPctEquity * in.Equity
		if byEquity := maxLoss / (units * in.Entry); byEquity < ceiling {
			ceiling, ceilingWhy = byEquity, fmt.Sprintf("the policy's maximum loss of %.2f%% of equity (%.2f)", in.Limits.MaxLossPctEquity*100, maxLoss)
		}
	}
	// 3. Venue ceiling (a cap too).
	if in.Bounds.MaxStopLossPct > 0 && in.Bounds.MaxStopLossPct < ceiling {
		ceiling, ceilingWhy = in.Bounds.MaxStopLossPct, fmt.Sprintf("the venue's %.0f%% maximum stop distance", in.Bounds.MaxStopLossPct*100)
	}
	if slPct > ceiling {
		if ceiling < floor {
			l.Feasible = false
			l.Reason = fmt.Sprintf("no stop satisfies both the %.2f%% floor and %s (%.2f%%)", floor*100, ceilingWhy, ceiling*100)
		} else {
			clamp("stop tightened from %.2f%% to %.2f%%: %s", slPct*100, ceiling*100, ceilingWhy)
		}
		slPct = ceiling
	}
	if tpPct > 0 && in.Bounds.MaxTakeProfitPct > 0 && tpPct > in.Bounds.MaxTakeProfitPct {
		clamp("target lowered from %.2f%% to the venue's %.0f%% maximum", tpPct*100, in.Bounds.MaxTakeProfitPct*100)
		tpPct = in.Bounds.MaxTakeProfitPct
	}

	// 4. Rounding: stop down, target up, to the tick. A stop that was
	// clamped to the ceiling must not be rounded past it (the risk check
	// and the venue measure the distance exactly): at the ceiling the
	// stop rounds up instead, one tick inside the cap.
	tick := in.tick()
	l.SLRate = math.Floor(in.Entry*(1-slPct)/tick+1e-9) * tick
	l.SLRate = roundTick(l.SLRate, tick)
	if l.SLRate <= 0 {
		l.SLRate = tick
	}
	l.SLPct = (in.Entry - l.SLRate) / in.Entry
	if !math.IsInf(ceiling, 1) && l.SLPct > ceiling+1e-12 {
		l.SLRate = roundTick(math.Ceil(in.Entry*(1-ceiling)/tick-1e-9)*tick, tick)
		l.SLPct = (in.Entry - l.SLRate) / in.Entry
	}
	if tpPct > 0 {
		l.TPRate = roundTick(math.Ceil(in.Entry*(1+tpPct)/tick-1e-9)*tick, tick)
		l.TPPct = (l.TPRate - in.Entry) / in.Entry
	}

	// Against the mark: a stop must be below it, a target above it.
	if l.SLRate >= mark {
		l.Feasible = false
		l.Reason = fmt.Sprintf("the stop %.2f is not below the current price %.2f (the position is already past it)", l.SLRate, mark)
	}
	if l.TPRate > 0 && l.TPRate <= mark {
		l.Feasible = false
		l.Reason = fmt.Sprintf("the target %.2f is not above the current price %.2f", l.TPRate, mark)
	}

	if in.ATR > 0 {
		l.SLATR = (mark - l.SLRate) / in.ATR
		if l.TPRate > 0 {
			l.TPATR = (l.TPRate - mark) / in.ATR
		}
	}
	if units > 0 {
		l.MaxLossUSD = units * (in.Entry - l.SLRate)
		if l.TPRate > 0 {
			l.TargetUSD = units * (l.TPRate - in.Entry)
			if l.MaxLossUSD > 0 {
				l.RR = l.TargetUSD / l.MaxLossUSD
			}
		}
	}
	return l
}

// roundTick removes floating noise after a floor/ceil to the tick.
func roundTick(x, tick float64) float64 {
	decimals := 0
	for t := tick; t < 1 && decimals < 10; t *= 10 {
		decimals++
	}
	p := math.Pow(10, float64(decimals))
	return math.Round(x*p) / p
}

// DisasterStopPct is the executor's rule-position stop as a fraction of the
// entry: the wider of DisasterATRMult x ATR% and DisasterFloorPct.
func DisasterStopPct(atrPct float64) float64 {
	return math.Max(DisasterATRMult*atrPct, DisasterFloorPct)
}

// Ratchet decides whether a held long's stop moves up to the candidate
// (close - k x ATR, computed by the caller): only when the candidate is
// higher than the current stop by at least step (an absolute price
// distance, e.g. DefaultRatchetStepATR x ATR) and still below the mark. A
// position with no stop (current <= 0) always takes a valid candidate. It
// never returns a lower stop.
func Ratchet(current, candidate, step, mark float64) (newStop float64, raise bool) {
	if !(candidate > 0) || (mark > 0 && candidate >= mark) {
		return current, false
	}
	if current <= 0 {
		return candidate, true
	}
	if candidate-current >= math.Max(step, 0) && candidate > current {
		return candidate, true
	}
	return current, false
}

// HasStop reports whether a venue stop rate is a real stop. eToro reports
// 0.01 on a position with no stop (demo evidence 2026-10-10); anything at
// or below NoStopRate is treated as none.
func HasStop(rate float64) bool { return rate > NoStopRate }

// NoStopRate is eToro's placeholder for "no stop loss".
const NoStopRate = 0.01
