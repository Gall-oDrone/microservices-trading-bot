// Command rotation-research studies dual momentum between the eToro index
// instruments NSDQ100 and SPX500 (and cash) as a candidate next cell after
// the SMA50 forward tests. Rule: shared/pkg/rotation. Simulator:
// cfdsim.SimulateRotation (same fills and costs as the frozen SMA50 study).
//
// Two product variants are priced:
//   - CFD: NSDQ100 / SPX500 at x1, frozen spreads, rate-linked financing per
//     calendar night (as cmd/index-research);
//   - ETF: QQQ / SPY at x1 (a real asset: 1.50 USD per order on the position
//     size, tiny spread, no financing, dividends via adjusted closes).
//
// Signals are computed on raw closes (what a live route serves); ETF P&L on
// total-return bars. Benchmarks: hold QQQ, hold SPY, and the frozen SMA50
// rule on each. Data: the Yahoo CSVs already committed for the index-CFD
// study (research-only input; no live decision depends on it).
//
//	go run ./cmd/rotation-research -data ../../docs/backtest-readiness/evidence-2026-10-10/data \
//	  -etoro ../../docs/etoro/evidence-2026-10-10 -out ../../docs/backtest-readiness/evidence-<date>
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/cfdsim"
	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/etorodaily"
	"bitso-trading-platform/shared/pkg/rotation"
	"bitso-trading-platform/shared/pkg/yahoodaily"
)

type variant struct {
	Name     string `json:"name"`
	Lookback int    `json:"lookback_bars"`
	Skip     int    `json:"skip_bars"`
	TBill    bool   `json:"tbill_hurdle"`
}

var variants = []variant{
	{"dm_12_1", 252, 21, false},
	{"dm_12_1_tbill", 252, 21, true},
	{"dm_12", 252, 0, false},
	{"dm_6", 126, 0, false},
	{"dm_6_tbill", 126, 0, true},
	{"dm_3", 63, 0, false},
}

type params struct {
	SMA            int               `json:"sma_benchmark"`
	SpreadNSDQ     float64           `json:"spread_rt_bps_nsdq100"`
	SpreadSPX      float64           `json:"spread_rt_bps_spx500"`
	SpreadQQQ      float64           `json:"spread_rt_bps_qqq"`
	SpreadSPY      float64           `json:"spread_rt_bps_spy"`
	OvernightBps   float64           `json:"overnight_bps_per_night"`
	MarkupAnnual   float64           `json:"financing_markup_annual"`
	IRXLast        float64           `json:"irx_last_annual"`
	SizeUSD        float64           `json:"size_usd"`
	ETFFeeBps      float64           `json:"etf_fee_bps_per_order"`
	Sims           int               `json:"sims"`
	Seed           int64             `json:"seed"`
	CostsStatus    string            `json:"costs_status"`
	Variants       []variant         `json:"variants"`
	DataSHA256     map[string]string `json:"data_sha256"`
	PrimaryVariant string            `json:"primary_variant"`
}

type row struct {
	Rule     string  `json:"rule"`
	Return   float64 `json:"return_pct"`
	CAGR     float64 `json:"cagr_pct"`
	Trips    int     `json:"round_trips"`
	Switches int     `json:"switches,omitempty"`
	Exposure float64 `json:"exposure_pct"`
	MaxDD    float64 `json:"max_dd_pct"`
	Sharpe   float64 `json:"sharpe_ann"`
	Trade    float64 `json:"trade_cost_pct"`
	Finance  float64 `json:"finance_pct"`
	CloseOut bool    `json:"margin_close_out,omitempty"`
	// BeatPct is the share of random same-composition rotations the rule
	// beats (primary variant only).
	BeatPct *float64 `json:"random_beat_pct,omitempty"`
}

type window struct {
	Label string `json:"label"`
	Bars  string `json:"bars"`
	From  string `json:"from"`
	To    string `json:"to"`
	Rows  []row  `json:"rows"`
}

type yearRow struct {
	Year     int     `json:"year"`
	RotCFD   float64 `json:"rotation_cfd_return_pct"`
	RotETF   float64 `json:"rotation_etf_return_pct"`
	QQQ      float64 `json:"qqq_hold_return_pct"`
	SPY      float64 `json:"spy_hold_return_pct"`
	SMA50QQQ float64 `json:"sma50_qqq_return_pct"`
	RotETFDD float64 `json:"rotation_etf_maxdd_pct"`
	SPYDD    float64 `json:"spy_hold_maxdd_pct"`
	QQQDD    float64 `json:"qqq_hold_maxdd_pct"`
	BeatPct  float64 `json:"random_beat_pct"`
	H1       bool    `json:"h1_dd_below_spy"`
	H2       bool    `json:"h2_beats_spy"`
	H2b      bool    `json:"h2b_beats_qqq"`
	H3       bool    `json:"h3_beats_95pct_random"`
	H4       bool    `json:"h4_beats_sma50_qqq"`
}

type rolling struct {
	Windows        int     `json:"windows"`
	H1Pct          float64 `json:"h1_etf_rotation_dd_below_spy_pct"`
	H2Pct          float64 `json:"h2_etf_rotation_beats_spy_pct"`
	H2bPct         float64 `json:"h2b_etf_rotation_beats_qqq_pct"`
	H4Pct          float64 `json:"h4_etf_rotation_beats_sma50_qqq_pct"`
	CFDBeatsSPYPct float64 `json:"cfd_rotation_beats_spy_pct"`
	MedianVsSPY    float64 `json:"median_etf_rotation_minus_spy_pp"`
	MedianVsQQQ    float64 `json:"median_etf_rotation_minus_qqq_pp"`
}

type sens struct {
	Label  string  `json:"label"`
	Window string  `json:"window"`
	RotCFD float64 `json:"rotation_cfd_return_pct"`
	RotETF float64 `json:"rotation_etf_return_pct"`
	SPY    float64 `json:"spy_hold_return_pct"`
	QQQ    float64 `json:"qqq_hold_return_pct"`
}

type results struct {
	Params      params    `json:"params"`
	Windows     []window  `json:"windows"`
	Years       []yearRow `json:"calendar_years"`
	Rolling     rolling   `json:"rolling_252d_step21"`
	Sensitivity []sens    `json:"sensitivity"`
}

// data holds the aligned series the study runs on.
type data struct {
	idx    [][]dailyrule.Bar // ^NDX, ^GSPC aligned (CFD proxy, raw closes)
	etfRaw [][]dailyrule.Bar // QQQ, SPY raw closes (signal)
	etfTR  [][]dailyrule.Bar // QQQ, SPY total return (P&L), aligned with etfRaw
	irx    yahoodaily.Series
}

func main() {
	dataDir := flag.String("data", "", "directory of Yahoo CSVs from cmd/index-research -fetch")
	etoroDir := flag.String("etoro", "", "eToro evidence directory with daily_nsdq100.csv / daily_spx500.csv (optional)")
	out := flag.String("out", "", "output directory for rotation-report.txt and rotation-results.json")
	nsdqSpread := flag.Float64("spread-bps-nsdq100", 15.5, "NSDQ100 round-trip spread, bps (frozen)")
	spxSpread := flag.Float64("spread-bps-spx500", 14.8, "SPX500 round-trip spread, bps (frozen)")
	qqqSpread := flag.Float64("spread-bps-qqq", 0.28, "QQQ round-trip spread, bps (in-session quote 2026-10-21)")
	spySpread := flag.Float64("spread-bps-spy", 0.26, "SPY round-trip spread, bps (in-session quote 2026-10-21)")
	overnight := flag.Float64("overnight-bps", 2.3, "overnight fee per calendar night at x1, bps")
	size := flag.Float64("size", 1100, "position size, USD (reporting only)")
	etfFeeBps := flag.Float64("etf-fee-bps", 15, "ETF commission per order, bps of notional (1.65 USD on 1,100 in the 2026-10-21 preview)")
	sims := flag.Int("sims", 2000, "random same-composition rotations per comparison")
	seed := flag.Int64("seed", 1, "random seed")
	status := flag.String("costs-status", "FROZEN for the CFDs (pre-registration 2026-10-12 §3); ETF commission and spreads from the in-session capture of 2026-10-21", "label printed with the cost block")
	primary := flag.String("primary", "dm_12_1", "variant used for base rates, random baseline and sensitivity")
	etoroETF := flag.String("etoro-etf", "", "eToro evidence directory with daily_qqq.csv / daily_spy.csv (for -forward and -state)")
	forward := flag.String("forward", "", "FROM:TO: evaluate the pre-registered hypotheses of the ETF dual-momentum cell on eToro ETF bars (writes forward.json) instead of the study")
	state := flag.Bool("state", false, "print the primary variant's state at the last closed eToro ETF bar (state at registration) and exit")
	flag.Parse()
	if *dataDir == "" || *out == "" {
		log.Fatal("-data and -out are required")
	}
	load := func(s string) []yahoodaily.Row {
		rows, err := yahoodaily.ReadCSV(filepath.Join(*dataDir, fileFor(s)))
		if err != nil {
			log.Fatal(err)
		}
		return rows
	}
	irxRows := load("^IRX")
	irx := yahoodaily.NewSeries(irxRows, func(r yahoodaily.Row) float64 { return r.Close / 100 })
	_, irxLast, _ := irx.Last()
	markup := *overnight*365/1e4 - irxLast

	p := params{SMA: 50, SpreadNSDQ: *nsdqSpread, SpreadSPX: *spxSpread, SpreadQQQ: *qqqSpread, SpreadSPY: *spySpread,
		OvernightBps: *overnight, MarkupAnnual: markup, IRXLast: irxLast, SizeUSD: *size, ETFFeeBps: *etfFeeBps,
		Sims: *sims, Seed: *seed, CostsStatus: *status, Variants: variants, DataSHA256: map[string]string{}, PrimaryVariant: *primary}
	for _, s := range []string{"^NDX", "^GSPC", "QQQ", "SPY", "^IRX"} {
		p.DataSHA256[fileFor(s)] = sha(filepath.Join(*dataDir, fileFor(s)))
	}
	qqq, spy := load("QQQ"), load("SPY")
	d := data{
		idx:    rotation.Align(yahoodaily.Bars(load("^NDX")), yahoodaily.Bars(load("^GSPC"))),
		etfRaw: rotation.Align(yahoodaily.Bars(qqq), yahoodaily.Bars(spy)),
		etfTR:  rotation.Align(yahoodaily.TotalReturnBars(qqq), yahoodaily.TotalReturnBars(spy)),
		irx:    irx,
	}
	if len(d.etfRaw[0]) != len(d.etfTR[0]) {
		log.Fatalf("ETF raw and total-return series differ in length: %d vs %d", len(d.etfRaw[0]), len(d.etfTR[0]))
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	cfdCosts := []cfdsim.Costs{
		{SpreadPerLeg: *nsdqSpread / 2 / 1e4, NightlyRate: cfdsim.RateLinked(irx.At, markup)},
		{SpreadPerLeg: *spxSpread / 2 / 1e4, NightlyRate: cfdsim.RateLinked(irx.At, markup)},
	}
	etfCosts := []cfdsim.Costs{
		{SpreadPerLeg: *qqqSpread / 2 / 1e4, FeePerLeg: *etfFeeBps / 1e4},
		{SpreadPerLeg: *spySpread / 2 / 1e4, FeePerLeg: *etfFeeBps / 1e4},
	}
	rng := rand.New(rand.NewSource(*seed))
	st := study{d: d, p: p, cfd: cfdCosts, etf: etfCosts, rng: rng, primary: findVariant(*primary)}

	if *state || *forward != "" {
		if *etoroETF == "" {
			log.Fatal("-state and -forward need -etoro-etf (a cmd/etoro-spike capture with daily_qqq.csv / daily_spy.csv)")
		}
		if *state {
			if err := runState(*etoroETF, st.primary, p, st.hurdle(st.primary)); err != nil {
				log.Fatal(err)
			}
			return
		}
		if err := runForward(*forward, *etoroETF, *out, st.primary, p, etfCosts, st.hurdle(st.primary), *sims, *seed); err != nil {
			log.Fatal(err)
		}
		return
	}

	f, err := os.Create(filepath.Join(*out, "rotation-report.txt"))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	w := io.MultiWriter(f, os.Stdout)
	res := &results{Params: p}

	fmt.Fprintf(w, "rotation-research  generated %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "rule: dual momentum (shared/pkg/rotation): month-end decision, pick the stronger of {NSDQ100, SPX500} over the lookback, hold only if it beats the hurdle, else cash; fill at next open; x1\n")
	fmt.Fprintf(w, "variants: ")
	for _, v := range variants {
		fmt.Fprintf(w, "%s (L=%d skip=%d tbill=%v)  ", v.Name, v.Lookback, v.Skip, v.TBill)
	}
	fmt.Fprintf(w, "\nprimary variant for base rates: %s\n", *primary)
	fmt.Fprintf(w, "costs [%s]: CFD spread NSDQ100 %.2f / SPX500 %.2f bps RT, financing (^IRX + %.3f%%)/365 per night; ETF %.0f bps per order (%.2f USD on %.0f USD) + QQQ %.2f / SPY %.2f bps RT, no financing\n",
		p.CostsStatus, *nsdqSpread, *spxSpread, markup*100, *etfFeeBps, *etfFeeBps/1e4**size, *size, *qqqSpread, *spySpread)
	fmt.Fprintf(w, "random baseline: %d permutations of the primary variant's monthly targets (same months in each asset, shuffled order), seed %d\n", *sims, *seed)
	fmt.Fprintf(w, "data: index proxies ^NDX/^GSPC %s..%s (%d aligned bars); ETFs QQQ/SPY %s..%s (%d aligned bars)\n",
		ds(d.idx[0][0].Date), ds(d.idx[0][len(d.idx[0])-1].Date), len(d.idx[0]), ds(d.etfRaw[0][0].Date), ds(d.etfRaw[0][len(d.etfRaw[0])-1].Date), len(d.etfRaw[0]))

	last := d.etfRaw[0][len(d.etfRaw[0])-1].Date
	etfStart := d.etfRaw[0][0].Date.AddDate(1, 2, 0) // 12-1 warm-up on the ETF series
	wins := []struct {
		label    string
		from, to time.Time
	}{
		{"since ETF inception", etfStart, last},
		{"2000-2009", dt("2000-07-01"), dt("2009-12-31")},
		{"2010-2019", dt("2010-01-01"), dt("2019-12-31")},
		{"2020-2026", dt("2020-01-01"), last},
		{"last 10y", last.AddDate(-10, 0, 0), last},
		{"last 5y", last.AddDate(-5, 0, 0), last},
		{"last 3y", last.AddDate(-3, 0, 0), last},
	}
	for _, wd := range wins {
		fmt.Fprintf(w, "\n== %s (%s..%s) ==\n", wd.label, ds(wd.from), ds(wd.to))
		header(w)
		res.Windows = append(res.Windows, st.window(w, wd.label, wd.from, wd.to, true))
	}

	// eToro's own CFD bars, if present (short: since 2022-12).
	if *etoroDir != "" {
		eb, err := loadEtoro(*etoroDir)
		if err != nil {
			fmt.Fprintf(w, "\n(eToro bars not loaded: %v)\n", err)
		} else {
			from := eb[0][st.primary.Lookback+st.primary.Skip+1].Date
			to := eb[0][len(eb[0])-1].Date
			fmt.Fprintf(w, "\n== eToro CFD bars (UTC-day buckets, NYSE days), %s..%s, %d aligned bars ==\n", ds(from), ds(to), len(eb[0]))
			header(w)
			win := st.windowOn(w, "eToro bars", eb, eb, cfdCosts, from, to, true, "cfd")
			win.Bars = "etoro"
			res.Windows = append(res.Windows, win)
			same := st.windowOn(io.Discard, "", d.idx, d.idx, cfdCosts, from, to, false, "cfd")
			fmt.Fprintf(w, "   same dates on index bars: %s %.2f%% (bar source effect %.2f pp)\n", st.primary.Name, same.Rows[0].Return, win.Rows[0].Return-same.Rows[0].Return)
		}
	}

	// Calendar years.
	fmt.Fprintf(w, "\n-- calendar years, primary variant %s (ETF product unless noted) --\n", st.primary.Name)
	fmt.Fprintf(w, "%-6s %9s %9s %9s %9s %9s %8s %8s %8s %7s  %-3s %-3s %-4s %-3s %-3s\n", "YEAR", "rotCFD%", "rotETF%", "QQQ%", "SPY%", "sma50Q%", "rotDD%", "SPYDD%", "QQQDD%", "rnd>%", "H1", "H2", "H2b", "H3", "H4")
	for y := etfStart.Year() + 1; y <= last.Year(); y++ {
		yr, ok := st.year(y)
		if !ok {
			continue
		}
		res.Years = append(res.Years, yr)
		rnd, h3 := "n/a", "n/a"
		if yr.BeatPct >= 0 {
			rnd, h3 = fmt.Sprintf("%.1f", yr.BeatPct), yn(yr.H3)
		}
		fmt.Fprintf(w, "%-6d %9.2f %9.2f %9.2f %9.2f %9.2f %8.2f %8.2f %8.2f %7s  %-3s %-3s %-4s %-3s %-3s\n", yr.Year, yr.RotCFD, yr.RotETF, yr.QQQ, yr.SPY, yr.SMA50QQQ,
			yr.RotETFDD, yr.SPYDD, yr.QQQDD, rnd, yn(yr.H1), yn(yr.H2), yn(yr.H2b), h3, yn(yr.H4))
	}
	var n1, n2, n2b, n3, n3n, n4 int
	for _, y := range res.Years {
		n1, n2, n2b, n4 = n1+b2i(y.H1), n2+b2i(y.H2), n2b+b2i(y.H2b), n4+b2i(y.H4)
		if y.BeatPct >= 0 {
			n3n++
			n3 += b2i(y.H3)
		}
	}
	fmt.Fprintf(w, "base rates over %d years: H1 (rotation DD < SPY DD) %d  H2 (> SPY) %d  H2b (> QQQ) %d  H3 (>= 95%% random) %d of %d applicable  H4 (> SMA50 on QQQ) %d\n", len(res.Years), n1, n2, n2b, n3, n3n, n4)

	// Rolling windows.
	res.Rolling = st.rolling(etfStart)
	fmt.Fprintf(w, "\n-- rolling 252-bar windows, step 21, since %s (%d windows), ETF product, primary variant --\n", ds(etfStart), res.Rolling.Windows)
	fmt.Fprintf(w, "rotation DD < SPY DD: %.1f%%   rotation > SPY: %.1f%%   rotation > QQQ: %.1f%%   rotation > SMA50 on QQQ: %.1f%%   CFD rotation > SPY: %.1f%%\n",
		res.Rolling.H1Pct, res.Rolling.H2Pct, res.Rolling.H2bPct, res.Rolling.H4Pct, res.Rolling.CFDBeatsSPYPct)
	fmt.Fprintf(w, "median (rotation - SPY): %.2f pp   median (rotation - QQQ): %.2f pp\n", res.Rolling.MedianVsSPY, res.Rolling.MedianVsQQQ)

	// Sensitivity.
	fmt.Fprintf(w, "\n-- cost sensitivity, primary variant (returns %%) --\n%-36s %-20s %10s %10s %10s %10s\n", "SCENARIO", "WINDOW", "rotCFD", "rotETF", "SPY", "QQQ")
	type sc struct {
		label                  string
		spreadMult, markupMult float64
		noFin                  bool
		etfFeeMult             float64
	}
	scs := []sc{
		{"base", 1, 1, false, 1},
		{"CFD spread x0.5", 0.5, 1, false, 1},
		{"CFD spread x2", 2, 1, false, 1},
		{"financing markup x0.5", 1, 0.5, false, 1},
		{"financing markup x1.5", 1, 1.5, false, 1},
		{"no financing", 1, 0, true, 1},
		{"ETF fee x2 (30 bps)", 1, 1, false, 2},
		{"ETF fee x0.5 (7.5 bps)", 1, 1, false, 0.5},
	}
	for _, wl := range []struct {
		label    string
		from, to time.Time
	}{{"since ETF inception", etfStart, last}, {"last 10y", last.AddDate(-10, 0, 0), last}, {"last 3y", last.AddDate(-3, 0, 0), last}} {
		for _, s := range scs {
			night := cfdsim.RateLinked(irx.At, markup*s.markupMult)
			if s.noFin {
				night = nil
			}
			cc := []cfdsim.Costs{
				{SpreadPerLeg: *nsdqSpread * s.spreadMult / 2 / 1e4, NightlyRate: night},
				{SpreadPerLeg: *spxSpread * s.spreadMult / 2 / 1e4, NightlyRate: night},
			}
			ec := []cfdsim.Costs{
				{SpreadPerLeg: *qqqSpread / 2 / 1e4, FeePerLeg: *etfFeeBps * s.etfFeeMult / 1e4},
				{SpreadPerLeg: *spySpread / 2 / 1e4, FeePerLeg: *etfFeeBps * s.etfFeeMult / 1e4},
			}
			s2 := st
			s2.cfd, s2.etf = cc, ec
			win := s2.window(io.Discard, "", wl.from, wl.to, false)
			r := sens{Label: s.label, Window: wl.label}
			for _, rw := range win.Rows {
				switch rw.Rule {
				case st.primary.Name + "_cfd":
					r.RotCFD = rw.Return
				case st.primary.Name + "_etf":
					r.RotETF = rw.Return
				case "spy_hold":
					r.SPY = rw.Return
				case "qqq_hold":
					r.QQQ = rw.Return
				}
			}
			res.Sensitivity = append(res.Sensitivity, r)
			fmt.Fprintf(w, "%-36s %-20s %10.2f %10.2f %10.2f %10.2f\n", r.Label, r.Window, r.RotCFD, r.RotETF, r.SPY, r.QQQ)
		}
	}

	b, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(filepath.Join(*out, "rotation-results.json"), append(b, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}

type study struct {
	d        data
	p        params
	cfd, etf []cfdsim.Costs
	rng      *rand.Rand
	primary  variant
}

func (s study) hurdle(v variant) func(time.Time) float64 {
	if !v.TBill {
		return nil
	}
	return func(d time.Time) float64 {
		r, ok := s.d.irx.At(d)
		if !ok {
			return 0
		}
		return r * float64(v.Lookback) / 252
	}
}

// targets of a variant on the signal series.
func (s study) targets(v variant, sig [][]dailyrule.Bar) []int {
	return rotation.Targets(sig, rotation.Params{Lookback: v.Lookback, Skip: v.Skip, Hurdle: s.hurdle(v)})
}

// window evaluates every variant on both products plus the benchmarks.
func (s study) window(w io.Writer, label string, from, to time.Time, random bool) window {
	win := s.windowOn(w, label, s.d.idx, s.d.idx, s.cfd, from, to, false, "cfd")
	ew := s.windowOn(w, label, s.d.etfRaw, s.d.etfTR, s.etf, from, to, random, "etf")
	win.Rows = append(win.Rows, ew.Rows...)
	// Benchmarks on the ETF: hold QQQ, hold SPY, SMA50 on each.
	lo, hi := span(s.d.etfTR[0], from, to)
	if hi-lo >= 20 {
		for k, name := range []string{"qqq", "spy"} {
			hold := make([]int, len(s.d.etfTR[k]))
			for i := range hold {
				hold[i] = k
			}
			r := cfdsim.SimulateRotation(s.d.etfTR, hold, lo, hi, s.etf)
			win.Rows = append(win.Rows, toRow(name+"_hold", r, nil))
			tr := dailyrule.Trend(s.d.etfRaw[k], s.p.SMA)
			want := make([]int, len(tr))
			for i, t := range tr {
				want[i] = -1
				if t {
					want[i] = k
				}
			}
			r2 := cfdsim.SimulateRotation(s.d.etfTR, want, lo, hi, s.etf)
			win.Rows = append(win.Rows, toRow("sma50_"+name, r2, nil))
		}
		for _, r := range win.Rows[len(win.Rows)-4:] {
			printRow(w, label, r)
		}
	}
	return win
}

// windowOn runs every variant with signals from sig and P&L on pnl.
func (s study) windowOn(w io.Writer, label string, sig, pnl [][]dailyrule.Bar, costs []cfdsim.Costs, from, to time.Time, random bool, product string) window {
	win := window{Label: label, Bars: "index", From: ds(from), To: ds(to)}
	lo, hi := span(pnl[0], from, to)
	if hi-lo < 20 {
		return win
	}
	for _, v := range variants {
		want := s.targets(v, sig)
		r := cfdsim.SimulateRotation(pnl, want, lo, hi, costs)
		var beat *float64
		if random && v.Name == s.primary.Name && s.p.Sims > 0 {
			if b, ok := s.randomBeat(pnl, want, lo, hi, costs, r.ReturnPct); ok {
				beat = &b
			}
		}
		rw := toRow(v.Name+"_"+product, r, beat)
		win.Rows = append(win.Rows, rw)
		printRow(w, label, rw)
	}
	return win
}

// randomBeat permutes the monthly targets inside [lo, hi]: the rotation
// spends the same number of months in each asset, in a random order. It
// reports ok=false when the window has fewer than three months or every
// month held the same target (nothing to permute: no timing to judge).
func (s study) randomBeat(pnl [][]dailyrule.Bar, want []int, lo, hi int, costs []cfdsim.Costs, ret float64) (float64, bool) {
	bars := pnl[0]
	// Collect decision segments (month-end to month-end) inside the window.
	var segs [][2]int
	start := lo
	for i := lo; i <= hi; i++ {
		if rotation.IsMonthEnd(bars, i) {
			segs = append(segs, [2]int{start, i})
			start = i + 1
		}
	}
	if len(segs) < 3 {
		return 0, false
	}
	// The target that was in force during each segment (decided at the
	// previous month end, i.e. want[seg.start-1]).
	tg := make([]int, len(segs))
	for k, sg := range segs {
		if sg[0] > 0 {
			tg[k] = want[sg[0]-1]
		} else {
			tg[k] = -1
		}
	}
	distinct := map[int]bool{}
	for _, t := range tg {
		distinct[t] = true
	}
	if len(distinct) < 2 {
		return 0, false
	}
	beat := 0
	perm := make([]int, len(tg))
	rw := make([]int, len(want))
	for n := 0; n < s.p.Sims; n++ {
		copy(perm, tg)
		s.rng.Shuffle(len(perm), func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
		for i := range rw {
			rw[i] = -1
		}
		for k, sg := range segs {
			// Target in force during segment k is decided at the bar before it.
			if sg[0] > 0 {
				rw[sg[0]-1] = perm[k]
			}
			for i := sg[0]; i < sg[1]; i++ {
				rw[i] = perm[k]
			}
		}
		r := cfdsim.SimulateRotation(pnl, rw, lo, hi, costs)
		if ret > r.ReturnPct {
			beat++
		}
	}
	return 100 * float64(beat) / float64(s.p.Sims), true
}

func (s study) year(y int) (yearRow, bool) {
	from, to := dt(fmt.Sprintf("%d-01-01", y)), dt(fmt.Sprintf("%d-12-31", y))
	win := s.window(io.Discard, "", from, to, true)
	if len(win.Rows) == 0 {
		return yearRow{}, false
	}
	yr := yearRow{Year: y, BeatPct: -1}
	for _, r := range win.Rows {
		switch r.Rule {
		case s.primary.Name + "_cfd":
			yr.RotCFD = r.Return
		case s.primary.Name + "_etf":
			yr.RotETF, yr.RotETFDD = r.Return, r.MaxDD
			if r.BeatPct != nil {
				yr.BeatPct = *r.BeatPct
			}
		case "qqq_hold":
			yr.QQQ, yr.QQQDD = r.Return, r.MaxDD
		case "spy_hold":
			yr.SPY, yr.SPYDD = r.Return, r.MaxDD
		case "sma50_qqq":
			yr.SMA50QQQ = r.Return
		}
	}
	yr.H1 = yr.RotETFDD < yr.SPYDD
	yr.H2 = yr.RotETF > yr.SPY
	yr.H2b = yr.RotETF > yr.QQQ
	yr.H3 = yr.BeatPct >= 95
	yr.H4 = yr.RotETF > yr.SMA50QQQ
	return yr, true
}

func (s study) rolling(start time.Time) rolling {
	var r rolling
	var dSPY, dQQQ []float64
	bars := s.d.etfTR[0]
	lo0, _ := span(bars, start, bars[len(bars)-1].Date)
	for lo := lo0; lo+251 < len(bars); lo += 21 {
		win := s.window(io.Discard, "", bars[lo].Date, bars[lo+251].Date, false)
		var rot, rotCFD, spy, qqq, sma row
		for _, rw := range win.Rows {
			switch rw.Rule {
			case s.primary.Name + "_etf":
				rot = rw
			case s.primary.Name + "_cfd":
				rotCFD = rw
			case "spy_hold":
				spy = rw
			case "qqq_hold":
				qqq = rw
			case "sma50_qqq":
				sma = rw
			}
		}
		if spy.Rule == "" {
			continue
		}
		r.Windows++
		r.H1Pct += float64(b2i(rot.MaxDD < spy.MaxDD))
		r.H2Pct += float64(b2i(rot.Return > spy.Return))
		r.H2bPct += float64(b2i(rot.Return > qqq.Return))
		r.H4Pct += float64(b2i(rot.Return > sma.Return))
		r.CFDBeatsSPYPct += float64(b2i(rotCFD.Return > spy.Return))
		dSPY = append(dSPY, rot.Return-spy.Return)
		dQQQ = append(dQQQ, rot.Return-qqq.Return)
	}
	if r.Windows > 0 {
		n := float64(r.Windows)
		r.H1Pct *= 100 / n
		r.H2Pct *= 100 / n
		r.H2bPct *= 100 / n
		r.H4Pct *= 100 / n
		r.CFDBeatsSPYPct *= 100 / n
		r.MedianVsSPY, r.MedianVsQQQ = median(dSPY), median(dQQQ)
	}
	return r
}

func loadEtoro(dir string) ([][]dailyrule.Bar, error) {
	var out [][]dailyrule.Bar
	for _, b := range []string{"nsdq100", "spx500"} {
		rows, err := bitsodaily.ReadCSV(filepath.Join(dir, "daily_"+b+".csv"))
		if err != nil {
			return nil, err
		}
		out = append(out, etorodaily.Bars(rows))
	}
	al := rotation.Align(out...)
	if len(al[0]) < 300 {
		return nil, fmt.Errorf("only %d aligned eToro bars", len(al[0]))
	}
	return al, nil
}

func toRow(name string, r cfdsim.RotationResult, beat *float64) row {
	return row{Rule: name, Return: r.ReturnPct, CAGR: r.CAGRPct, Trips: r.RoundTrips, Switches: r.Switches, Exposure: r.ExposurePct,
		MaxDD: r.MaxDDPct, Sharpe: r.SharpeAnn, Trade: r.TradeCostPct, Finance: r.FinancePct, CloseOut: r.MarginCloseOut, BeatPct: beat}
}

func header(w io.Writer) {
	fmt.Fprintf(w, "%-22s %-18s %10s %8s %6s %5s %7s %8s %7s %8s %8s %3s  %s\n", "WINDOW", "RULE", "RETURN%", "CAGR%", "TRIPS", "SW", "EXPOS%", "MAXDD%", "SHARPE", "TRADE%", "FIN%", "CO", "RANDOM")
	fmt.Fprintln(w, strings.Repeat("-", 135))
}

func printRow(w io.Writer, label string, r row) {
	rnd, co := "", ""
	if r.BeatPct != nil {
		rnd = fmt.Sprintf("beats %5.1f%%", *r.BeatPct)
	}
	if r.CloseOut {
		co = "CO"
	}
	fmt.Fprintf(w, "%-22s %-18s %10.2f %8.2f %6d %5d %7.1f %8.2f %7.2f %8.2f %8.2f %3s  %s\n", label, r.Rule, r.Return, r.CAGR, r.Trips, r.Switches, r.Exposure, r.MaxDD, r.Sharpe, r.Trade, r.Finance, co, rnd)
}

func findVariant(name string) variant {
	for _, v := range variants {
		if v.Name == name {
			return v
		}
	}
	log.Fatalf("unknown variant %q", name)
	return variant{}
}

func fileFor(sym string) string {
	return strings.ToLower(strings.TrimPrefix(sym, "^")) + "_yahoo_daily.csv"
}

func sha(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func span(bars []dailyrule.Bar, from, to time.Time) (int, int) {
	lo := sort.Search(len(bars), func(i int) bool { return !bars[i].Date.Before(from) })
	hi := sort.Search(len(bars), func(i int) bool { return bars[i].Date.After(to) }) - 1
	return lo, hi
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func ds(t time.Time) string { return t.Format("2006-01-02") }

func dt(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func yn(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
