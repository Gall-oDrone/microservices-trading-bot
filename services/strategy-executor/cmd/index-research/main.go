// Command index-research is the P2 study of the frozen SMA50 daily trend
// rule (shared/pkg/dailyrule) on the eToro index CFDs NSDQ100 and SPX500,
// against holding the same CFD and holding the ETF benchmarks QQQ and SPY.
//
// Data (all research-only):
//   - official index history from Yahoo (^NDX since 1985, ^GSPC since 1970):
//     the long-history proxy for the CFD. On the 2022-12..2026-10 overlap the
//     eToro CFD bars track it (close ratio ~1.0000, daily-return correlation
//     0.96-0.97, SMA50 position agreement 98.8-99.2 %);
//   - QQQ / SPY with dividend-adjusted closes (what an ETF holder earns);
//   - ^IRX (13-week T-bill) as the financing reference rate;
//   - eToro's own daily CFD bars (docs/etoro/evidence-*/daily_*.csv).
//
// Costs: the CFD pays the round-trip spread (half per leg) and overnight
// financing per held calendar night, modelled as (^IRX + markup)/365 with
// the markup calibrated so today's rate equals the measured eToro fee
// (rate-linked), or as the measured fee flat (fixed). The ETF pays a fixed
// commission per order (1.50 USD on the position size) plus its tiny spread,
// and no financing. Simulation: shared/pkg/cfdsim (same fills as dailyrule).
//
//	go run ./cmd/index-research -fetch -data ../../docs/backtest-readiness/evidence-2026-10-10/data \
//	  -etoro ../../docs/etoro/evidence-2026-10-10 -out ../../docs/backtest-readiness/evidence-2026-10-10
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/cfdsim"
	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/etorodaily"
	"bitso-trading-platform/shared/pkg/yahoodaily"
)

type pair struct {
	CFD, Index, ETF string
	SpreadRTBps     float64 // CFD round-trip spread
	ETFSpreadRTBps  float64
}

type params struct {
	SMA          int               `json:"sma"`
	OvernightBps float64           `json:"overnight_bps_per_night"`
	MarkupAnnual float64           `json:"financing_markup_annual"`
	IRXLast      float64           `json:"irx_last_annual"`
	IRXLastDate  string            `json:"irx_last_date"`
	SizeUSD      float64           `json:"size_usd"`
	ETFFeeUSD    float64           `json:"etf_fee_usd_per_order"`
	Sims         int               `json:"sims"`
	Seed         int64             `json:"seed"`
	CostsStatus  string            `json:"costs_status"`
	Pairs        []pair            `json:"pairs"`
	DataSHA256   map[string]string `json:"data_sha256"`
}

func main() {
	dataDir := flag.String("data", "", "directory of Yahoo CSVs (written with -fetch)")
	fetch := flag.Bool("fetch", false, "download ^NDX ^GSPC QQQ SPY ^IRX into -data first")
	etoroDir := flag.String("etoro", "", "eToro evidence directory with daily_nsdq100.csv / daily_spx500.csv")
	out := flag.String("out", "", "output directory for report.txt and results.json")
	nsdqSpread := flag.Float64("spread-bps-nsdq100", 15.5, "NSDQ100 round-trip spread, bps")
	spxSpread := flag.Float64("spread-bps-spx500", 14.8, "SPX500 round-trip spread, bps")
	qqqSpread := flag.Float64("spread-bps-qqq", 0.13, "QQQ round-trip spread, bps")
	spySpread := flag.Float64("spread-bps-spy", 0.26, "SPY round-trip spread, bps")
	overnight := flag.Float64("overnight-bps", 2.3, "eToro overnight fee per night at x1, bps of the position")
	size := flag.Float64("size", 1100, "position size, USD (ETF fixed fee is relative to it)")
	etfFee := flag.Float64("etf-fee", 1.5, "ETF commission per order, USD")
	sims := flag.Int("sims", 2000, "random same-trip strategies per comparison")
	seed := flag.Int64("seed", 1, "random seed")
	status := flag.String("costs-status", "PROVISIONAL (weekend capture 2026-10-10)", "label printed with the cost block")
	forward := flag.String("forward", "", "FROM:TO: evaluate the pre-registered hypotheses on eToro bars in this window (writes forward.json) instead of the study")
	flag.Parse()
	if *dataDir == "" || *out == "" {
		log.Fatal("-data and -out are required")
	}
	syms := []string{"^NDX", "^GSPC", "QQQ", "SPY", "^IRX"}
	if *fetch {
		c := &http.Client{Timeout: 60 * time.Second}
		for _, s := range syms {
			rows, err := yahoodaily.Fetch(context.Background(), c, s, time.Now())
			if err != nil {
				log.Fatalf("fetch %s: %v", s, err)
			}
			if err := yahoodaily.WriteCSV(filepath.Join(*dataDir, fileFor(s)), s, rows); err != nil {
				log.Fatal(err)
			}
			log.Printf("%s: %d rows %s..%s", s, len(rows), rows[0].Date, rows[len(rows)-1].Date)
			time.Sleep(1500 * time.Millisecond)
		}
	}
	load := func(s string) []yahoodaily.Row {
		rows, err := yahoodaily.ReadCSV(filepath.Join(*dataDir, fileFor(s)))
		if err != nil {
			log.Fatal(err)
		}
		return rows
	}
	irx := yahoodaily.NewSeries(load("^IRX"), func(r yahoodaily.Row) float64 { return r.Close / 100 })
	irxDate, irxLast, _ := irx.Last()
	markup := *overnight*365/1e4 - irxLast

	p := params{SMA: 50, OvernightBps: *overnight, MarkupAnnual: markup, IRXLast: irxLast, IRXLastDate: irxDate.Format("2006-01-02"),
		SizeUSD: *size, ETFFeeUSD: *etfFee, Sims: *sims, Seed: *seed, CostsStatus: *status, DataSHA256: map[string]string{},
		Pairs: []pair{
			{CFD: "NSDQ100", Index: "^NDX", ETF: "QQQ", SpreadRTBps: *nsdqSpread, ETFSpreadRTBps: *qqqSpread},
			{CFD: "SPX500", Index: "^GSPC", ETF: "SPY", SpreadRTBps: *spxSpread, ETFSpreadRTBps: *spySpread},
		}}
	for _, s := range syms {
		p.DataSHA256[fileFor(s)] = sha(filepath.Join(*dataDir, fileFor(s)))
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	if *forward != "" {
		if err := runForward(*forward, *etoroDir, *out, load, irx, p); err != nil {
			log.Fatal(err)
		}
		return
	}
	f, err := os.Create(filepath.Join(*out, "report.txt"))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	w := io.MultiWriter(f, os.Stdout)
	res := &results{Params: p}

	fmt.Fprintf(w, "index-research  generated %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "rule: frozen SMA%d (dailyrule.Trend), decide at close t, fill at open t+1, long/flat, x1\n", p.SMA)
	fmt.Fprintf(w, "costs [%s]: CFD spread round trip NSDQ100 %.2f bps, SPX500 %.2f bps; overnight %.2f bps/night\n", p.CostsStatus, *nsdqSpread, *spxSpread, *overnight)
	fmt.Fprintf(w, "  financing rate-linked = (^IRX + %.3f%%)/365 per calendar night (^IRX %.3f%% on %s); fixed = %.2f bps/night\n",
		markup*100, irxLast*100, p.IRXLastDate, *overnight)
	fmt.Fprintf(w, "  ETF: %.2f USD/order on %.0f USD = %.2f bps per leg + spread; no financing; dividends via adjusted close\n", *etfFee, *size, *etfFee / *size * 1e4)
	fmt.Fprintf(w, "  random baseline: %d same-trip strategies, seed %d\n", *sims, *seed)

	for _, pr := range p.Pairs {
		res.Pairs = append(res.Pairs, runPair(w, pr, load(pr.Index), load(pr.ETF), *etoroDir, irx, p))
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(filepath.Join(*out, "results.json"), append(b, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
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

type results struct {
	Params params       `json:"params"`
	Pairs  []pairResult `json:"pairs"`
}

type ruleRow struct {
	Rule   string        `json:"rule"`
	R      cfdsim.Result `json:"result"`
	BeatPc *float64      `json:"random_beat_pct,omitempty"`
}

type window struct {
	Label string    `json:"label"`
	Bars  string    `json:"bars"` // index | etoro
	From  string    `json:"from"`
	To    string    `json:"to"`
	Rows  []ruleRow `json:"rows"`
}

type yearRow struct {
	Year            int     `json:"year"`
	CFDHoldRet      float64 `json:"cfd_hold_return_pct"`
	CFDTrendRet     float64 `json:"cfd_trend_return_pct"`
	ETFHoldRet      float64 `json:"etf_hold_return_pct"`
	ETFTrendRet     float64 `json:"etf_trend_return_pct"`
	CFDHoldDD       float64 `json:"cfd_hold_maxdd_pct"`
	CFDTrendDD      float64 `json:"cfd_trend_maxdd_pct"`
	BeatPct         float64 `json:"cfd_trend_random_beat_pct"`
	H1, H2, H2b, H3 bool
}

type rolling struct {
	Windows              int     `json:"windows"`
	H1DrawdownLowerPct   float64 `json:"h1_trend_dd_lower_pct"`
	H2BeatsCFDHoldPct    float64 `json:"h2_trend_beats_cfd_hold_pct"`
	H2bBeatsETFHoldPct   float64 `json:"h2b_cfd_trend_beats_etf_hold_pct"`
	ETFTrendBeatsETFHold float64 `json:"etf_trend_beats_etf_hold_pct"`
	MedianTrendMinusHold float64 `json:"median_cfd_trend_minus_cfd_hold_pp"`
	MedianTrendMinusETF  float64 `json:"median_cfd_trend_minus_etf_hold_pp"`
}

type sens struct {
	Label  string  `json:"label"`
	Window string  `json:"window"`
	Trend  float64 `json:"cfd_trend_return_pct"`
	Hold   float64 `json:"cfd_hold_return_pct"`
	ETF    float64 `json:"etf_hold_return_pct"`
	ETFTr  float64 `json:"etf_trend_return_pct"`
}

type pairResult struct {
	Pair        pair      `json:"pair"`
	Windows     []window  `json:"windows"`
	Years       []yearRow `json:"calendar_years"`
	Rolling     rolling   `json:"rolling_252d_step21"`
	Sensitivity []sens    `json:"sensitivity"`
}

// costs builds the cost models of a pair.
type costSet struct {
	cfd, etf cfdsim.Costs
}

func makeCosts(pr pair, p params, irx yahoodaily.Series, spreadMult, markupMult float64, fixed bool, size float64) costSet {
	night := cfdsim.RateLinked(irx.At, p.MarkupAnnual*markupMult)
	if fixed {
		night = cfdsim.FixedNightly(p.OvernightBps / 1e4 * markupMult)
	}
	return costSet{
		cfd: cfdsim.Costs{SpreadPerLeg: pr.SpreadRTBps * spreadMult / 2 / 1e4, NightlyRate: night},
		etf: cfdsim.Costs{SpreadPerLeg: pr.ETFSpreadRTBps / 2 / 1e4, FeePerLeg: p.ETFFeeUSD / size},
	}
}

func runPair(w io.Writer, pr pair, idxRows, etfRows []yahoodaily.Row, etoroDir string, irx yahoodaily.Series, p params) pairResult {
	idx := yahoodaily.Bars(idxRows)
	etf := yahoodaily.TotalReturnBars(etfRows)
	idxWant := dailyrule.Trend(idx, p.SMA)
	etfWant := alignWant(idx, idxWant, etf)
	hold := func(n int) []bool {
		h := make([]bool, n)
		for i := range h {
			h[i] = true
		}
		return h
	}
	idxHold, etfHold := hold(len(idx)), hold(len(etf))
	base := makeCosts(pr, p, irx, 1, 1, false, p.SizeUSD)
	rng := rand.New(rand.NewSource(p.Seed))
	out := pairResult{Pair: pr}

	fmt.Fprintf(w, "\n%s\n%s (CFD) <- %s (index proxy, %s..%s, %d bars)   benchmark %s (TR, %s..%s)\n%s\n",
		strings.Repeat("=", 110), pr.CFD, pr.Index, idx[0].Date.Format("2006-01-02"), idx[len(idx)-1].Date.Format("2006-01-02"), len(idx),
		pr.ETF, etf[0].Date.Format("2006-01-02"), etf[len(etf)-1].Date.Format("2006-01-02"), strings.Repeat("=", 110))

	last := idx[len(idx)-1].Date
	etfStart := etf[0].Date.AddDate(0, 3, 0) // after the SMA warm-up on the index
	wins := []struct {
		label    string
		from, to time.Time
	}{
		{"full index history", idx[0].Date.AddDate(0, 4, 0), last},
		{"since ETF inception", etfStart, last},
		{"2000-2009", d("2000-01-01"), d("2009-12-31")},
		{"2010-2019", d("2010-01-01"), d("2019-12-31")},
		{"2020-2026", d("2020-01-01"), last},
		{"last 10y", last.AddDate(-10, 0, 0), last},
		{"last 5y", last.AddDate(-5, 0, 0), last},
		{"last 3y", last.AddDate(-3, 0, 0), last},
	}
	header(w)
	for _, wd := range wins {
		win := evalWindow(w, wd.label, idx, idxWant, idxHold, etf, etfWant, etfHold, wd.from, wd.to, base, rng, p.Sims)
		out.Windows = append(out.Windows, win)
	}

	// eToro's own CFD bars (what the forward test trades).
	if etoroDir != "" {
		path := filepath.Join(etoroDir, "daily_"+strings.ToLower(pr.CFD)+".csv")
		if rows, err := bitsodaily.ReadCSV(path); err == nil && len(rows) > p.SMA+10 {
			eb := etorodaily.Bars(rows)
			ew := dailyrule.Trend(eb, p.SMA)
			etfOnE := alignWant(eb, ew, etf)
			from := eb[p.SMA+10].Date
			fmt.Fprintf(w, "\n-- eToro %s daily bars (UTC-day buckets, NYSE trading days), %s..%s --\n", pr.CFD, from.Format("2006-01-02"), eb[len(eb)-1].Date.Format("2006-01-02"))
			header(w)
			win := evalWindow(w, "eToro bars", eb, ew, hold(len(eb)), etf, etfOnE, etfHold, from, eb[len(eb)-1].Date, base, rng, p.Sims)
			win.Bars = "etoro"
			out.Windows = append(out.Windows, win)
			idxSame := evalWindow(io.Discard, "index bars, same dates", idx, idxWant, idxHold, etf, etfWant, etfHold, from, eb[len(eb)-1].Date, base, rng, 0)
			fmt.Fprintf(w, "   same dates on index bars: cfd_trend %.2f%%  cfd_hold %.2f%%  (bar source effect on the rule: %.2f pp)\n",
				idxSame.Rows[1].R.ReturnPct, idxSame.Rows[0].R.ReturnPct, win.Rows[1].R.ReturnPct-idxSame.Rows[1].R.ReturnPct)
		} else if err != nil {
			fmt.Fprintf(w, "\n(eToro bars not loaded: %v)\n", err)
		}
	}

	// Calendar years since the ETF exists (pre-registration base rates).
	fmt.Fprintf(w, "\n-- calendar years (base rates for the pre-registration; CFD rate-linked financing) --\n")
	fmt.Fprintf(w, "%-6s %9s %9s %9s %9s %8s %8s %8s   %-3s %-3s %-4s %-3s\n", "YEAR", "CFDhold%", "CFDtrnd%", "ETFhold%", "ETFtrnd%", "holdDD%", "trndDD%", "rnd>%", "H1", "H2", "H2b", "H3")
	for y := etf[0].Date.Year() + 1; y <= last.Year(); y++ {
		from, to := d(fmt.Sprintf("%d-01-01", y)), d(fmt.Sprintf("%d-12-31", y))
		win := evalWindow(io.Discard, "", idx, idxWant, idxHold, etf, etfWant, etfHold, from, to, base, rng, p.Sims)
		if len(win.Rows) < 4 {
			continue
		}
		h, t, eh, et := win.Rows[0].R, win.Rows[1].R, win.Rows[2].R, win.Rows[3].R
		yr := yearRow{Year: y, CFDHoldRet: h.ReturnPct, CFDTrendRet: t.ReturnPct, ETFHoldRet: eh.ReturnPct, ETFTrendRet: et.ReturnPct,
			CFDHoldDD: h.MaxDDPct, CFDTrendDD: t.MaxDDPct}
		if win.Rows[1].BeatPc != nil {
			yr.BeatPct = *win.Rows[1].BeatPc
		}
		yr.H1, yr.H2, yr.H2b, yr.H3 = t.MaxDDPct < h.MaxDDPct, t.ReturnPct > h.ReturnPct, t.ReturnPct > eh.ReturnPct, yr.BeatPct >= 95
		out.Years = append(out.Years, yr)
		fmt.Fprintf(w, "%-6d %9.2f %9.2f %9.2f %9.2f %8.2f %8.2f %8.1f   %-3s %-3s %-4s %-3s\n", y, h.ReturnPct, t.ReturnPct, eh.ReturnPct, et.ReturnPct,
			h.MaxDDPct, t.MaxDDPct, yr.BeatPct, yn(yr.H1), yn(yr.H2), yn(yr.H2b), yn(yr.H3))
	}
	var n1, n2, n2b, n3 int
	for _, y := range out.Years {
		n1, n2, n2b, n3 = n1+b2i(y.H1), n2+b2i(y.H2), n2b+b2i(y.H2b), n3+b2i(y.H3)
	}
	fmt.Fprintf(w, "base rates over %d years: H1 %d  H2 %d  H2b %d  H3 %d\n", len(out.Years), n1, n2, n2b, n3)

	// Rolling one-year windows.
	out.Rolling = rollingStats(idx, idxWant, idxHold, etf, etfWant, etfHold, etfStart, base)
	fmt.Fprintf(w, "\n-- rolling 252-bar windows, step 21, since ETF inception (%d windows) --\n", out.Rolling.Windows)
	fmt.Fprintf(w, "trend maxDD < CFD hold maxDD: %.1f%%   trend > CFD hold: %.1f%%   CFD trend > ETF hold: %.1f%%   ETF trend > ETF hold: %.1f%%\n",
		out.Rolling.H1DrawdownLowerPct, out.Rolling.H2BeatsCFDHoldPct, out.Rolling.H2bBeatsETFHoldPct, out.Rolling.ETFTrendBeatsETFHold)
	fmt.Fprintf(w, "median (CFD trend - CFD hold): %.2f pp   median (CFD trend - ETF hold): %.2f pp\n", out.Rolling.MedianTrendMinusHold, out.Rolling.MedianTrendMinusETF)

	// Cost sensitivity.
	fmt.Fprintf(w, "\n-- cost sensitivity (returns %%) --\n%-44s %-20s %10s %10s %10s %10s\n", "SCENARIO", "WINDOW", "CFDtrend", "CFDhold", "ETFhold", "ETFtrend")
	type sc struct {
		label        string
		spread, mark float64
		fixed        bool
		size         float64
	}
	scs := []sc{
		{"base (rate-linked financing)", 1, 1, false, p.SizeUSD},
		{"spread x0.5", 0.5, 1, false, p.SizeUSD},
		{"spread x2", 2, 1, false, p.SizeUSD},
		{"financing markup x0.5", 1, 0.5, false, p.SizeUSD},
		{"financing markup x1.5", 1, 1.5, false, p.SizeUSD},
		{"financing fixed at today's fee", 1, 1, true, p.SizeUSD},
		{"no financing (gross of carry)", 1, 0, true, p.SizeUSD},
		{"ETF size 5,000 USD", 1, 1, false, 5000},
	}
	for _, wl := range []struct {
		label    string
		from, to time.Time
	}{{"since ETF inception", etfStart, last}, {"last 10y", last.AddDate(-10, 0, 0), last}, {"last 3y", last.AddDate(-3, 0, 0), last}} {
		for _, s := range scs {
			c := makeCosts(pr, p, irx, s.spread, s.mark, s.fixed, s.size)
			win := evalWindow(io.Discard, "", idx, idxWant, idxHold, etf, etfWant, etfHold, wl.from, wl.to, c, rng, 0)
			row := sens{Label: s.label, Window: wl.label, Hold: win.Rows[0].R.ReturnPct, Trend: win.Rows[1].R.ReturnPct, ETF: win.Rows[2].R.ReturnPct, ETFTr: win.Rows[3].R.ReturnPct}
			out.Sensitivity = append(out.Sensitivity, row)
			fmt.Fprintf(w, "%-44s %-20s %10.2f %10.2f %10.2f %10.2f\n", s.label, wl.label, row.Trend, row.Hold, row.ETF, row.ETFTr)
		}
	}
	return out
}

func header(w io.Writer) {
	fmt.Fprintf(w, "%-22s %-10s %10s %8s %7s %8s %8s %7s %8s %8s %6s %6s %3s  %s\n", "WINDOW", "RULE", "RETURN%", "CAGR%", "TRIPS", "EXPOS%", "MAXDD%", "SHARPE", "TRADE%", "FIN%", "NIGHTS", "MINEQ", "CO", "RANDOM")
	fmt.Fprintln(w, strings.Repeat("-", 140))
}

// evalWindow runs the four rules on [from, to]. Row order: cfd_hold,
// cfd_trend (on the CFD/index bars), etf_hold, etf_trend (on ETF bars, the
// index signal). The random baseline is computed for cfd_trend.
func evalWindow(w io.Writer, label string, cb []dailyrule.Bar, cw, ch []bool, eb []dailyrule.Bar, ew, eh []bool, from, to time.Time, c costSet, rng *rand.Rand, sims int) window {
	win := window{Label: label, Bars: "index", From: from.Format("2006-01-02"), To: to.Format("2006-01-02")}
	clo, chi := span(cb, from, to)
	elo, ehi := span(eb, from, to)
	if chi-clo < 20 || ehi-elo < 20 {
		return win
	}
	rows := []ruleRow{
		{Rule: "cfd_hold", R: cfdsim.Simulate(cb, ch, clo, chi, c.cfd)},
		{Rule: "cfd_trend", R: cfdsim.Simulate(cb, cw, clo, chi, c.cfd)},
		{Rule: "etf_hold", R: cfdsim.Simulate(eb, eh, elo, ehi, c.etf)},
		{Rule: "etf_trend", R: cfdsim.Simulate(eb, ew, elo, ehi, c.etf)},
	}
	if sims > 0 && rows[1].R.RoundTrips > 0 {
		beat := 0
		for s := 0; s < sims; s++ {
			rr := cfdsim.Simulate(cb, randomWant(rng, len(cb), clo, chi, rows[1].R.RoundTrips), clo, chi, c.cfd)
			if rows[1].R.ReturnPct > rr.ReturnPct {
				beat++
			}
		}
		v := 100 * float64(beat) / float64(sims)
		rows[1].BeatPc = &v
	}
	win.Rows = rows
	for _, r := range rows {
		rnd := ""
		if r.BeatPc != nil {
			rnd = fmt.Sprintf("beats %5.1f%%", *r.BeatPc)
		}
		co := ""
		if r.R.MarginCloseOut {
			co = "CO"
		}
		fmt.Fprintf(w, "%-22s %-10s %10.2f %8.2f %7d %8.1f %8.2f %7.2f %8.2f %8.2f %6d %6.2f %3s  %s\n", label, r.Rule, r.R.ReturnPct, r.R.CAGRPct, r.R.RoundTrips,
			r.R.ExposurePct, r.R.MaxDDPct, r.R.SharpeAnn, r.R.TradeCostPct, r.R.FinancePct, r.R.NightsHeld, r.R.MinEquity, co, rnd)
	}
	return win
}

func rollingStats(cb []dailyrule.Bar, cw, ch []bool, eb []dailyrule.Bar, ew, eh []bool, start time.Time, c costSet) rolling {
	var r rolling
	var dh, de []float64
	lo0, _ := span(cb, start, cb[len(cb)-1].Date)
	for lo := lo0; lo+251 < len(cb); lo += 21 {
		from, to := cb[lo].Date, cb[lo+251].Date
		win := evalWindow(io.Discard, "", cb, cw, ch, eb, ew, eh, from, to, c, nil, 0)
		if len(win.Rows) < 4 {
			continue
		}
		h, t, e, et := win.Rows[0].R, win.Rows[1].R, win.Rows[2].R, win.Rows[3].R
		r.Windows++
		r.H1DrawdownLowerPct += float64(b2i(t.MaxDDPct < h.MaxDDPct))
		r.H2BeatsCFDHoldPct += float64(b2i(t.ReturnPct > h.ReturnPct))
		r.H2bBeatsETFHoldPct += float64(b2i(t.ReturnPct > e.ReturnPct))
		r.ETFTrendBeatsETFHold += float64(b2i(et.ReturnPct > e.ReturnPct))
		dh = append(dh, t.ReturnPct-h.ReturnPct)
		de = append(de, t.ReturnPct-e.ReturnPct)
	}
	if r.Windows > 0 {
		n := float64(r.Windows)
		r.H1DrawdownLowerPct *= 100 / n
		r.H2BeatsCFDHoldPct *= 100 / n
		r.H2bBeatsETFHoldPct *= 100 / n
		r.ETFTrendBeatsETFHold *= 100 / n
		r.MedianTrendMinusHold, r.MedianTrendMinusETF = median(dh), median(de)
	}
	return r
}

// alignWant maps a signal computed on `src` bars onto `dst` bars by date,
// carrying the last known signal forward over dates the source lacks.
func alignWant(src []dailyrule.Bar, want []bool, dst []dailyrule.Bar) []bool {
	out := make([]bool, len(dst))
	j := 0
	cur := false
	for i, b := range dst {
		for j < len(src) && !src[j].Date.After(b.Date) {
			cur = want[j]
			j++
		}
		out[i] = cur
	}
	return out
}

func span(bars []dailyrule.Bar, from, to time.Time) (int, int) {
	lo := sort.Search(len(bars), func(i int) bool { return !bars[i].Date.Before(from) })
	hi := sort.Search(len(bars), func(i int) bool { return bars[i].Date.After(to) }) - 1
	return lo, hi
}

// randomWant is cmd/daily-research's random baseline: exactly `trips`
// long spells at random cut points over the window.
func randomWant(r *rand.Rand, n, lo, hi, trips int) []bool {
	w := make([]bool, n)
	span := hi - lo + 1
	if trips <= 0 || span <= 0 {
		return w
	}
	cuts := r.Perm(span)[:min(2*trips, span)]
	sort.Ints(cuts)
	on, ci := false, 0
	for dd := 0; dd < span; dd++ {
		for ci < len(cuts) && cuts[ci] == dd {
			on = !on
			ci++
		}
		if idx := lo - 1 + dd; idx >= 0 {
			w[idx] = on
		}
	}
	return w
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

func d(s string) time.Time {
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
