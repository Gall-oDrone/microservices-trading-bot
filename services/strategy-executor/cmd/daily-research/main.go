// Command daily-research asks one question of the Yahoo Finance BTC-USD daily
// bars and LLM-scored news: at a 24-hour horizon, and after Bitso's real
// costs, does a simple rule beat simply holding?
//
// It exists because the trade archive is too short (34 days) and too
// fine-grained (1-minute bars, where cost is ~50x the median move) to answer
// that. Daily bars are where fees become payable at all, and there is a year
// of them.
//
// Mechanics, chosen to avoid the classic ways a daily backtest flatters itself:
//
//   - A decision for day t uses only data known at t's close: that day's
//     OHLC, and news whose UTC timestamp falls on or before t. It is executed
//     at day t+1's OPEN. Nothing ever trades at the close that generated it.
//   - Long-only, all-in / all-out: Bitso spot has no shorting.
//   - Every fill pays commission on its own notional plus slippage, per leg.
//     Defaults are Bitso's confirmed production btc_mxn taker rate.
//   - An open position is closed at the final close, with costs, so exposure
//     at the end is not silently dropped.
//   - Parameters are fixed up front (flags with conventional defaults), NOT
//     tuned on the sample. With ~365 bars, tuning would fit noise.
//   - Each rule is compared against 2,000 random strategies that make the
//     same number of round trips, so "beat buy-and-hold" can be separated
//     from "got lucky".
//
// Caveat carried into every result: this is BTC-USD on global venues. It is
// evidence about the SIGNAL. Costs are modelled as Bitso's, but fills on
// btc_mxn would differ.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/strategy-executor/internal/dailyrule"
	"bitso-trading-platform/strategy-executor/internal/yahoo"
)

// bar is the shared daily bar type, so bars flow into dailyrule unchanged.
type bar = dailyrule.Bar

// newsDay aggregates the scored BTC news published on one UTC day.
type newsDay struct {
	N       int
	SentSum float64 // sum of overall_sentiment x confidence
	Bullish int
	Bearish int
}

func (d newsDay) score() float64 {
	if d.N == 0 {
		return 0
	}
	return d.SentSum / float64(d.N)
}

func main() {
	pricesDir := flag.String("prices", "", "btc-usd daily CSV partitions dir (month=/day= files), or the compacted yahoo_crypto_daily.parquet")
	newsDir := flag.String("news", "", "transformed news daily CSV partitions dir, or the compacted news_crypto_agentic.parquet")
	book := flag.String("book", "btc-usd", "with a .parquet -prices file: the book to load")
	tickers := flag.String("tickers", "BTC,BTC-USD", "news llm_ticker values to keep")
	windows := flag.String("windows", "2025-01-01:2025-12-31,2026-08-01:2026-12-31", "comma-separated from:to windows, each run independently (first = in-sample)")
	buyBPS := flag.Float64("buy-bps", 78, "buy-leg commission bps (Bitso btc_mxn taker, production)")
	sellBPS := flag.Float64("sell-bps", 78, "sell-leg commission bps")
	slipBPS := flag.Float64("slippage-bps", 10, "slippage bps per leg")
	smaN := flag.Int("sma", 50, "trend rule: long while close > SMA(n)")
	newsK := flag.Int("news-window", 3, "news rule: trailing days of news to average")
	newsThr := flag.Float64("news-threshold", 0, "news rule: long while trailing score > threshold")
	sims := flag.Int("sims", 2000, "random same-trade-count strategies per rule")
	seed := flag.Int64("seed", 1, "RNG seed for the random baseline")
	jsonOut := flag.String("json", "", "also write the results as JSON (schema "+ReportSchema+") to this file; the text output is unchanged")
	flag.Parse()

	if *pricesDir == "" {
		fmt.Fprintln(os.Stderr, "daily-research: -prices is required")
		os.Exit(2)
	}
	bars, err := loadBars(*pricesDir, *book)
	if err != nil {
		fail(err)
	}
	news := map[string]newsDay{}
	if *newsDir != "" {
		keep := map[string]bool{}
		for _, t := range strings.Split(*tickers, ",") {
			keep[strings.TrimSpace(t)] = true
		}
		news, err = loadNews(*newsDir, keep)
		if err != nil {
			fail(err)
		}
	}
	costs := costModel{buy: (*buyBPS + *slipBPS) / 1e4, sell: (*sellBPS + *slipBPS) / 1e4}

	fmt.Printf("DATA   : %d daily bars %s .. %s; %d news days\n", len(bars),
		bars[0].Date.Format("2006-01-02"), bars[len(bars)-1].Date.Format("2006-01-02"), len(news))
	fmt.Printf("COSTS  : buy %.0f + sell %.0f bps commission, %.0f bps slippage per leg -> %.0f bps round trip\n",
		*buyBPS, *sellBPS, *slipBPS, *buyBPS+*sellBPS+2**slipBPS)
	fmt.Printf("RULES  : trend = close > SMA(%d); news = trailing %d-day score > %.2f; fixed a priori, not tuned\n",
		*smaN, *newsK, *newsThr)
	fmt.Printf("TIMING : decide at day t close, fill at day t+1 open; long-only\n")

	rep := Report{
		Commit: buildCommit(),
		Flags:  setFlags("prices", "news"),
		Data: ReportData{
			Prices: filepath.Base(*pricesDir), Bars: len(bars),
			First: bars[0].Date.Format("2006-01-02"), Last: bars[len(bars)-1].Date.Format("2006-01-02"),
			NewsDays: len(news),
		},
		Costs:  ReportCosts{BuyBPS: *buyBPS, SellBPS: *sellBPS, SlippageBPS: *slipBPS, RoundTripBPS: *buyBPS + *sellBPS + 2**slipBPS},
		Params: ReportParams{SMA: *smaN, NewsWindow: *newsK, NewsThreshold: *newsThr, Sims: *sims, Seed: *seed},
	}
	if strings.HasSuffix(*pricesDir, ".parquet") {
		rep.Data.Book = *book
	}
	if *newsDir != "" {
		rep.Data.News = filepath.Base(*newsDir)
	}
	delete(rep.Flags, "json") // where the report went, not how it was produced

	for wi, w := range strings.Split(*windows, ",") {
		from, to, err := parseWindow(w)
		if err != nil {
			fail(err)
		}
		// Indicators are computed over the full history, so the trend rule is
		// already warm at the window's start when enough contiguous history
		// precedes it (see signalTrend on gaps). Only trading is restricted to
		// the window.
		label := "IN-SAMPLE"
		if wi > 0 {
			label = "OUT-OF-SAMPLE"
		}
		rep.Windows = append(rep.Windows, runWindow(os.Stdout, label, bars, news, from, to, costs, *smaN, *newsK, *newsThr, *sims, *seed))
	}
	if *jsonOut != "" {
		if err := writeReport(*jsonOut, rep); err != nil {
			fail(err)
		}
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "daily-research: %v\n", err)
	os.Exit(1)
}

// ---------------------------------------------------------------------------
// Signals: each returns want[i] = desired position AFTER day i's close.
// ---------------------------------------------------------------------------

func signalBuyAndHold(n int) []bool {
	w := make([]bool, n)
	for i := range w {
		w[i] = true
	}
	return w
}

// signalTrend is the frozen trend rule. It lives in internal/dailyrule so that
// cmd/daily-executor computes exactly the same signal. The SMA warm-up
// restarts after a gap longer than dailyrule.MaxGapDays: short holes (a few
// missing days) are tolerated; the multi-month hole in the 2026 data is not,
// because an SMA spanning it would blend prices from different market regimes
// into one number.
func signalTrend(bars []bar, n int) []bool {
	return dailyrule.Trend(bars, n)
}

func signalNews(bars []bar, news map[string]newsDay, k int, thr float64) []bool {
	w := make([]bool, len(bars))
	for i, b := range bars {
		var sum float64
		var n int
		for d := 0; d < k; d++ {
			nd, ok := news[b.Date.AddDate(0, 0, -d).Format("2006-01-02")]
			if ok && nd.N > 0 {
				sum += nd.score()
				n++
			}
		}
		w[i] = n > 0 && sum/float64(n) > thr
	}
	return w
}

func and(a, b []bool) []bool {
	w := make([]bool, len(a))
	for i := range a {
		w[i] = a[i] && b[i]
	}
	return w
}

// ---------------------------------------------------------------------------
// Simulator
// ---------------------------------------------------------------------------

type costModel struct{ buy, sell float64 } // per-leg decimal, commission + slippage

type result = dailyrule.Result

// simulate trades bars[lo..hi] (inclusive). want[i] is the position desired
// after day i's close; it is filled at bars[i+1].Open. So the first possible
// fill is at bars[lo].Open, driven by want[lo-1]; when lo == 0 there is no
// prior close to decide on, and the first fill is at bars[1].Open.
//
// The loop moved verbatim to shared/pkg/dailyrule (Simulate) on 2026-10-08
// so services/backtesting runs the same simulator (plan §7 item 5); the
// golden test pins its output bit for bit.
func simulate(bars []bar, want []bool, lo, hi int, c costModel) result {
	return dailyrule.Simulate(bars, want, lo, hi, dailyrule.Costs{Buy: c.buy, Sell: c.sell})
}

// randomWant builds a position series over [lo-1, hi-1] with exactly `trips`
// entries at random days, each held for a random length, so the random
// strategy trades as often as the rule it is compared with.
func randomWant(r *rand.Rand, n, lo, hi, trips int) []bool {
	w := make([]bool, n)
	if trips <= 0 {
		return w
	}
	span := hi - lo + 1
	// Choose 2*trips distinct cut points; alternate flat/long between them.
	cuts := r.Perm(span)[:min(2*trips, span)]
	sort.Ints(cuts)
	on := false
	ci := 0
	for d := 0; d < span; d++ {
		for ci < len(cuts) && cuts[ci] == d {
			on = !on
			ci++
		}
		if idx := lo - 1 + d; idx >= 0 {
			w[idx] = on
		}
	}
	return w
}

// runWindow prints one window's table to w and returns the same numbers, unrounded.
func runWindow(w io.Writer, label string, bars []bar, news map[string]newsDay, from, to time.Time, c costModel, smaN, newsK int, newsThr float64, sims int, seed int64) WindowReport {
	lo := sort.Search(len(bars), func(i int) bool { return !bars[i].Date.Before(from) })
	hi := sort.Search(len(bars), func(i int) bool { return bars[i].Date.After(to) }) - 1
	wr := WindowReport{Label: label, From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Bars: max(hi-lo+1, 0), Results: []RuleResult{}}
	fmt.Fprintf(w, "\n%s\n%s  %s .. %s  (%d bars)\n", strings.Repeat("=", 104), label, from.Format("2006-01-02"), to.Format("2006-01-02"), hi-lo+1)
	if hi-lo+1 < 2 {
		fmt.Fprintln(w, "  not enough bars in window")
		wr.Note = "not enough bars in window"
		return wr
	}
	if gaps := calendarGaps(bars[lo : hi+1]); gaps != "" {
		fmt.Fprintf(w, "  WARNING: missing days inside window: %s\n", gaps)
		wr.Gaps = gaps
	}
	fmt.Fprintln(w, strings.Repeat("=", 104))

	trend := signalTrend(bars, smaN)
	newsSig := signalNews(bars, news, newsK, newsThr)
	rules := []struct {
		name string
		want []bool
	}{
		{"buy_and_hold", signalBuyAndHold(len(bars))},
		{fmt.Sprintf("trend_sma%d", smaN), trend},
		{"news_sentiment", newsSig},
		{"trend_and_news", and(trend, newsSig)},
	}

	// buy_and_hold enters on the window's first open, so seed the decision
	// the day before.
	fmt.Fprintf(w, "%-18s %10s %8s %10s %10s %10s %12s   %s\n", "RULE", "RETURN%", "TRIPS", "EXPOSURE%", "MAX DD%", "COSTS%", "vs B&H (pp)", "RANDOM, SAME TRIPS")
	fmt.Fprintln(w, strings.Repeat("-", 104))
	var bh result
	r := newRand(seed)
	for i, rule := range rules {
		res := simulate(bars, rule.want, lo, hi, c)
		if i == 0 {
			bh = res
		}
		row := RuleResult{
			Rule: rule.name, ReturnPct: res.ReturnPct, RoundTrips: res.RoundTrips, ExposurePct: res.ExposurePct,
			MaxDDPct: res.MaxDDPct, CostPct: res.CostPct, VsHoldPP: res.ReturnPct - bh.ReturnPct,
		}
		rnd := ""
		if i > 0 && sims > 0 && res.RoundTrips > 0 {
			beat := 0
			for s := 0; s < sims; s++ {
				rr := simulate(bars, randomWant(r, len(bars), lo, hi, res.RoundTrips), lo, hi, c)
				if res.ReturnPct > rr.ReturnPct {
					beat++
				}
			}
			row.Random = &RandomBaseline{Sims: sims, BeatPct: 100 * float64(beat) / float64(sims)}
			rnd = fmt.Sprintf("beats %5.1f%% of %d", row.Random.BeatPct, sims)
		}
		fmt.Fprintf(w, "%-18s %10.2f %8d %10.1f %10.2f %10.2f %12.2f   %s\n",
			rule.name, res.ReturnPct, res.RoundTrips, res.ExposurePct, res.MaxDDPct, res.CostPct, row.VsHoldPP, rnd)
		wr.Results = append(wr.Results, row)
	}
	return wr
}

func calendarGaps(bs []bar) string {
	var gaps []string
	for i := 1; i < len(bs); i++ {
		if d := int(bs[i].Date.Sub(bs[i-1].Date).Hours() / 24); d > 1 {
			gaps = append(gaps, fmt.Sprintf("%s..%s (%d days)", bs[i-1].Date.AddDate(0, 0, 1).Format("01-02"), bs[i].Date.AddDate(0, 0, -1).Format("01-02"), d-1))
		}
	}
	return strings.Join(gaps, ", ")
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

func loadBars(dir, book string) ([]bar, error) {
	if strings.HasSuffix(dir, ".parquet") {
		return loadBarsParquet(dir, book)
	}
	byDate := map[string]bar{}
	err := walkCSV(dir, func(path string, header []string, row []string) error {
		get := fieldGetter(header, row)
		d, err := time.Parse("2006-01-02", get("date"))
		if err != nil {
			return nil
		}
		b := bar{Date: d}
		var perr error
		parse := func(k string) float64 {
			v, err := strconv.ParseFloat(get(k), 64)
			if err != nil && perr == nil {
				perr = fmt.Errorf("%s: %s=%q: %w", path, k, get(k), err)
			}
			return v
		}
		b.Open, b.High, b.Low, b.Close = parse("open"), parse("high"), parse("low"), parse("close")
		if perr != nil {
			return perr
		}
		byDate[get("date")] = b // daily partitions only; a date seen twice is the same bar
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]bar, 0, len(byDate))
	for _, b := range byDate {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	if len(out) == 0 {
		return nil, fmt.Errorf("no bars under %s", dir)
	}
	return out, nil
}

// loadNews reads scored news from either the compacted Parquet file or a
// local copy of the CSV partitions. Both paths apply the same rule: one row
// per canonical article URL (yahoo.DedupNews). De-duplicating on the id
// column is WRONG -- ids are shared by distinct articles, and doing so
// silently discarded ~3,100 real articles in the first 2026-09-26 run.
func loadNews(dir string, keep map[string]bool) (map[string]newsDay, error) {
	if strings.HasSuffix(dir, ".parquet") {
		return loadNewsParquet(dir, keep)
	}
	cands, _, err := yahoo.ReadNews(dir, "")
	if err != nil {
		return nil, err
	}
	rows, _ := yahoo.DedupNews(cands)
	return aggregateNews(rows, keep), nil
}

// loadBarsParquet reads one book from the compacted prices file written by
// cmd/yahoo-compact. That file is already deduplicated on (book, date).
func loadBarsParquet(path, book string) ([]bar, error) {
	rows, _, err := yahoo.ReadPricesFile(path)
	if err != nil {
		return nil, err
	}
	var out []bar
	for _, r := range rows {
		if r.Book == book {
			out = append(out, bar{Date: r.Day(), Open: r.Open, High: r.High, Low: r.Low, Close: r.Close})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	if len(out) == 0 {
		return nil, fmt.Errorf("no %s bars in %s", book, path)
	}
	return out, nil
}

// loadNewsParquet aggregates the compacted news file written by
// cmd/yahoo-compact. That file is already deduplicated on the canonical
// article URL, so no id-based de-duplication is applied here (the id column
// is shared by distinct articles and would drop real rows).
func loadNewsParquet(path string, keep map[string]bool) (map[string]newsDay, error) {
	rows, _, err := yahoo.ReadNewsFile(path)
	if err != nil {
		return nil, err
	}
	return aggregateNews(rows, keep), nil
}

// aggregateNews sums scored, ticker-matching articles per UTC publication
// day. Unscored rows carry no signal and are skipped.
func aggregateNews(rows []yahoo.NewsRow, keep map[string]bool) map[string]newsDay {
	out := map[string]newsDay{}
	for _, r := range rows {
		if r.LLMTicker == nil || !keep[*r.LLMTicker] || r.Datetime == nil ||
			r.LLMOverallSentiment == nil || r.LLMConfidence == nil {
			continue
		}
		day := r.Time().Format("2006-01-02")
		nd := out[day]
		nd.N++
		nd.SentSum += *r.LLMOverallSentiment * *r.LLMConfidence
		if r.LLMSignal != nil {
			switch *r.LLMSignal {
			case "bullish":
				nd.Bullish++
			case "bearish":
				nd.Bearish++
			}
		}
		out[day] = nd
	}
	return out
}

func walkCSV(dir string, fn func(path string, header, row []string) error) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".csv") {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		r := csv.NewReader(f)
		r.FieldsPerRecord = -1
		r.LazyQuotes = true
		header, err := r.Read()
		if err != nil {
			return nil
		}
		for {
			row, err := r.Read()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if err := fn(path, header, row); err != nil {
				return err
			}
		}
	})
}

func fieldGetter(header, row []string) func(string) string {
	return func(k string) string {
		for i, h := range header {
			if h == k && i < len(row) {
				return strings.TrimSpace(row[i])
			}
		}
		return ""
	}
}

func parseWindow(s string) (time.Time, time.Time, error) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, fmt.Errorf("window %q: want from:to", s)
	}
	from, err := time.Parse("2006-01-02", parts[0])
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := time.Parse("2006-01-02", parts[1])
	return from, to, err
}

func newRand(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }
