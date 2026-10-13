package cfdrisk

import (
	"errors"
	"math"
	"math/rand/v2"
	"sort"

	"bitso-trading-platform/shared/pkg/cfdsim"
	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/shared/pkg/montecarlo"
)

// Monte Carlo of the frozen rule on an index CFD (M12).
//
// The resampling is montecarlo.Run's: each historical day contributes the
// pair (overnight gap Open_i/Close_{i-1}, intraday move Close_i/Open_i),
// drawn with the stationary block bootstrap (mean block MeanBlock days) so
// volatility clustering and short trends survive and fills at the next
// open keep their real distribution. Two things change for a CFD:
//
//   - synthetic dates advance by NYSE trading days (mktcal.NextTradingDay),
//     not calendar days, so a path has the real pattern of one-night and
//     three-night (weekend) holds and holiday nights;
//   - every path is priced with cfdsim.Simulate, which charges the spread
//     per leg and overnight financing per calendar night, and flags the
//     margin close-out. The hold benchmark is priced twice: as the same
//     CFD held throughout (pays financing every night) and as the ETF
//     (fixed commission per order, no financing, no dividends in these
//     paths: the index closes are price returns).
//
// Reporting only, as montecarlo.Run is: the rule and both simulators are
// called unchanged, and nothing here may be used to choose a parameter.
// montecarlo.Run itself is not modified (frozen with the Bitso evidence).

// Monte Carlo defaults.
const (
	DefaultPaths     = montecarlo.DefaultPaths
	DefaultHorizon   = mktcal.TradingDaysPerYear // trading days
	DefaultMeanBlock = montecarlo.DefaultMeanBlock
	DefaultSeed      = montecarlo.DefaultSeed
	MaxPaths         = montecarlo.MaxPaths
)

// MCConfig parameterises a run.
type MCConfig struct {
	Paths     int    `json:"paths"`
	Horizon   int    `json:"horizon_trading_days"`
	MeanBlock int    `json:"mean_block_days"`
	Seed      uint64 `json:"seed"`
	SMA       int    `json:"sma"`
	// CFD prices the trend rule and the CFD hold; ETF prices the ETF hold.
	CFD cfdsim.Costs `json:"-"`
	ETF cfdsim.Costs `json:"-"`
	// onPath, for tests: called with every simulated path before pricing.
	onPath func(bars []dailyrule.Bar, want []bool, lo, hi int)
}

// MCSummary is a run's result. Returns, drawdowns and cost shares are
// fractions of the starting equity.
type MCSummary struct {
	Config     MCConfig `json:"config"`
	SampleFrom string   `json:"sample_from"` // first day whose moves were resampled
	SampleTo   string   `json:"sample_to"`
	SampleDays int      `json:"sample_days"`
	StartClose float64  `json:"start_close"` // the last real close the paths continue from
	StartLong  bool     `json:"start_long"`  // the rule's position at that close
	HorizonEnd string   `json:"horizon_end"` // the last synthetic trading day

	TrendReturn   montecarlo.Dist `json:"trend_return"`
	CFDHoldReturn montecarlo.Dist `json:"cfd_hold_return"`
	ETFHoldReturn montecarlo.Dist `json:"etf_hold_return"`
	ExcessCFD     montecarlo.Dist `json:"excess_vs_cfd_hold"` // trend minus CFD hold, per path
	ExcessETF     montecarlo.Dist `json:"excess_vs_etf_hold"`
	TrendMaxDD    montecarlo.Dist `json:"trend_max_dd"`
	CFDHoldMaxDD  montecarlo.Dist `json:"cfd_hold_max_dd"`
	TrendTrips    montecarlo.Dist `json:"trend_round_trips"`
	TrendFinance  montecarlo.Dist `json:"trend_financing"`    // financing paid by the rule, fraction of start
	HoldFinance   montecarlo.Dist `json:"cfd_hold_financing"` // financing paid by the CFD hold
	TrendTrade    montecarlo.Dist `json:"trend_trade_cost"`   // spread paid by the rule
	FinanceShare  montecarlo.Dist `json:"trend_financing_share_of_costs"`

	PTrendLoss     montecarlo.Prob `json:"p_trend_loss"`
	PCFDHoldLoss   montecarlo.Prob `json:"p_cfd_hold_loss"`
	PBeatsCFDHold  montecarlo.Prob `json:"p_beats_cfd_hold"` // H2: trend return > CFD hold return
	PBeatsETFHold  montecarlo.Prob `json:"p_beats_etf_hold"` // H2b: trend return > ETF hold return
	PShallowerDD   montecarlo.Prob `json:"p_shallower_dd"`   // H1: trend max DD < CFD hold max DD
	PTrendCloseOut montecarlo.Prob `json:"p_trend_margin_close_out"`
	PHoldCloseOut  montecarlo.Prob `json:"p_cfd_hold_margin_close_out"`
}

type pair struct{ gap, intra float64 }

// MonteCarlo simulates cfg.Paths paths of cfg.Horizon trading days
// continuing from the last bar of hist, resampling the days of
// hist[sampleFrom:] (sampleFrom >= 1: day i needs the close of day i-1).
// hist must be sorted, on trading days, gap-free enough for the rule
// (dailyrule.MaxGapDays) at its end, and at least cfg.SMA bars long.
func MonteCarlo(hist []dailyrule.Bar, sampleFrom int, cfg MCConfig) (MCSummary, error) {
	if cfg.SMA <= 0 || cfg.Paths <= 0 || cfg.Paths > MaxPaths || cfg.Horizon < 20 || cfg.MeanBlock < 1 {
		return MCSummary{}, errors.New("cfdrisk: need SMA > 0, 1..20000 paths, horizon >= 20 days, mean block >= 1")
	}
	if sampleFrom < 1 {
		sampleFrom = 1
	}
	if len(hist) < cfg.SMA || len(hist)-sampleFrom < 2*cfg.MeanBlock {
		return MCSummary{}, errors.New("cfdrisk: not enough history")
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
		return MCSummary{}, errors.New("cfdrisk: not enough usable days")
	}

	seed := hist[len(hist)-cfg.SMA:]
	n := len(seed) + cfg.Horizon
	bars := make([]dailyrule.Bar, n)
	copy(bars, seed)
	// Trading-day dates for the horizon, shared by every path.
	d := seed[len(seed)-1].Date
	for k := len(seed); k < n; k++ {
		d = mktcal.NextTradingDay(d)
		bars[k].Date = d
	}
	hold := make([]bool, n)
	for i := range hold {
		hold[i] = true
	}
	lo, hi := len(seed), n-1
	rng := rand.New(rand.NewPCG(cfg.Seed, 0))
	pStay := 1 - 1/float64(cfg.MeanBlock)

	P := cfg.Paths
	tr, ch, eh := make([]float64, P), make([]float64, P), make([]float64, P)
	exC, exE := make([]float64, P), make([]float64, P)
	tdd, hdd, trips := make([]float64, P), make([]float64, P), make([]float64, P)
	tfin, hfin, ttrade, share := make([]float64, P), make([]float64, P), make([]float64, P), make([]float64, P)
	var tLoss, hLoss, beatsC, beatsE, shallower, tCO, hCO int
	for p := 0; p < P; p++ {
		j := rng.IntN(len(pairs))
		prev := seed[len(seed)-1].Close
		for k := lo; k < n; k++ {
			if k > lo {
				if rng.Float64() < pStay {
					j = (j + 1) % len(pairs)
				} else {
					j = rng.IntN(len(pairs))
				}
			}
			o := prev * pairs[j].gap
			c := o * pairs[j].intra
			bars[k].Open, bars[k].High, bars[k].Low, bars[k].Close = o, math.Max(o, c), math.Min(o, c), c
			prev = c
		}
		want := dailyrule.Trend(bars, cfg.SMA)
		if cfg.onPath != nil {
			cfg.onPath(bars, want, lo, hi)
		}
		t := cfdsim.Simulate(bars, want, lo, hi, cfg.CFD)
		h := cfdsim.Simulate(bars, hold, lo, hi, cfg.CFD)
		e := cfdsim.Simulate(bars, hold, lo, hi, cfg.ETF)
		tr[p], ch[p], eh[p] = t.ReturnPct/100, h.ReturnPct/100, e.ReturnPct/100
		exC[p], exE[p] = tr[p]-ch[p], tr[p]-eh[p]
		tdd[p], hdd[p] = t.MaxDDPct/100, h.MaxDDPct/100
		trips[p] = float64(t.RoundTrips)
		tfin[p], hfin[p], ttrade[p] = t.FinancePct/100, h.FinancePct/100, t.TradeCostPct/100
		if tot := tfin[p] + ttrade[p]; tot > 0 {
			share[p] = tfin[p] / tot
		}
		if tr[p] < 0 {
			tLoss++
		}
		if ch[p] < 0 {
			hLoss++
		}
		if tr[p] > ch[p] {
			beatsC++
		}
		if tr[p] > eh[p] {
			beatsE++
		}
		if tdd[p] < hdd[p] {
			shallower++
		}
		if t.MarginCloseOut {
			tCO++
		}
		if h.MarginCloseOut {
			hCO++
		}
	}
	startLong := dailyrule.Trend(seed, cfg.SMA)[len(seed)-1]
	return MCSummary{
		Config: cfg, SampleFrom: hist[sampleFrom].Date.Format("2006-01-02"), SampleTo: hist[len(hist)-1].Date.Format("2006-01-02"),
		SampleDays: len(pairs), StartClose: seed[len(seed)-1].Close, StartLong: startLong, HorizonEnd: bars[hi].Date.Format("2006-01-02"),
		TrendReturn: dist(tr), CFDHoldReturn: dist(ch), ETFHoldReturn: dist(eh), ExcessCFD: dist(exC), ExcessETF: dist(exE),
		TrendMaxDD: dist(tdd), CFDHoldMaxDD: dist(hdd), TrendTrips: dist(trips),
		TrendFinance: dist(tfin), HoldFinance: dist(hfin), TrendTrade: dist(ttrade), FinanceShare: dist(share),
		PTrendLoss: wilson(tLoss, P), PCFDHoldLoss: wilson(hLoss, P),
		PBeatsCFDHold: wilson(beatsC, P), PBeatsETFHold: wilson(beatsE, P), PShallowerDD: wilson(shallower, P),
		PTrendCloseOut: wilson(tCO, P), PHoldCloseOut: wilson(hCO, P),
	}, nil
}

// dist sorts a copy of xs and reads the quantiles (nearest rank), as
// montecarlo does.
func dist(xs []float64) montecarlo.Dist {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	q := func(p float64) float64 { return s[int(math.Round(p*float64(len(s)-1)))] }
	var sum float64
	for _, x := range s {
		sum += x
	}
	return montecarlo.Dist{Mean: sum / float64(len(s)), P5: q(.05), P25: q(.25), P50: q(.5), P75: q(.75), P95: q(.95)}
}

// wilson is k/n with its 95 % Wilson score interval, as montecarlo does.
func wilson(k, n int) montecarlo.Prob {
	if n == 0 {
		return montecarlo.Prob{}
	}
	const z = 1.959963984540054
	p := float64(k) / float64(n)
	nn := float64(n)
	d := 1 + z*z/nn
	c := (p + z*z/(2*nn)) / d
	h := z * math.Sqrt(p*(1-p)/nn+z*z/(4*nn*nn)) / d
	return montecarlo.Prob{P: p, Lo: math.Max(0, c-h), Hi: math.Min(1, c+h)}
}
