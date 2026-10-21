// Command etf-trend-research is the base-rate study for candidate cell #1:
// the frozen SMA50 daily trend rule (shared/pkg/dailyrule) applied to the
// ETFs QQQ and SPY themselves, held at x1 as the real asset on eToro
// (settlement "real", 1.50 USD per order, no financing), against holding
// the same ETF.
//
// It differs from cmd/index-research in one deliberate way: the signal is
// computed on the ETF's OWN raw closes (what eToro's live daily route serves
// for instrument 3006 / 3000), not on the index proxy. P&L uses the
// dividend-adjusted (total-return) bars, as an ETF holder earns. The
// index-signal variant is reported next to it for comparison with the
// 2026-10-10 study. The frozen cfdsim.Simulate is used unchanged.
//
//	go run ./cmd/etf-trend-research -data ../../docs/backtest-readiness/evidence-2026-10-10/data \
//	  -out ../../docs/backtest-readiness/evidence-<date>
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

	"bitso-trading-platform/shared/pkg/cfdsim"
	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/yahoodaily"
)

type etf struct {
	Symbol      string  `json:"symbol"`
	Index       string  `json:"index_proxy"`
	SpreadRTBps float64 `json:"spread_rt_bps"`
}

type params struct {
	SMA        int               `json:"sma"`
	SizeUSD    float64           `json:"size_usd"`
	FeeBps     float64           `json:"fee_bps_per_order"`
	FeeUSD     float64           `json:"fee_usd_per_order_at_size"`
	Sims       int               `json:"sims"`
	Seed       int64             `json:"seed"`
	Status     string            `json:"costs_status"`
	ETFs       []etf             `json:"etfs"`
	DataSHA256 map[string]string `json:"data_sha256"`
}

type row struct {
	Rule   string        `json:"rule"`
	R      cfdsim.Result `json:"result"`
	BeatPc *float64      `json:"random_beat_pct,omitempty"`
}

type window struct {
	Label string `json:"label"`
	From  string `json:"from"`
	To    string `json:"to"`
	Rows  []row  `json:"rows"` // etf_hold, etf_trend_self, etf_trend_index
}

type yearRow struct {
	Year        int     `json:"year"`
	HoldRet     float64 `json:"etf_hold_return_pct"`
	SelfRet     float64 `json:"etf_trend_self_return_pct"`
	IndexRet    float64 `json:"etf_trend_index_return_pct"`
	HoldDD      float64 `json:"etf_hold_maxdd_pct"`
	SelfDD      float64 `json:"etf_trend_self_maxdd_pct"`
	BeatPct     float64 `json:"random_beat_pct"`
	Trips       int     `json:"round_trips"`
	EH1, EH2    bool
	EH3         bool
	SignalAgree float64 `json:"self_vs_index_signal_agreement_pct"`
}

type rolling struct {
	Windows      int     `json:"windows"`
	EH1Pct       float64 `json:"eh1_trend_dd_below_hold_pct"`
	EH2Pct       float64 `json:"eh2_trend_beats_hold_pct"`
	MedianDiffPP float64 `json:"median_trend_minus_hold_pp"`
	MedianDDPP   float64 `json:"median_hold_dd_minus_trend_dd_pp"`
}

type sens struct {
	Label  string  `json:"label"`
	Window string  `json:"window"`
	Hold   float64 `json:"etf_hold_return_pct"`
	Trend  float64 `json:"etf_trend_self_return_pct"`
}

type etfResult struct {
	ETF         etf       `json:"etf"`
	Windows     []window  `json:"windows"`
	Years       []yearRow `json:"calendar_years"`
	Rolling     rolling   `json:"rolling_252d_step21"`
	Sensitivity []sens    `json:"sensitivity"`
}

type results struct {
	Params params      `json:"params"`
	ETFs   []etfResult `json:"etfs"`
}

func main() {
	dataDir := flag.String("data", "", "directory of Yahoo CSVs from cmd/index-research -fetch")
	out := flag.String("out", "", "output directory for etf-report.txt and etf-results.json")
	size := flag.Float64("size", 1100, "position size, USD (reporting only: the commission is proportional)")
	feeBps := flag.Float64("etf-fee-bps", 15, "commission per order, bps of notional (eToro ETF x1 real: 1.50 USD on 1,000 and 1.65 USD on 1,100 in the 2026-10-10 / 2026-10-21 previews = 0.15 %)")
	qqqSpread := flag.Float64("spread-bps-qqq", 0.28, "QQQ round-trip spread, bps (in-session quote 2026-10-21)")
	spySpread := flag.Float64("spread-bps-spy", 0.26, "SPY round-trip spread, bps (in-session quote 2026-10-21)")
	sims := flag.Int("sims", 2000, "random same-trip strategies per comparison")
	seed := flag.Int64("seed", 1, "random seed")
	status := flag.String("costs-status", "in-session capture 2026-10-21 (docs/etoro/evidence-2026-10-21/costs.json): 0.15 % per order, no overnight fee", "label printed with the cost block")
	etoroDir := flag.String("etoro", "", "eToro evidence directory with daily_qqq.csv / daily_spy.csv (for -forward)")
	forward := flag.String("forward", "", "FROM:TO: evaluate the pre-registered hypotheses E-H1..E-H3 on eToro ETF bars in this window (writes forward.json) instead of the study")
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
	p := params{SMA: 50, SizeUSD: *size, FeeBps: *feeBps, FeeUSD: *feeBps / 1e4 * *size, Sims: *sims, Seed: *seed, Status: *status, DataSHA256: map[string]string{},
		ETFs: []etf{{"QQQ", "^NDX", *qqqSpread}, {"SPY", "^GSPC", *spySpread}}}
	for _, s := range []string{"QQQ", "SPY", "^NDX", "^GSPC"} {
		p.DataSHA256[fileFor(s)] = sha(filepath.Join(*dataDir, fileFor(s)))
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	if *forward != "" {
		if err := runForward(*forward, *etoroDir, *out, load, p); err != nil {
			log.Fatal(err)
		}
		return
	}
	f, err := os.Create(filepath.Join(*out, "etf-report.txt"))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	w := io.MultiWriter(f, os.Stdout)
	res := &results{Params: p}
	fmt.Fprintf(w, "etf-trend-research  generated %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "rule: frozen SMA%d (dailyrule.Trend) on the ETF's own raw closes; decide at close t, fill at open t+1; long/flat; x1 real asset\n", p.SMA)
	fmt.Fprintf(w, "costs [%s]: %.0f bps per order (%.2f USD on %.0f USD) + spread (QQQ %.2f / SPY %.2f bps RT); no financing; dividends via adjusted close\n",
		p.Status, *feeBps, p.FeeUSD, *size, *qqqSpread, *spySpread)
	fmt.Fprintf(w, "random baseline: %d same-trip strategies, seed %d (as every earlier pre-registration)\n", *sims, *seed)
	rng := rand.New(rand.NewSource(*seed))
	for _, e := range p.ETFs {
		res.ETFs = append(res.ETFs, run(w, e, load(e.Symbol), load(e.Index), p, rng))
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(filepath.Join(*out, "etf-results.json"), append(b, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}

func run(w io.Writer, e etf, etfRows, idxRows []yahoodaily.Row, p params, rng *rand.Rand) etfResult {
	raw := yahoodaily.Bars(etfRows)
	tr := yahoodaily.TotalReturnBars(etfRows)
	idx := yahoodaily.Bars(idxRows)
	selfWant := dailyrule.Trend(raw, p.SMA)
	idxWant := alignWant(idx, dailyrule.Trend(idx, p.SMA), tr)
	hold := make([]bool, len(tr))
	for i := range hold {
		hold[i] = true
	}
	costs := func(feeBps float64) cfdsim.Costs {
		return cfdsim.Costs{SpreadPerLeg: e.SpreadRTBps / 2 / 1e4, FeePerLeg: feeBps / 1e4}
	}
	base := costs(p.FeeBps)
	out := etfResult{ETF: e}
	last := tr[len(tr)-1].Date
	start := tr[0].Date.AddDate(0, 3, 0)
	fmt.Fprintf(w, "\n%s\n%s (x1 real asset) %s..%s, %d bars; signal on own closes; index-signal variant from %s\n%s\n",
		strings.Repeat("=", 110), e.Symbol, ds(tr[0].Date), ds(last), len(tr), e.Index, strings.Repeat("=", 110))
	wins := []struct {
		label    string
		from, to time.Time
	}{
		{"since inception", start, last},
		{"2000-2009", d("2000-01-01"), d("2009-12-31")},
		{"2010-2019", d("2010-01-01"), d("2019-12-31")},
		{"2020-2026", d("2020-01-01"), last},
		{"last 10y", last.AddDate(-10, 0, 0), last},
		{"last 5y", last.AddDate(-5, 0, 0), last},
		{"last 3y", last.AddDate(-3, 0, 0), last},
		{"last 1y (dry run of the procedure)", last.AddDate(-1, 0, 0), last},
	}
	header(w)
	for _, wd := range wins {
		out.Windows = append(out.Windows, evalWindow(w, wd.label, tr, hold, selfWant, idxWant, wd.from, wd.to, base, rng, p.Sims))
	}

	fmt.Fprintf(w, "\n-- calendar years (base rates for the ETF pre-registration) --\n")
	fmt.Fprintf(w, "%-6s %9s %9s %9s %8s %8s %6s %7s %7s  %-4s %-4s %-4s\n", "YEAR", "hold%", "trend%", "idxsig%", "holdDD%", "trndDD%", "trips", "rnd>%", "agree%", "E-H1", "E-H2", "E-H3")
	for y := tr[0].Date.Year() + 1; y <= last.Year(); y++ {
		from, to := d(fmt.Sprintf("%d-01-01", y)), d(fmt.Sprintf("%d-12-31", y))
		win := evalWindow(io.Discard, "", tr, hold, selfWant, idxWant, from, to, base, rng, p.Sims)
		if len(win.Rows) < 3 {
			continue
		}
		h, t, ix := win.Rows[0].R, win.Rows[1].R, win.Rows[2].R
		yr := yearRow{Year: y, HoldRet: h.ReturnPct, SelfRet: t.ReturnPct, IndexRet: ix.ReturnPct, HoldDD: h.MaxDDPct, SelfDD: t.MaxDDPct, Trips: t.RoundTrips,
			SignalAgree: agreement(tr, selfWant, idxWant, from, to)}
		if win.Rows[1].BeatPc != nil {
			yr.BeatPct = *win.Rows[1].BeatPc
		}
		yr.EH1, yr.EH2, yr.EH3 = t.MaxDDPct < h.MaxDDPct, t.ReturnPct > h.ReturnPct, yr.BeatPct >= 95
		out.Years = append(out.Years, yr)
		fmt.Fprintf(w, "%-6d %9.2f %9.2f %9.2f %8.2f %8.2f %6d %7.1f %7.1f  %-4s %-4s %-4s\n", y, h.ReturnPct, t.ReturnPct, ix.ReturnPct, h.MaxDDPct, t.MaxDDPct, t.RoundTrips, yr.BeatPct, yr.SignalAgree, yn(yr.EH1), yn(yr.EH2), yn(yr.EH3))
	}
	var n1, n2, n3 int
	for _, y := range out.Years {
		n1, n2, n3 = n1+b2i(y.EH1), n2+b2i(y.EH2), n3+b2i(y.EH3)
	}
	fmt.Fprintf(w, "base rates over %d years: E-H1 (trend DD < hold DD) %d  E-H2 (trend > hold) %d  E-H3 (>= 95%% random) %d\n", len(out.Years), n1, n2, n3)

	out.Rolling = rollingStats(tr, hold, selfWant, idxWant, start, base)
	fmt.Fprintf(w, "\n-- rolling 252-bar windows, step 21 (%d windows) --\ntrend DD < hold DD: %.1f%%   trend > hold: %.1f%%   median (trend - hold): %.2f pp   median (hold DD - trend DD): %.2f pp\n",
		out.Rolling.Windows, out.Rolling.EH1Pct, out.Rolling.EH2Pct, out.Rolling.MedianDiffPP, out.Rolling.MedianDDPP)

	fmt.Fprintf(w, "\n-- cost sensitivity (returns %%) --\n%-28s %-18s %10s %10s\n", "SCENARIO", "WINDOW", "hold", "trend")
	for _, wl := range []struct {
		label    string
		from, to time.Time
	}{{"since inception", start, last}, {"last 10y", last.AddDate(-10, 0, 0), last}, {"last 3y", last.AddDate(-3, 0, 0), last}} {
		for _, s := range []struct {
			label string
			c     cfdsim.Costs
		}{
			{"base (15 bps per order)", base},
			{"fee x2 (30 bps)", costs(2 * p.FeeBps)},
			{"fee x0.5 (7.5 bps)", costs(p.FeeBps / 2)},
			{"fixed 1.50 USD on 1,100 (old reading)", costs(1.5 / p.SizeUSD * 1e4)},
			{"no commission", costs(0)},
		} {
			win := evalWindow(io.Discard, "", tr, hold, selfWant, idxWant, wl.from, wl.to, s.c, rng, 0)
			r := sens{Label: s.label, Window: wl.label, Hold: win.Rows[0].R.ReturnPct, Trend: win.Rows[1].R.ReturnPct}
			out.Sensitivity = append(out.Sensitivity, r)
			fmt.Fprintf(w, "%-28s %-18s %10.2f %10.2f\n", r.Label, r.Window, r.Hold, r.Trend)
		}
	}
	return out
}

func header(w io.Writer) {
	fmt.Fprintf(w, "%-36s %-16s %10s %8s %7s %8s %8s %7s %8s  %s\n", "WINDOW", "RULE", "RETURN%", "CAGR%", "TRIPS", "EXPOS%", "MAXDD%", "SHARPE", "TRADE%", "RANDOM")
	fmt.Fprintln(w, strings.Repeat("-", 130))
}

func evalWindow(w io.Writer, label string, bars []dailyrule.Bar, hold, self, idx []bool, from, to time.Time, c cfdsim.Costs, rng *rand.Rand, sims int) window {
	win := window{Label: label, From: ds(from), To: ds(to)}
	lo, hi := span(bars, from, to)
	if hi-lo < 20 {
		return win
	}
	rows := []row{
		{Rule: "etf_hold", R: cfdsim.Simulate(bars, hold, lo, hi, c)},
		{Rule: "etf_trend_self", R: cfdsim.Simulate(bars, self, lo, hi, c)},
		{Rule: "etf_trend_index", R: cfdsim.Simulate(bars, idx, lo, hi, c)},
	}
	if sims > 0 && rows[1].R.RoundTrips > 0 {
		beat := 0
		for s := 0; s < sims; s++ {
			rr := cfdsim.Simulate(bars, randomWant(rng, len(bars), lo, hi, rows[1].R.RoundTrips), lo, hi, c)
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
		fmt.Fprintf(w, "%-36s %-16s %10.2f %8.2f %7d %8.1f %8.2f %7.2f %8.2f  %s\n", label, r.Rule, r.R.ReturnPct, r.R.CAGRPct, r.R.RoundTrips, r.R.ExposurePct, r.R.MaxDDPct, r.R.SharpeAnn, r.R.TradeCostPct, rnd)
	}
	return win
}

func rollingStats(bars []dailyrule.Bar, hold, self, idx []bool, start time.Time, c cfdsim.Costs) rolling {
	var r rolling
	var diff, dd []float64
	lo0, _ := span(bars, start, bars[len(bars)-1].Date)
	for lo := lo0; lo+251 < len(bars); lo += 21 {
		win := evalWindow(io.Discard, "", bars, hold, self, idx, bars[lo].Date, bars[lo+251].Date, c, nil, 0)
		if len(win.Rows) < 3 {
			continue
		}
		h, t := win.Rows[0].R, win.Rows[1].R
		r.Windows++
		r.EH1Pct += float64(b2i(t.MaxDDPct < h.MaxDDPct))
		r.EH2Pct += float64(b2i(t.ReturnPct > h.ReturnPct))
		diff = append(diff, t.ReturnPct-h.ReturnPct)
		dd = append(dd, h.MaxDDPct-t.MaxDDPct)
	}
	if r.Windows > 0 {
		n := float64(r.Windows)
		r.EH1Pct *= 100 / n
		r.EH2Pct *= 100 / n
		r.MedianDiffPP, r.MedianDDPP = median(diff), median(dd)
	}
	return r
}

// agreement is the % of bars in [from, to] where the own-close signal and
// the index signal agree.
func agreement(bars []dailyrule.Bar, a, b []bool, from, to time.Time) float64 {
	lo, hi := span(bars, from, to)
	if hi < lo {
		return 0
	}
	same := 0
	for i := lo; i <= hi; i++ {
		if a[i] == b[i] {
			same++
		}
	}
	return 100 * float64(same) / float64(hi-lo+1)
}

// alignWant maps a signal computed on src bars onto dst bars by date (as
// cmd/index-research does), carrying the last known signal forward.
func alignWant(src []dailyrule.Bar, want []bool, dst []dailyrule.Bar) []bool {
	out := make([]bool, len(dst))
	j, cur := 0, false
	for i, b := range dst {
		for j < len(src) && !src[j].Date.After(b.Date) {
			cur = want[j]
			j++
		}
		out[i] = cur
	}
	return out
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

func ds(t time.Time) string { return t.Format("2006-01-02") }

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
