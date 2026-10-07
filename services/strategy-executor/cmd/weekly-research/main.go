// weekly-research asks whether strategies that act more often than the frozen
// SMA50 rule beat Bitso's fees on daily bars. It tests, per book:
//
//   - volume-spike events: what happens 1, 3 and 5 days after a day whose
//     volume is k times its 20-day average, split by up and down days;
//   - volume-spike trading rules built on those events;
//   - SMA50 with volume-confirmed entries;
//   - a 20/50/100/200-day trend ensemble (position in 25% steps);
//   - weekly volatility targeting, alone and on top of the trend signals.
//
// History is split into a development window and a holdout (by default the
// last two years, from 2024-10-01). Every variant and parameter below was
// fixed before the holdout was looked at, and the tool prints all of them for
// both windows, so nothing is selected on the holdout.
//
// Execution model (same as cmd/daily-research): a decision taken at day t's
// close fills at day t+1's open; each leg pays -leg-bps (commission plus
// slippage); a position still open at the end is sold at the last close,
// with costs. Long or flat only (Bitso spot). Weights between 0 and 1 drift
// with price between decisions; the account trades only when the target
// weight changes.
//
//	go run ./cmd/weekly-research -book btc_mxn -csv btc_mxn_daily.csv -leg-bps 70
//
// With -json x.json the same numbers are also written as research-run/v1 JSON
// (see report.go), plus x-stress-<bps>bps.json when -stress-leg-bps is set.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/strategy-executor/internal/bitsodaily"
	"bitso-trading-platform/strategy-executor/internal/dailyrule"
)

type day struct {
	date                   time.Time
	open, high, low, close float64
	volume                 float64
}

func main() {
	book := flag.String("book", "btc_mxn", "book label for the report")
	csvPath := flag.String("csv", "", "Bitso daily candles CSV (bitso-daily / daily-executor format)")
	legBPS := flag.Float64("leg-bps", 70, "cost per leg in bps, commission + slippage (pre-registered: btc_mxn 70, btc_usd 40)")
	stressBPS := flag.Float64("stress-leg-bps", 0, "optional second cost level, e.g. taker fees (0 = skip)")
	holdout := flag.String("holdout-start", "2024-10-01", "first day of the holdout window")
	end := flag.String("end", "2026-09-30", "last day included")
	jsonOut := flag.String("json", "", "also write the results as JSON (schema "+ReportSchema+") to this file, plus <name>-stress-<bps>bps.json with -stress-leg-bps; the text output is unchanged")
	flag.Parse()
	if *csvPath == "" {
		fail(fmt.Errorf("-csv is required"))
	}
	d, err := load(*csvPath)
	if err != nil {
		fail(err)
	}
	hs, err := time.Parse("2006-01-02", *holdout)
	if err != nil {
		fail(err)
	}
	ed, err := time.Parse("2006-01-02", *end)
	if err != nil {
		fail(err)
	}

	devLo := firstWarm(d, 200) // every variant has a warm signal from here on
	holdLo := indexOnOrAfter(d, hs)
	hi := indexOnOrBefore(d, ed)
	if devLo < 0 || holdLo <= devLo || hi <= holdLo {
		fail(fmt.Errorf("windows do not fit the data (%s .. %s)", d[0].date.Format("2006-01-02"), d[len(d)-1].date.Format("2006-01-02")))
	}
	windows := []struct {
		name   string
		lo, hi int
	}{
		{"DEVELOPMENT", devLo, holdLo - 1},
		{"HOLDOUT", holdLo, hi},
	}

	fmt.Printf("weekly-research | book %s | %d bars %s .. %s | cost %.0f bps/leg (%.0f bps round trip)\n",
		*book, len(d), d[0].date.Format("2006-01-02"), d[len(d)-1].date.Format("2006-01-02"), *legBPS, 2**legBPS)
	fmt.Println("All variants were fixed before the holdout was examined; every one is printed for both windows.")

	flags := setFlags("csv")
	delete(flags, "json") // where the report went, not how it was produced
	base := newReport(d, *book, *csvPath, *holdout, *end, flags, *legBPS, "base")
	stress := newReport(d, *book, *csvPath, *holdout, *end, flags, *stressBPS, "stress")

	strats := strategies(d)
	for _, w := range windows {
		fmt.Printf("\n================ %s %s .. %s (%d days) ================\n", w.name,
			d[w.lo].date.Format("2006-01-02"), d[w.hi].date.Format("2006-01-02"), w.hi-w.lo+1)
		ev := eventStudy(d, w.lo, w.hi, *legBPS/1e4)
		printEvents(ev, *legBPS/1e4)
		rows := table(d, strats, w.lo, w.hi, *legBPS/1e4)
		printTable(rows, *legBPS/1e4, "base costs")
		bw := windowReport(w.name, d, w.lo, w.hi, rows)
		if *stressBPS > 0 {
			srows := table(d, strats, w.lo, w.hi, *stressBPS/1e4)
			printTable(srows, *stressBPS/1e4, fmt.Sprintf("stress costs %.0f bps/leg", *stressBPS))
			stress.Windows = append(stress.Windows, windowReport(w.name, d, w.lo, w.hi, srows))
		}
		sens := sensitivity(d, w.lo, w.hi, *legBPS/1e4)
		printSensitivity(sens)
		bw.Events = ev
		bw.Sensitivity = &SensitivityRows{PostHoc: true, Chosen: 1.5, Rows: sens}
		base.Windows = append(base.Windows, bw)
	}

	if *jsonOut != "" {
		if err := writeReport(*jsonOut, base); err != nil {
			fail(err)
		}
		if *stressBPS > 0 {
			if err := writeReport(stressPath(*jsonOut, *stressBPS), stress); err != nil {
				fail(err)
			}
		}
	}
}

// newReport fills a report's header for one cost level (windows are added by main).
func newReport(d []day, book, csvPath, holdout, end string, flags map[string]string, legBPS float64, level string) Report {
	return Report{
		Commit: buildCommit(),
		Flags:  flags,
		Data: ReportData{
			Prices: filepath.Base(csvPath), Book: book, Bars: len(d),
			First: d[0].date.Format("2006-01-02"), Last: d[len(d)-1].date.Format("2006-01-02"),
		},
		Costs: ReportCosts{
			BuyBPS: legBPS, SellBPS: legBPS, RoundTripBPS: 2 * legBPS, Level: level,
			Note: "one cost per leg that includes commission and slippage",
		},
		Params: ReportParams{
			SMA: 50, HoldoutStart: holdout, End: end, VolumeRatioDays: 20, VolTarget: 0.50,
		},
		Windows: []WindowReport{},
	}
}

// windowReport converts a window's table rows; vs_hold_pp is against buy-and-hold at the same costs.
func windowReport(label string, d []day, lo, hi int, rows []row) WindowReport {
	hold := 0.0
	for _, x := range rows {
		if x.s.id == holdID {
			hold = x.r.ret
		}
	}
	w := WindowReport{
		Label: label, From: d[lo].date.Format("2006-01-02"), To: d[hi].date.Format("2006-01-02"),
		Bars: hi - lo + 1, Results: []RuleResult{},
	}
	for _, x := range rows {
		w.Results = append(w.Results, ruleResult(x.s, x.r, x.r0, hold))
	}
	return w
}

// sensitivity varies the volume threshold of the volume-confirmed SMA50 entry.
// It was added AFTER the main tables had been seen, so it is a robustness
// check only: k=1.5 stays the pre-declared value whatever this shows.
func sensitivity(d []day, lo, hi int, c float64) []SensitivityRow {
	bars := make([]dailyrule.Bar, len(d))
	for i, x := range d {
		bars[i] = dailyrule.Bar{Date: x.date, Close: x.close}
	}
	trend := dailyrule.Trend(bars, 50)
	vr := volumeRatio(d, 20)
	var out []SensitivityRow
	for _, k := range []float64{0, 1.0, 1.25, 1.5, 1.75, 2.0, 2.5} {
		r := simulate(d, volConfirmed(trend, vr, k), lo, hi, c)
		out = append(out, SensitivityRow{K: k, ReturnPct: 100 * r.ret, MaxDDPct: 100 * r.maxDD, Sharpe: r.sharpe, Trades: r.trades})
	}
	return out
}

func printSensitivity(rows []SensitivityRow) {
	fmt.Printf("\nPOST-HOC SENSITIVITY (added after seeing the tables above; not used to choose k): sma50 entry needs volume >= k x 20-day mean\n")
	fmt.Printf("%-10s %8s %7s %6s %6s\n", "k", "ret %", "maxDD%", "Sharpe", "trades")
	for _, r := range rows {
		label := fmt.Sprintf("%.2f", r.K)
		if r.K == 0 {
			label = "none"
		}
		fmt.Printf("%-10s %8.1f %7.1f %6.2f %6d\n", label, r.ReturnPct, r.MaxDDPct, r.Sharpe, r.Trades)
	}
}

// ---------------------------------------------------------------- signals

type strategy struct {
	id   string    // stable rule id for the JSON report
	name string    // as printed
	w    []float64 // target weight after each day's close, 0..1
}

const holdID = "buy_and_hold"

func strategies(d []day) []strategy {
	bars := make([]dailyrule.Bar, len(d))
	for i, x := range d {
		bars[i] = dailyrule.Bar{Date: x.date, Open: x.open, High: x.high, Low: x.low, Close: x.close}
	}
	trend := map[int][]bool{}
	for _, n := range []int{20, 50, 100, 200} {
		trend[n] = dailyrule.Trend(bars, n)
	}
	vr := volumeRatio(d, 20)
	rv := realizedVol(d, 30)

	n := len(d)
	ones := make([]float64, n)
	sma50 := make([]float64, n)
	ens := make([]float64, n)
	alwaysOn := make([]bool, n)
	for i := range d {
		ones[i] = 1
		alwaysOn[i] = true
		if trend[50][i] {
			sma50[i] = 1
		}
		c := 0
		for _, k := range []int{20, 50, 100, 200} {
			if trend[k][i] {
				c++
			}
		}
		ens[i] = float64(c) / 4
	}
	volTgt := make([]float64, n)
	ensVol := make([]float64, n)
	for i := range d {
		volTgt[i] = 0
		if rv[i] > 0 {
			volTgt[i] = math.Min(1, 0.50/rv[i])
		}
		ensVol[i] = ens[i] * volTgt[i]
	}

	out := []strategy{
		{holdID, "buy-and-hold", ones},
		{"sma50", "sma50 (frozen rule)", sma50},
		{"sma50_volume_1.5x", "sma50, entry needs volume >= 1.5x", volConfirmed(trend[50], vr, 1.5)},
		{"trend_ensemble_daily", "trend ensemble 20/50/100/200, daily", ens},
		{"trend_ensemble_weekly", "trend ensemble, weekly (Sun close)", weekly(d, alwaysOn, ens, 0)},
		{"vol_target_50_weekly", "vol-target 50% of buy-and-hold, weekly", weekly(d, alwaysOn, volTgt, 0.10)},
		{"sma50_x_vol_target_50_weekly", "sma50 x vol-target 50%, weekly size", weekly(d, trend[50], volTgt, 0.10)},
		{"ensemble_x_vol_target_50_weekly", "ensemble x vol-target 50%, weekly", weekly(d, alwaysOn, ensVol, 0.10)},
	}
	for _, k := range []float64{2, 3} {
		for _, h := range []int{1, 5} {
			out = append(out,
				strategy{fmt.Sprintf("volume_spike_%.0fx_up_hold_%dd", k, h), fmt.Sprintf("volume spike %.0fx on UP day, hold %dd", k, h), spikeRule(d, vr, k, h, true)},
				strategy{fmt.Sprintf("volume_spike_%.0fx_down_hold_%dd", k, h), fmt.Sprintf("volume spike %.0fx on DOWN day, hold %dd", k, h), spikeRule(d, vr, k, h, false)})
		}
	}
	return out
}

// volumeRatio is day i's volume over the mean of the previous n days' volume
// (day i excluded). NaN while there is not enough history.
func volumeRatio(d []day, n int) []float64 {
	out := make([]float64, len(d))
	sum := 0.0
	for i := range d {
		out[i] = math.NaN()
		if i >= n && sum > 0 {
			out[i] = d[i].volume / (sum / float64(n))
		}
		sum += d[i].volume
		if i >= n {
			sum -= d[i-n].volume
		}
	}
	return out
}

// realizedVol is the annualized (365-day) standard deviation of the last n
// daily log close-to-close returns, ending at day i. 0 while warming up.
func realizedVol(d []day, n int) []float64 {
	out := make([]float64, len(d))
	for i := n; i < len(d); i++ {
		var s, s2 float64
		for j := i - n + 1; j <= i; j++ {
			r := math.Log(d[j].close / d[j-1].close)
			s += r
			s2 += r * r
		}
		m := s / float64(n)
		v := s2/float64(n) - m*m
		if v > 0 {
			out[i] = math.Sqrt(v * 365)
		}
	}
	return out
}

// volConfirmed is the SMA trend rule, except that a new entry also needs the
// signal day's volume ratio to be at least k. Exits are unchanged.
func volConfirmed(trend []bool, vr []float64, k float64) []float64 {
	out := make([]float64, len(trend))
	held := false
	for i := range trend {
		switch {
		case !held && trend[i] && vr[i] >= k:
			held = true
		case held && !trend[i]:
			held = false
		}
		if held {
			out[i] = 1
		}
	}
	return out
}

// weekly holds size z constant between Sunday closes (Mexico City dates), so
// the account rebalances at Monday's open at most once a week. on[] switches
// the position in or out immediately on any day; a fresh entry takes the
// current z. A size change smaller than band is ignored.
func weekly(d []day, on []bool, z []float64, band float64) []float64 {
	out := make([]float64, len(d))
	cur := 0.0
	for i := range d {
		entering := on[i] && (i == 0 || !on[i-1])
		if d[i].date.Weekday() == time.Sunday || entering || cur == 0 {
			if c := z[i]; entering || cur == 0 || math.Abs(c-cur) >= band {
				cur = c
			}
		}
		if on[i] {
			out[i] = cur
		}
	}
	return out
}

// spikeRule buys at the open after a day whose volume ratio is >= k and whose
// close-to-close return has the given sign, and holds for h days. A new event
// while holding extends the hold.
func spikeRule(d []day, vr []float64, k float64, h int, up bool) []float64 {
	out := make([]float64, len(d))
	until := -1
	for i := 1; i < len(d); i++ {
		r := d[i].close/d[i-1].close - 1
		if vr[i] >= k && (r > 0) == up && r != 0 {
			until = i + h - 1
		}
		if i <= until {
			out[i] = 1
		}
	}
	return out
}

// ---------------------------------------------------------------- simulation

type result struct {
	ret, ret0, cagr, maxDD, sharpe, exposure, cost, turnover float64
	trades                                                   int
	entries                                                  int // buys from a flat position (round trips)
	weeksUp, weeksDown, weeksFlat                            int
	worstWeek, medianWeek                                    float64
}

// simulate trades d[lo..hi]. w[i] is the target weight after day i's close,
// filled at d[i+1].open. The account only trades when the target changes.
func simulate(d []day, w []float64, lo, hi int, c float64) result {
	cash, units := 1.0, 0.0
	var r result
	peak, prevEq := 1.0, 1.0
	var rets []float64
	var weekEnds []float64
	weekKey, weekEq := "", 1.0
	lastTarget := math.NaN()

	for i := lo; i <= hi; i++ {
		target := 0.0
		if i > 0 {
			target = clamp01(w[i-1])
		}
		o := d[i].open
		if target != lastTarget {
			eq := cash + units*o
			delta := target*eq/o - units
			if delta > 0 {
				if max := cash / (o * (1 + c)); delta > max {
					delta = max
				}
				fee := delta * o * c
				cash -= delta*o + fee
				r.cost += fee
			} else if delta < 0 {
				proceeds := -delta * o
				fee := proceeds * c
				cash += proceeds - fee
				r.cost += fee
			}
			if math.Abs(delta*o) > 1e-12 {
				r.trades++
				r.turnover += math.Abs(delta*o) / eq
				if delta > 0 && units*o <= 1e-12 {
					r.entries++
				}
			}
			units += delta
			lastTarget = target
		}
		eq := cash + units*d[i].close
		if eq > peak {
			peak = eq
		}
		if dd := (peak - eq) / peak; dd > r.maxDD {
			r.maxDD = dd
		}
		rets = append(rets, eq/prevEq-1)
		prevEq = eq
		r.exposure += units * d[i].close / eq

		y, wk := d[i].date.ISOWeek()
		if k := fmt.Sprintf("%d-%02d", y, wk); k != weekKey {
			if weekKey != "" {
				weekEnds = append(weekEnds, weekEq)
			}
			weekKey = k
		}
		weekEq = eq
	}
	if units > 0 { // close out at the last close, with costs
		gross := units * d[hi].close
		fee := gross * c
		cash += gross - fee
		r.cost += fee
		units = 0
	}
	weekEnds = append(weekEnds, cash)

	days := float64(hi - lo + 1)
	r.ret = cash - 1
	years := d[hi].date.Sub(d[lo].date).Hours()/24/365 + 1.0/365
	r.cagr = math.Pow(cash, 1/years) - 1
	r.exposure /= days
	r.sharpe = sharpe(rets)

	var wr []float64
	prev := 1.0
	for _, e := range weekEnds {
		x := e/prev - 1
		prev = e
		wr = append(wr, x)
		switch {
		case x > 1e-9:
			r.weeksUp++
		case x < -1e-9:
			r.weeksDown++
		default:
			r.weeksFlat++
		}
	}
	sort.Float64s(wr)
	r.worstWeek = wr[0]
	r.medianWeek = wr[len(wr)/2]
	return r
}

func sharpe(rets []float64) float64 {
	if len(rets) < 2 {
		return 0
	}
	var s, s2 float64
	for _, x := range rets {
		s += x
		s2 += x * x
	}
	n := float64(len(rets))
	m := s / n
	v := (s2 - n*m*m) / (n - 1)
	if v <= 0 {
		return 0
	}
	return m / math.Sqrt(v) * math.Sqrt(365)
}

// row is one strategy's result at a cost level, with the same trades at zero cost.
type row struct {
	s     strategy
	r, r0 result
}

func table(d []day, strats []strategy, lo, hi int, c float64) []row {
	out := make([]row, 0, len(strats))
	for _, s := range strats {
		out = append(out, row{s, simulate(d, s.w, lo, hi, c), simulate(d, s.w, lo, hi, 0)})
	}
	return out
}

func printTable(rows []row, c float64, label string) {
	fmt.Printf("\nSTRATEGIES (%s, %.0f bps/leg; 'ret 0' = same trades with zero costs)\n", label, c*1e4)
	fmt.Printf("%-42s %8s %8s %7s %7s %6s %5s %6s %7s %6s %14s %7s %7s\n",
		"strategy", "ret %", "ret 0 %", "CAGR %", "maxDD%", "Sharpe", "expo", "trades", "turn x", "cost%", "weeks +/-/0", "med wk", "worst")
	for _, x := range rows {
		s, r, r0 := x.s, x.r, x.r0
		fmt.Printf("%-42s %8.1f %8.1f %7.1f %7.1f %6.2f %4.0f%% %6d %7.1f %6.1f %14s %6.2f%% %6.1f%%\n",
			s.name, 100*r.ret, 100*r0.ret, 100*r.cagr, 100*r.maxDD, r.sharpe, 100*r.exposure, r.trades, r.turnover,
			100*r.cost, fmt.Sprintf("%d/%d/%d", r.weeksUp, r.weeksDown, r.weeksFlat), 100*r.medianWeek, 100*r.worstWeek)
	}
}

// ---------------------------------------------------------------- event study

// eventStudy measures forward returns after volume-spike days, entering at the
// next open and exiting at the close h days after the event, against the
// same measurement taken after every day in the window.
func eventStudy(d []day, lo, hi int, c float64) []EventRow {
	vr := volumeRatio(d, 20)
	var out []EventRow
	for _, h := range []int{1, 3, 5} {
		type grp struct {
			name string
			keep func(i int, r float64) bool
		}
		groups := []grp{
			{"all days (baseline)", func(int, float64) bool { return true }},
			{"volume >= 2x, UP day", func(i int, r float64) bool { return vr[i] >= 2 && r > 0 }},
			{"volume >= 2x, DOWN day", func(i int, r float64) bool { return vr[i] >= 2 && r < 0 }},
			{"volume >= 3x, UP day", func(i int, r float64) bool { return vr[i] >= 3 && r > 0 }},
			{"volume >= 3x, DOWN day", func(i int, r float64) bool { return vr[i] >= 3 && r < 0 }},
		}
		for _, g := range groups {
			var xs []float64
			for i := lo; i+h <= hi && i+1 <= hi; i++ {
				if i == 0 {
					continue
				}
				r := d[i].close/d[i-1].close - 1
				if !g.keep(i, r) {
					continue
				}
				xs = append(xs, d[i+h].close/d[i+1].open-1)
			}
			m, med, hit, t := stats(xs)
			out = append(out, EventRow{
				Condition: g.name, H: h, N: len(xs), MeanPct: 100 * m, MedianPct: 100 * med,
				HitPct: 100 * hit, T: t, MeanAfterCostsPct: 100 * (m - 2*c),
			})
		}
	}
	return out
}

func printEvents(rows []EventRow, c float64) {
	fmt.Printf("\nVOLUME-SPIKE EVENT STUDY (enter next open, exit close h days after the event; round-trip cost %.2f%%)\n", 200*c)
	fmt.Printf("%-28s %3s %6s %8s %8s %6s %7s %10s\n", "condition", "h", "n", "mean %", "median%", "hit %", "t*", "mean-cost%")
	for _, e := range rows {
		fmt.Printf("%-28s %3d %6d %8.2f %8.2f %6.1f %7.2f %10.2f\n", e.Condition, e.H, e.N, e.MeanPct, e.MedianPct, e.HitPct, e.T, e.MeanAfterCostsPct)
	}
	fmt.Println("t* = mean / (sd / sqrt(n)); overlapping windows for h > 1 make it optimistic.")
}

func stats(xs []float64) (mean, median, hit, t float64) {
	n := float64(len(xs))
	if n == 0 {
		return
	}
	var s, s2 float64
	pos := 0
	for _, x := range xs {
		s += x
		s2 += x * x
		if x > 0 {
			pos++
		}
	}
	mean = s / n
	ys := append([]float64(nil), xs...)
	sort.Float64s(ys)
	median = ys[len(ys)/2]
	hit = float64(pos) / n
	if n > 1 {
		if v := (s2 - n*mean*mean) / (n - 1); v > 0 {
			t = mean / math.Sqrt(v/n)
		}
	}
	return
}

// ---------------------------------------------------------------- data

func load(path string) ([]day, error) {
	rows, err := bitsodaily.ReadCSV(path)
	if err != nil {
		return nil, err
	}
	out := make([]day, 0, len(rows))
	for _, r := range rows {
		t, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			return nil, fmt.Errorf("date %q: %w", r.Date, err)
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(r.Volume), 64)
		if err != nil {
			return nil, fmt.Errorf("volume %q on %s: %w", r.Volume, r.Date, err)
		}
		out = append(out, day{date: t, open: r.Open, high: r.High, low: r.Low, close: r.Close, volume: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].date.Before(out[j].date) })
	return out, nil
}

// firstWarm is the first index at which the slowest signal (SMA n) is warm.
func firstWarm(d []day, n int) int {
	bars := make([]dailyrule.Bar, len(d))
	for i, x := range d {
		bars[i] = dailyrule.Bar{Date: x.date, Close: x.close}
	}
	for i, p := range dailyrule.Evaluate(bars, n) {
		if p.Warm {
			return i + 1 // first fill driven by a warm decision
		}
	}
	return -1
}

func indexOnOrAfter(d []day, t time.Time) int {
	for i, x := range d {
		if !x.date.Before(t) {
			return i
		}
	}
	return -1
}

func indexOnOrBefore(d []day, t time.Time) int {
	for i := len(d) - 1; i >= 0; i-- {
		if !d[i].date.After(t) {
			return i
		}
	}
	return -1
}

func clamp01(x float64) float64 {
	if math.IsNaN(x) || x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "weekly-research:", err)
	os.Exit(1)
}
