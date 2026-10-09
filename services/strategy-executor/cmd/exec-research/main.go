// Command exec-research replays alternative execution schedules against the
// archived Bitso public trades and reports what each would have cost per
// leg, against the pre-registered cost assumptions (plan §6.4.13).
//
//	go run ./cmd/exec-research -archive-dir <dir containing trades_compacted/> \
//	    -out-json exec.json -out-md exec.md
//	go run ./cmd/exec-research -bucket mtb-development-data-archive-326105557351 ...
//
// Every archived day is simulated twice per schedule (a buy and a sell leg),
// starting at each -starts time of day (UTC). Days are Bitso's candle days
// (Mexico City midnight, 06:00 UTC), so "vs open" is the cost against the
// paper account's price; the stage executor starts at 06:15. Schedules: an
// immediate market order, one post-only order resting
// for each window then a market fallback (today: 60m), the same with the
// price re-pegged every 5 minutes, and TWAP child orders over the window.
// Two fill models bracket the unknown queue position (internal/execsim).
//
// Reporting only: the live executor and the frozen costs do not change
// because of this study. Any execution change needs its own
// pre-registration. Exit status: 0 on success, 1 on errors, 2 on bad usage.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/shared/pkg/execcost"
	"bitso-trading-platform/strategy-executor/internal/backtest/loader"
	"bitso-trading-platform/strategy-executor/internal/execsim"
)

const schema = "exec-research/v1"

// Schedule is one execution schedule studied.
type Schedule struct {
	Name    string        `json:"name"`
	Window  time.Duration `json:"window_ns"`
	Slices  int           `json:"slices"`
	Reprice time.Duration `json:"reprice_ns"`
	Today   bool          `json:"today"` // the live executor's schedule
}

// Row is one schedule × fill model × size × start time.
type Row struct {
	Schedule string  `json:"schedule"`
	Model    string  `json:"model"`
	QtyBTC   float64 `json:"qty_btc"`
	Start    string  `json:"start"`
	execsim.Summary
	VsPrimaryBps float64 `json:"vs_primary_bps"` // mean cost − pre-registered primary leg (+ is over)
}

// BookReport is one book's study.
type BookReport struct {
	Book          string  `json:"book"`
	From          string  `json:"from"`
	To            string  `json:"to"`
	Trades        int     `json:"trades"`
	Days          int     `json:"days"`      // days with trades
	DaysUsed      int     `json:"days_used"` // days whose coverage passed, at the first start
	DaysSkipped   int     `json:"days_skipped"`
	ADVBTC        float64 `json:"adv_btc"`
	DailyVol      float64 `json:"daily_vol"`
	HalfSpreadBps float64 `json:"half_spread_bps"`
	// HalfSpreadSource is "flag" or "book-samples (n=…)": the measured
	// median spread / 2 from the hourly sampler when -book-samples is set.
	HalfSpreadSource string  `json:"half_spread_source"`
	MakerFeeBps      float64 `json:"maker_fee_bps"`
	TakerFeeBps      float64 `json:"taker_fee_bps"`
	PrimaryLegBps    float64 `json:"primary_leg_bps"`
	SecondaryLegBps  float64 `json:"secondary_leg_bps"`
	Rows             []Row   `json:"rows"`
	// Comparisons pair -compare-base and -compare-alt leg by leg (same day,
	// side, start, size and fill model); negative is alt cheaper.
	Comparisons []Comparison `json:"comparisons"`
}

// Comparison is one paired comparison.
type Comparison struct {
	Base   string  `json:"base"`
	Alt    string  `json:"alt"`
	Model  string  `json:"model"`
	QtyBTC float64 `json:"qty_btc"`
	Start  string  `json:"start"`
	execsim.Paired
}

// Report is the JSON written.
type Report struct {
	Schema      string       `json:"schema"`
	GeneratedAt string       `json:"generated_at"`
	Source      string       `json:"source"`
	MaxGap      string       `json:"max_gap"`
	ImpactY     float64      `json:"impact_y"`
	Schedules   []Schedule   `json:"schedules"`
	Books       []BookReport `json:"books"`
	Note        string       `json:"note"`
}

type config struct {
	starts      []time.Duration // time of day, UTC
	windows     []time.Duration
	sizes       []float64
	halfSpread  map[string]float64
	maxGap      time.Duration
	quoteMaxAge time.Duration
	impactY     float64
	reportSize  float64
	keep        map[string]bool // -schedules; empty keeps all
	base, alt   string          // -compare-base, -compare-alt
}

func main() {
	archiveDir := flag.String("archive-dir", "", "local copy of the archive (the directory that contains -prefix)")
	bucket := flag.String("bucket", "", "S3 bucket (read-only: list and get)")
	region := flag.String("region", "us-east-1", "AWS region for -bucket")
	prefix := flag.String("prefix", "trades_compacted", "archive root inside the bucket or directory")
	books := flag.String("books", "btc_mxn,btc_usd", "books")
	fromS := flag.String("from", "2026-08-01", "first day (UTC)")
	toS := flag.String("to", "", "last day (UTC, default yesterday)")
	startsS := flag.String("starts", "06:01,06:15,14:00", "leg start times of day, UTC (the daily close is 06:00 UTC; the stage executor starts at 06:15)")
	windowsS := flag.String("windows", "15m,30m,60m,120m,240m,480m", "maker windows")
	sizesS := flag.String("sizes", "0.001,0.01,0.1,0.5", "leg sizes, BTC")
	hsS := flag.String("half-spread", "btc_mxn=1.5,btc_usd=1.3", "fallback half-spread per book when no fresh bid/ask print exists, bps (capacity page: spreads 1.8-4 and 2.6 bps)")
	quoteMaxAge := flag.Duration("quote-max-age", 10*time.Minute, "how old a bid/ask print may be to stand in for the best price (0: always the half-spread)")
	maxGap := flag.Duration("max-gap", 30*time.Minute, "skip a day when the archive has a longer stretch without trades between the day's open and the end of the longest window")
	impactY := flag.Float64("impact-y", 1.0, "square-root impact coefficient for market orders (1.0: the conservative end)")
	reportSize := flag.Float64("report-size", 0.001, "leg size of the detailed markdown tables")
	schedulesS := flag.String("schedules", "", "only these schedules, comma-separated names as in the report (default: all)")
	compareBase := flag.String("compare-base", "maker 1h", "paired comparison: the baseline schedule (today's)")
	compareAlt := flag.String("compare-alt", "maker 1h repriced 5m", "paired comparison: the alternative schedule")
	bookSamples := flag.String("book-samples", "", "hourly book samples dir (cmd/book-sampler): the fallback half-spread becomes the measured median spread / 2 when it has at least 24 samples in the window")
	outJSON := flag.String("out-json", "", "write the JSON here")
	outMD := flag.String("out-md", "", "write the markdown here (default stdout)")
	flag.Parse()
	if (*archiveDir == "") == (*bucket == "") || flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "exec-research: exactly one of -archive-dir or -bucket is required")
		flag.Usage()
		os.Exit(2)
	}
	cfg, err := parseConfig(*startsS, *windowsS, *sizesS, *hsS)
	if err != nil {
		fmt.Fprintln(os.Stderr, "exec-research:", err)
		os.Exit(2)
	}
	cfg.maxGap, cfg.impactY, cfg.reportSize, cfg.quoteMaxAge = *maxGap, *impactY, *reportSize, *quoteMaxAge
	cfg.base, cfg.alt = *compareBase, *compareAlt
	if names := splitList(*schedulesS); len(names) > 0 {
		cfg.keep = map[string]bool{}
		for _, n := range names {
			cfg.keep[n] = true
		}
	}
	from, err := time.Parse("2006-01-02", *fromS)
	if err != nil {
		fmt.Fprintln(os.Stderr, "exec-research: -from:", err)
		os.Exit(2)
	}
	to := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	if *toS != "" {
		if to, err = time.Parse("2006-01-02", *toS); err != nil {
			fmt.Fprintln(os.Stderr, "exec-research: -to:", err)
			os.Exit(2)
		}
	}

	ctx := context.Background()
	var store loader.ObjectStore
	source := "s3://" + *bucket + "/" + *prefix
	if *archiveDir != "" {
		store, source = loader.NewLocalObjectStore(*archiveDir), *archiveDir+"/"+*prefix
	} else {
		s, err := loader.NewAWSObjectStore(ctx, *region, *bucket)
		if err != nil {
			fmt.Fprintln(os.Stderr, "exec-research:", err)
			os.Exit(1)
		}
		store = s
	}
	src := loader.NewS3Archive(store, loader.ArchiveConfig{Bucket: *bucket, Prefix: *prefix, Concurrency: 32})

	rep := Report{Schema: schema, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Source: source,
		MaxGap: cfg.maxGap.String(), ImpactY: cfg.impactY, Schedules: filterSchedules(schedules(cfg.windows), cfg.keep), Note: note}
	if len(rep.Schedules) == 0 {
		fmt.Fprintln(os.Stderr, "exec-research: -schedules matches no schedule")
		os.Exit(2)
	}
	for _, b := range strings.Split(*books, ",") {
		b = strings.TrimSpace(b)
		rows, st, err := src.LoadTrades(ctx, b, from, to.Add(24*time.Hour-time.Nanosecond))
		if err != nil {
			fmt.Fprintln(os.Stderr, "exec-research:", b, err)
			os.Exit(1)
		}
		if st.ObjectsFailed > 0 {
			fmt.Fprintf(os.Stderr, "exec-research: %s: %d objects failed to load\n", b, st.ObjectsFailed)
		}
		trades := make([]execsim.Trade, len(rows))
		for i, r := range rows {
			trades[i] = execsim.Trade{At: r.ExchangeTS.UTC(), Price: r.Price, Amount: r.Amount, MakerSide: makerSide(r.MakerSide)}
		}
		bc := cfg
		bc.halfSpread = map[string]float64{b: cfg.halfSpread[b]}
		src := "flag"
		if *bookSamples != "" {
			if hs, n := sampledHalfSpread(*bookSamples, b, from, to.Add(24*time.Hour)); n >= minSamples {
				bc.halfSpread[b], src = hs, fmt.Sprintf("book-samples (n=%d)", n)
			}
		}
		br := study(b, trades, bc, rep.Schedules)
		br.HalfSpreadSource = src
		rep.Books = append(rep.Books, br)
	}
	if *outJSON != "" {
		data, _ := json.MarshalIndent(rep, "", "  ")
		if err := writeFile(*outJSON, append(data, '\n')); err != nil {
			fmt.Fprintln(os.Stderr, "exec-research:", err)
			os.Exit(1)
		}
	}
	w := io.Writer(os.Stdout)
	if *outMD != "" {
		f, err := os.Create(*outMD)
		if err != nil {
			fmt.Fprintln(os.Stderr, "exec-research:", err)
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}
	writeMarkdown(w, rep, cfg)
}

const note = "Research only. Trades are Bitso production public prints; stage fills are thinner. " +
	"The best bid/ask is the last print with that maker side (else the last trade ± the assumed half-spread); " +
	"Through/Touch bracket the unknown queue position. " +
	"Market orders take the ask/bid plus square-root impact. The live executor and the pre-registered costs are unchanged; " +
	"any execution change needs its own pre-registration."

// minSamples is the number of hourly book samples (a day) before the
// measured spread replaces the flag's half-spread.
const minSamples = 24

// sampledHalfSpread is half the median spread of book's samples in [from, to).
func sampledHalfSpread(dir, book string, from, to time.Time) (float64, int) {
	samples, _, err := execcost.ReadSamples(execcost.SamplePath(dir, book))
	if err != nil {
		return 0, 0
	}
	var in []execcost.Sample
	for _, s := range samples {
		if at, err := time.Parse(time.RFC3339, s.At); err == nil && at.Before(to) {
			in = append(in, s)
		}
	}
	sum := execcost.SummarizeSamples(in, from)
	return sum.SpreadBps.P50 / 2, sum.Samples
}

func filterSchedules(all []Schedule, keep map[string]bool) []Schedule {
	if len(keep) == 0 {
		return all
	}
	var out []Schedule
	for _, s := range all {
		if keep[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

// makerSide maps the archive's maker_side to execsim's convention.
func makerSide(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "buy":
		return 1
	case "sell":
		return -1
	}
	return 0
}

func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func parseConfig(starts, windows, sizes, hs string) (config, error) {
	var c config
	for _, s := range splitList(starts) {
		t, err := time.Parse("15:04", s)
		if err != nil {
			return c, fmt.Errorf("-starts %q: %w", s, err)
		}
		c.starts = append(c.starts, time.Duration(t.Hour())*time.Hour+time.Duration(t.Minute())*time.Minute)
	}
	for _, s := range splitList(windows) {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			return c, fmt.Errorf("-windows %q: want a positive duration", s)
		}
		c.windows = append(c.windows, d)
	}
	for _, s := range splitList(sizes) {
		q, err := strconv.ParseFloat(s, 64)
		if err != nil || q <= 0 {
			return c, fmt.Errorf("-sizes %q: want a positive number", s)
		}
		c.sizes = append(c.sizes, q)
	}
	c.halfSpread = map[string]float64{}
	for _, kv := range splitList(hs) {
		k, v, ok := strings.Cut(kv, "=")
		f, err := strconv.ParseFloat(v, 64)
		if !ok || err != nil || f < 0 {
			return c, fmt.Errorf("-half-spread %q: want book=bps", kv)
		}
		c.halfSpread[k] = f
	}
	if len(c.starts) == 0 || len(c.windows) == 0 || len(c.sizes) == 0 {
		return c, fmt.Errorf("-starts, -windows and -sizes must not be empty")
	}
	return c, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// todayWindow is the live executor's maker timeout (dailyexec.DefaultConfig).
const todayWindow = 60 * time.Minute

// todayStart is when the stage executor starts (scripts/ops-run.sh cron, 06:15 UTC).
const todayStart = "06:15"

func schedules(windows []time.Duration) []Schedule {
	out := []Schedule{{Name: "market"}}
	for _, w := range windows {
		out = append(out, Schedule{Name: "maker " + short(w), Window: w, Slices: 1, Today: w == todayWindow})
	}
	for _, w := range windows {
		if w > 5*time.Minute {
			out = append(out, Schedule{Name: "maker " + short(w) + " repriced 5m", Window: w, Slices: 1, Reprice: 5 * time.Minute})
		}
	}
	for _, w := range windows {
		if w >= 60*time.Minute {
			out = append(out, Schedule{Name: "TWAP 4×" + short(w/4), Window: w, Slices: 4})
		}
	}
	return out
}

func short(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
}

// dayStats returns the mean daily volume and the std of daily log returns
// (last trade to last trade) over complete UTC days.
func dayStats(trades []execsim.Trade) (adv, vol float64, days int) {
	type agg struct {
		vol  float64
		last float64
	}
	byDay := map[string]*agg{}
	var keys []string
	for _, t := range trades {
		k := t.At.Format("2006-01-02")
		a := byDay[k]
		if a == nil {
			a = &agg{}
			byDay[k] = a
			keys = append(keys, k)
		}
		a.vol += t.Amount
		a.last = t.Price
	}
	sort.Strings(keys)
	days = len(keys)
	if days < 3 {
		return 0, 0, days
	}
	inner := keys[1 : len(keys)-1] // the first and last day may be partial
	closes, vols := make([]float64, len(inner)), make([]float64, len(inner))
	for i, k := range inner {
		closes[i], vols[i] = byDay[k].last, byDay[k].vol
	}
	adv, _, vol = execcost.DailyStats(closes, vols, len(inner))
	return adv, vol, days
}

func study(book string, trades []execsim.Trade, cfg config, scheds []Schedule) BookReport {
	br := BookReport{Book: book, Trades: len(trades), HalfSpreadBps: cfg.halfSpread[book], Rows: []Row{}, Comparisons: []Comparison{}}
	if pc, ok := dailyledger.PreregCosts[book]; ok {
		br.PrimaryLegBps, br.SecondaryLegBps = pc.PrimaryLegBps, pc.SecondaryLegBps
		br.MakerFeeBps, br.TakerFeeBps = pc.PrimaryLegBps-10, pc.SecondaryLegBps-10
	}
	if len(trades) == 0 {
		return br
	}
	br.From, br.To = trades[0].At.Format("2006-01-02"), trades[len(trades)-1].At.Format("2006-01-02")
	br.ADVBTC, br.DailyVol, br.Days = dayStats(trades)
	impact := func(q float64) float64 {
		return execcost.SqrtImpactBps(q, br.ADVBTC, br.DailyVol, cfg.impactY)
	}
	var maxW time.Duration
	for _, s := range scheds {
		if s.Window > maxW {
			maxW = s.Window
		}
	}
	fm := trades[0].At.In(bitsodaily.Mexico)
	first := time.Date(fm.Year(), fm.Month(), fm.Day(), 0, 0, 0, 0, bitsodaily.Mexico)
	last := trades[len(trades)-1].At

	type key struct {
		sched, model string
		qty          float64
		start        time.Duration
	}
	legs := map[key][]execsim.Leg{}
	for si, start := range cfg.starts {
		for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
			// d is the day's open (Mexico City midnight); the leg starts at
			// the first -starts time of day (UTC) at or after it.
			t0 := d.UTC().Truncate(24 * time.Hour).Add(start)
			if t0.Before(d) {
				t0 = t0.Add(24 * time.Hour)
			}
			end := t0.Add(maxW)
			if end.After(last) || d.Before(trades[0].At) {
				continue // outside the archive: neither used nor skipped
			}
			if execsim.MaxGap(trades, d, end) > cfg.maxGap {
				if si == 0 {
					br.DaysSkipped++
				}
				continue
			}
			if si == 0 {
				br.DaysUsed++
			}
			open := firstFrom(trades, d)
			for _, s := range scheds {
				models := []execsim.FillModel{execsim.Through, execsim.Touch}
				if s.Window == 0 {
					models = models[:1] // a market order has no queue
				}
				for _, m := range models {
					for _, q := range cfg.sizes {
						for _, buy := range []bool{true, false} {
							r, ok := execsim.Run(trades, t0, execsim.Params{Buy: buy, Qty: q, Window: s.Window, Slices: s.Slices,
								RepriceEvery: s.Reprice, QuoteMaxAge: cfg.quoteMaxAge, HalfSpreadBps: br.HalfSpreadBps, Model: m,
								MakerFeeBps: br.MakerFeeBps, TakerFeeBps: br.TakerFeeBps, ImpactBps: impact})
							if !ok {
								continue
							}
							k := key{s.Name, m.String(), q, start}
							legs[k] = append(legs[k], execsim.Leg{Result: r, VsOpenBps: r.VsBps(open, buy)})
						}
					}
				}
			}
		}
	}
	for _, start := range cfg.starts {
		for _, q := range cfg.sizes {
			for _, s := range scheds {
				for _, m := range []string{"through", "touch"} {
					l, ok := legs[key{s.Name, m, q, start}]
					if !ok {
						continue
					}
					sum := execsim.Summarize(l)
					br.Rows = append(br.Rows, Row{Schedule: s.Name, Model: m, QtyBTC: q, Start: clock(start),
						Summary: sum, VsPrimaryBps: sum.MeanBps - br.PrimaryLegBps})
				}
			}
			for _, m := range []string{"through", "touch"} {
				// Legs are appended in the same day/side order for every
				// schedule, so index i is the same leg in both slices.
				if p, ok := execsim.Compare(legs[key{cfg.base, m, q, start}], legs[key{cfg.alt, m, q, start}]); ok {
					br.Comparisons = append(br.Comparisons, Comparison{Base: cfg.base, Alt: cfg.alt, Model: m, QtyBTC: q, Start: clock(start), Paired: p})
				}
			}
		}
	}
	return br
}

func firstFrom(trades []execsim.Trade, t time.Time) float64 {
	i := sort.Search(len(trades), func(i int) bool { return !trades[i].At.Before(t) })
	if i < len(trades) {
		return trades[i].Price
	}
	return 0
}

func clock(d time.Duration) string {
	return fmt.Sprintf("%02d:%02d", int(d/time.Hour), int(d%time.Hour/time.Minute))
}

func find(rows []Row, sched, model string, q float64, start string) (Row, bool) {
	for _, r := range rows {
		if r.Schedule == sched && r.Model == model && r.QtyBTC == q && r.Start == start {
			return r, true
		}
	}
	return Row{}, false
}

func writeMarkdown(w io.Writer, rep Report, cfg config) {
	fmt.Fprintf(w, "# Execution research (generated %s)\n\n", rep.GeneratedAt)
	fmt.Fprintf(w, "Source: `%s`. Days are Bitso candle days (open at Mexico City midnight, 06:00 UTC); a day with a gap over %s between its open and the end of the longest window is skipped. Market orders: half-spread + square-root impact (Y = %.1f).\n\n", rep.Source, rep.MaxGap, rep.ImpactY)
	fmt.Fprintf(w, "> %s\n\n", rep.Note)
	todayName := "maker " + short(todayWindow)
	for _, b := range rep.Books {
		fmt.Fprintf(w, "## %s\n\n", b.Book)
		fmt.Fprintf(w, "%s → %s: %d trades over %d days; %d days used, %d skipped. ADV %.2f BTC, daily vol %.2f %%. Half-spread %.1f bps (%s). Fees: maker %.0f, taker %.0f bps; pre-registered legs: primary %.0f, secondary %.0f bps.\n\n",
			b.From, b.To, b.Trades, b.Days, b.DaysUsed, b.DaysSkipped, b.ADVBTC, b.DailyVol*100, b.HalfSpreadBps, b.HalfSpreadSource, b.MakerFeeBps, b.TakerFeeBps, b.PrimaryLegBps, b.SecondaryLegBps)
		if len(b.Rows) == 0 {
			fmt.Fprintf(w, "No usable days.\n\n")
			continue
		}
		for _, start := range cfg.starts {
			st := clock(start)
			fmt.Fprintf(w, "### Start %s UTC, %.3g BTC per leg\n\n", st, cfg.reportSize)
			fmt.Fprintf(w, "| Schedule | Model | Legs | Maker share | No fallback | Mean | Median | p90 | vs primary | vs open (mean ± sd) |\n|---|---|---|---|---|---|---|---|---|---|\n")
			for _, r := range b.Rows {
				if r.Start != st || r.QtyBTC != cfg.reportSize {
					continue
				}
				name := r.Schedule
				if name == todayName {
					name += " (today)"
				}
				fmt.Fprintf(w, "| %s | %s | %d | %.0f %% | %.0f %% | %.1f | %.1f | %.1f | %+.1f | %+.1f ± %.0f |\n",
					name, r.Model, r.Legs, r.MakerShare*100, r.FullMakerRate*100, r.MeanBps, r.MedianBps, r.P90Bps, r.VsPrimaryBps, r.MeanVsOpenBps, r.StdVsOpenBps)
			}
			fmt.Fprintln(w)
		}
		st := clock(cfg.starts[len(cfg.starts)-1])
		for _, s := range cfg.starts {
			if clock(s) == todayStart {
				st = todayStart
			}
		}
		fmt.Fprintf(w, "### By size, start %s UTC, Through model (mean cost per leg, bps)\n\n| Size BTC | market | %s (today) | best schedule | best mean | best p90 |\n|---|---|---|---|---|---|\n", st, todayName)
		for _, q := range cfg.sizes {
			mk, _ := find(b.Rows, "market", "through", q, st)
			td, _ := find(b.Rows, todayName, "through", q, st)
			best := Row{Summary: execsim.Summary{MeanBps: math.Inf(1)}}
			for _, r := range b.Rows {
				if r.Start == st && r.QtyBTC == q && r.Model == "through" && r.MeanBps < best.MeanBps {
					best = r
				}
			}
			fmt.Fprintf(w, "| %g | %.1f | %.1f | %s | %.1f | %.1f |\n", q, mk.MeanBps, td.MeanBps, best.Schedule, best.MeanBps, best.P90Bps)
		}
		fmt.Fprintln(w)
		if len(b.Comparisons) > 0 {
			c0 := b.Comparisons[0]
			fmt.Fprintf(w, "### Paired: %s against %s (alt − base, bps per leg; negative = alt cheaper)\n\n", c0.Alt, c0.Base)
			fmt.Fprintf(w, "| Start | Size BTC | Model | Legs | Base mean | Alt mean | Diff | SE | p (alt cheaper) | Alt cheaper / tie | Base p90 | Alt p90 |\n|---|---|---|---|---|---|---|---|---|---|---|---|\n")
			for _, c := range b.Comparisons {
				fmt.Fprintf(w, "| %s | %g | %s | %d | %.1f | %.1f | %+.2f | %.2f | %.3f | %.0f %% / %.0f %% | %.1f | %.1f |\n",
					c.Start, c.QtyBTC, c.Model, c.N, c.BaseMeanBps, c.AltMeanBps, c.MeanDiffBps, c.StdErrBps, c.PAltCheaper, c.AltCheaperShare*100, c.TieShare*100, c.BaseP90Bps, c.AltP90Bps)
			}
			fmt.Fprintln(w)
		}
	}
}
