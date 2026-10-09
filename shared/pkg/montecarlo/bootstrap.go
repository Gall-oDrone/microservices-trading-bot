// Package montecarlo simulates the frozen daily rule (shared/pkg/dailyrule)
// on resampled price paths, and computes the historical statistics that
// calibrate it: the distribution of closed round trips and the rule against
// buy-and-hold per calendar year (plan §6.4.11).
//
// Reporting only. The pre-registrations freeze the rule and its evaluation;
// nothing here may be used to choose parameters. The rule and the simulator
// are called unchanged: synthetic bars go through dailyrule.Trend and
// dailyrule.Simulate exactly as real bars do.
//
// Resampling. Each historical day i contributes one pair: the overnight gap
// Open_i/Close_{i-1} and the intraday move Close_i/Open_i, so fills at the
// next open keep their real distribution. Pairs are drawn with the
// stationary bootstrap (Politis and Romano, 1994): consecutive days in
// blocks of geometric length (mean MeanBlock), which keeps volatility
// clustering and short trends. Independent draws would erase the fat tails
// and trends the rule depends on. Long trends are still cut, so the trend
// rule's upside is understated; read the results as a lower bound for it.
package montecarlo

import (
	"errors"
	"math"
	"math/rand"
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/dailyrule"
)

// Defaults.
const (
	DefaultPaths     = 2000
	DefaultHorizon   = 365 // days
	DefaultMeanBlock = 20  // days
	DefaultSeed      = 1
	MaxPaths         = 20000
	histBins         = 30
)

// Config parameterises a run.
type Config struct {
	Paths     int             `json:"paths"`
	Horizon   int             `json:"horizon_days"`
	MeanBlock int             `json:"mean_block_days"`
	Seed      int64           `json:"seed"`
	SMA       int             `json:"sma"`
	Costs     dailyrule.Costs `json:"costs"`
}

// Dist summarises one outcome over the paths.
type Dist struct {
	Mean float64 `json:"mean"`
	P5   float64 `json:"p5"`
	P25  float64 `json:"p25"`
	P50  float64 `json:"p50"`
	P75  float64 `json:"p75"`
	P95  float64 `json:"p95"`
}

// Prob is a probability over the paths with a 95 % Wilson interval.
type Prob struct {
	P  float64 `json:"p"`
	Lo float64 `json:"lo"`
	Hi float64 `json:"hi"`
}

// Bin is one histogram bin of returns (fractions), with path counts.
type Bin struct {
	Lo    float64 `json:"lo"`
	Hi    float64 `json:"hi"`
	Trend int     `json:"trend"`
	Hold  int     `json:"hold"`
}

// Summary is a run's result. Returns and drawdowns are fractions.
type Summary struct {
	Config     Config  `json:"config"`
	SampleFrom string  `json:"sample_from"` // first day whose moves were resampled
	SampleTo   string  `json:"sample_to"`
	SampleDays int     `json:"sample_days"`
	StartClose float64 `json:"start_close"` // the last real close the paths continue from
	StartLong  bool    `json:"start_long"`  // the rule's position at that close

	TrendReturn  Dist  `json:"trend_return"`
	HoldReturn   Dist  `json:"hold_return"`
	Excess       Dist  `json:"excess"` // trend minus hold, per path
	TrendMaxDD   Dist  `json:"trend_max_dd"`
	HoldMaxDD    Dist  `json:"hold_max_dd"`
	TrendTrips   Dist  `json:"trend_round_trips"`
	PTrendLoss   Prob  `json:"p_trend_loss"`
	PHoldLoss    Prob  `json:"p_hold_loss"`
	PBeatsHold   Prob  `json:"p_beats_hold"`   // H2: trend return > hold return
	PShallowerDD Prob  `json:"p_shallower_dd"` // H1: trend max DD < hold max DD
	Histogram    []Bin `json:"histogram"`
}

// pair is one day's resampled moves.
type pair struct{ gap, intra float64 }

// Run simulates cfg.Paths paths of cfg.Horizon days continuing from the
// last bar of hist, resampling the days of hist[sampleFrom:] (sampleFrom
// >= 1: day i needs the close of day i-1). hist must be sorted, gap-free
// enough for the rule (dailyrule.MaxGapDays) at its end, and at least
// cfg.SMA bars long.
func Run(hist []dailyrule.Bar, sampleFrom int, cfg Config) (Summary, error) {
	if cfg.SMA <= 0 || cfg.Paths <= 0 || cfg.Paths > MaxPaths || cfg.Horizon < 20 || cfg.MeanBlock < 1 {
		return Summary{}, errors.New("montecarlo: need SMA > 0, 1..20000 paths, horizon >= 20 days, mean block >= 1")
	}
	if sampleFrom < 1 {
		sampleFrom = 1
	}
	if len(hist) < cfg.SMA || len(hist)-sampleFrom < 2*cfg.MeanBlock {
		return Summary{}, errors.New("montecarlo: not enough history")
	}
	pairs := make([]pair, 0, len(hist)-sampleFrom)
	for i := sampleFrom; i < len(hist); i++ {
		p, o, c := hist[i-1].Close, hist[i].Open, hist[i].Close
		if p <= 0 || o <= 0 || c <= 0 {
			continue
		}
		pairs = append(pairs, pair{gap: o / p, intra: c / o})
	}
	if len(pairs) < 2*cfg.MeanBlock {
		return Summary{}, errors.New("montecarlo: not enough usable days")
	}

	seed := hist[len(hist)-cfg.SMA:]
	n := len(seed) + cfg.Horizon
	bars := make([]dailyrule.Bar, n)
	copy(bars, seed)
	hold := make([]bool, n)
	for i := range hold {
		hold[i] = true
	}
	lo, hi := len(seed), n-1
	rng := rand.New(rand.NewSource(cfg.Seed))
	pStay := 1 - 1/float64(cfg.MeanBlock)

	tr, ho, ex, tdd, hdd, trips := make([]float64, cfg.Paths), make([]float64, cfg.Paths), make([]float64, cfg.Paths),
		make([]float64, cfg.Paths), make([]float64, cfg.Paths), make([]float64, cfg.Paths)
	var tLoss, hLoss, beats, shallower int
	last := seed[len(seed)-1].Date
	for p := 0; p < cfg.Paths; p++ {
		j := rng.Intn(len(pairs))
		prev := seed[len(seed)-1].Close
		for k := lo; k < n; k++ {
			if k > lo {
				if rng.Float64() < pStay {
					j = (j + 1) % len(pairs)
				} else {
					j = rng.Intn(len(pairs))
				}
			}
			o := prev * pairs[j].gap
			c := o * pairs[j].intra
			bars[k] = dailyrule.Bar{Date: last.Add(time.Duration(k-lo+1) * 24 * time.Hour), Open: o, High: math.Max(o, c), Low: math.Min(o, c), Close: c}
			prev = c
		}
		want := dailyrule.Trend(bars, cfg.SMA)
		t := dailyrule.Simulate(bars, want, lo, hi, cfg.Costs)
		h := dailyrule.Simulate(bars, hold, lo, hi, cfg.Costs)
		tr[p], ho[p] = t.ReturnPct/100, h.ReturnPct/100
		ex[p] = tr[p] - ho[p]
		tdd[p], hdd[p] = t.MaxDDPct/100, h.MaxDDPct/100
		trips[p] = float64(t.RoundTrips)
		if tr[p] < 0 {
			tLoss++
		}
		if ho[p] < 0 {
			hLoss++
		}
		if tr[p] > ho[p] {
			beats++
		}
		if tdd[p] < hdd[p] {
			shallower++
		}
	}
	startLong := dailyrule.Trend(seed, cfg.SMA)[len(seed)-1]
	s := Summary{
		Config: cfg, SampleFrom: hist[sampleFrom].Date.Format("2006-01-02"), SampleTo: hist[len(hist)-1].Date.Format("2006-01-02"),
		SampleDays: len(pairs), StartClose: seed[len(seed)-1].Close, StartLong: startLong,
		TrendReturn: dist(tr), HoldReturn: dist(ho), Excess: dist(ex), TrendMaxDD: dist(tdd), HoldMaxDD: dist(hdd), TrendTrips: dist(trips),
		PTrendLoss: wilson(tLoss, cfg.Paths), PHoldLoss: wilson(hLoss, cfg.Paths),
		PBeatsHold: wilson(beats, cfg.Paths), PShallowerDD: wilson(shallower, cfg.Paths),
	}
	s.Histogram = histogram(tr, ho)
	return s, nil
}

// dist sorts a copy of xs and reads the quantiles (nearest rank).
func dist(xs []float64) Dist {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	q := func(p float64) float64 { return s[int(math.Round(p*float64(len(s)-1)))] }
	var sum float64
	for _, x := range s {
		sum += x
	}
	return Dist{Mean: sum / float64(len(s)), P5: q(.05), P25: q(.25), P50: q(.5), P75: q(.75), P95: q(.95)}
}

// wilson is k/n with its 95 % Wilson score interval.
func wilson(k, n int) Prob {
	if n == 0 {
		return Prob{}
	}
	const z = 1.959963984540054
	p := float64(k) / float64(n)
	nn := float64(n)
	d := 1 + z*z/nn
	c := (p + z*z/(2*nn)) / d
	h := z * math.Sqrt(p*(1-p)/nn+z*z/(4*nn*nn)) / d
	return Prob{P: p, Lo: math.Max(0, c-h), Hi: math.Min(1, c+h)}
}

// histogram bins both return sets over their common 1st..99th percentile
// range; the outer bins also hold the tails.
func histogram(a, b []float64) []Bin {
	all := append(append([]float64(nil), a...), b...)
	sort.Float64s(all)
	lo := all[int(0.01*float64(len(all)-1))]
	hi := all[int(0.99*float64(len(all)-1))]
	if !(hi > lo) {
		hi = lo + 1e-9
	}
	w := (hi - lo) / histBins
	bins := make([]Bin, histBins)
	for i := range bins {
		bins[i] = Bin{Lo: lo + float64(i)*w, Hi: lo + float64(i+1)*w}
	}
	idx := func(x float64) int {
		i := int((x - lo) / w)
		return min(max(i, 0), histBins-1)
	}
	for _, x := range a {
		bins[idx(x)].Trend++
	}
	for _, x := range b {
		bins[idx(x)].Hold++
	}
	return bins
}
