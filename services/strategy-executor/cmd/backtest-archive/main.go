// Command backtest-archive runs the backtest engine directly against the S3
// trade archive and reports results broken down by market regime.
//
// It deliberately does NOT use the production POST /api/v1/backtests endpoint.
// That endpoint is wired to indicators.HTTPDataProvider, which reads the live
// market-data service's own rolling Redis store — a different and much shorter
// dataset than the archive. Using it here would answer a different question.
//
// Usage:
//
//	go run ./cmd/backtest-archive \
//	  -archive <sync-dir> -from 2026-08-19 -to 2026-09-22 \
//	  -labels regime_labels.csv \
//	  -strategies mean_reversion,momentum,limit_profit,random_baseline
//
// The -labels CSV comes from strategy-router's classifier-backtest CLI run with
// its -labels flag. Every closed round-trip is attributed to the regime that
// was active at ENTRY time, which is the moment the decision was actually made.
//
// On sample sufficiency: any regime bucket with fewer than -min-samples closed
// trades is reported as DIRECTIONAL ONLY. A profitable-looking number drawn
// from a handful of trades is noise, and labelling it as such in the output is
// the whole point of this tool.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/strategy-executor/internal/backtest"
	"bitso-trading-platform/strategy-executor/internal/backtest/loader"
	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// MinSamplesDefault is the floor below which a per-regime result is treated as
// noise rather than a finding. It is intentionally blunt: there is no sound
// way to read expectancy off a couple of dozen trades.
const MinSamplesDefault = 30

func main() {
	archiveDir := flag.String("archive", "", "local directory holding an `aws s3 sync` copy of the archive")
	bucket := flag.String("bucket", "", "S3 bucket, when not using -archive")
	region := flag.String("region", "us-east-1", "AWS region for -bucket")
	prefix := flag.String("prefix", "trades", "archive root prefix")
	book := flag.String("book", "btc_mxn", "book to backtest")
	fromS := flag.String("from", "", "window start (YYYY-MM-DD or RFC3339)")
	toS := flag.String("to", "", "window end (YYYY-MM-DD or RFC3339)")
	labelsPath := flag.String("labels", "", "regime label CSV from classifier-backtest -labels")
	stratList := flag.String("strategies", "mean_reversion,momentum,limit_profit,random_baseline", "comma-separated strategy types")
	commissionBPS := flag.Float64("commission-bps", 65, "per-leg commission in bps (Bitso retail taker is 65 bps)")
	slippageBPS := flag.Float64("slippage-bps", 10, "slippage in bps")
	initialBalance := flag.Float64("balance", 100000, "initial quote balance")
	minSamples := flag.Int("min-samples", MinSamplesDefault, "closed trades required before a per-regime result is called supported")
	paramsJSON := flag.String("params", "", "JSON object of strategy parameters applied to every strategy (e.g. '{\"min_signal_interval\":0}')")
	jsonOut := flag.String("json", "", "write the full result set as JSON here")
	flag.Parse()

	params := map[string]interface{}{}
	if *paramsJSON != "" {
		if err := json.Unmarshal([]byte(*paramsJSON), &params); err != nil {
			fmt.Fprintf(os.Stderr, "backtest-archive: -params is not valid JSON: %v\n", err)
			os.Exit(1)
		}
	}

	if err := run(runArgs{
		archiveDir: *archiveDir, bucket: *bucket, region: *region, prefix: *prefix,
		book: *book, fromS: *fromS, toS: *toS, labelsPath: *labelsPath,
		strategies: splitCSV(*stratList), commissionBPS: *commissionBPS,
		slippageBPS: *slippageBPS, initialBalance: *initialBalance,
		minSamples: *minSamples, jsonOut: *jsonOut, params: params,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "backtest-archive: %v\n", err)
		os.Exit(1)
	}
}

type runArgs struct {
	archiveDir, bucket, region, prefix string
	book, fromS, toS, labelsPath       string
	strategies                         []string
	commissionBPS, slippageBPS         float64
	initialBalance                     float64
	minSamples                         int
	jsonOut                            string
	params                             map[string]interface{}
}

// regimeStat accumulates closed round-trips for one (strategy, regime) pair.
type regimeStat struct {
	Regime    string  `json:"regime"`
	Trades    int     `json:"trades"`
	Wins      int     `json:"wins"`
	Losses    int     `json:"losses"`
	NetPnL    float64 `json:"net_pnl"`
	GrossWin  float64 `json:"gross_win"`
	GrossLoss float64 `json:"gross_loss"`
	Supported bool    `json:"supported"`
}

func (r *regimeStat) winRate() float64 {
	if r.Trades == 0 {
		return 0
	}
	return float64(r.Wins) / float64(r.Trades) * 100
}

func (r *regimeStat) avgTrade() float64 {
	if r.Trades == 0 {
		return 0
	}
	return r.NetPnL / float64(r.Trades)
}

type strategyReport struct {
	Strategy     string                   `json:"strategy"`
	Overall      *backtest.BacktestResult `json:"overall"`
	ByRegime     map[string]*regimeStat   `json:"by_regime"`
	Unattributed int                      `json:"unattributed_trades"`
}

func run(a runArgs) error {
	from, err := parseTime(a.fromS)
	if err != nil {
		return fmt.Errorf("-from: %w", err)
	}
	to, err := parseTime(a.toS)
	if err != nil {
		return fmt.Errorf("-to: %w", err)
	}

	ctx := context.Background()

	var store loader.ObjectStore
	switch {
	case a.archiveDir != "":
		store = loader.NewLocalObjectStore(a.archiveDir)
	case a.bucket != "":
		s, err := loader.NewAWSObjectStore(ctx, a.region, a.bucket)
		if err != nil {
			return err
		}
		store = s
	default:
		return fmt.Errorf("provide -archive or -bucket")
	}

	src := loader.NewS3Archive(store, loader.ArchiveConfig{Bucket: a.bucket, Prefix: a.prefix, Concurrency: 64})
	trades, st, err := loader.LoadTradesOnly(ctx, src, a.book, from, to)
	if err != nil {
		return fmt.Errorf("load archive: %w", err)
	}
	fmt.Printf("DATA SOURCE : %s\n", src.Describe())
	fmt.Printf("LOAD STATS  : %s\n\n", st)
	if len(trades) == 0 {
		return fmt.Errorf("no trades loaded")
	}

	labels, err := loadLabels(a.labelsPath)
	if err != nil {
		return fmt.Errorf("labels: %w", err)
	}
	if labels.count() == 0 {
		fmt.Println("WARNING: no regime labels supplied — results will be reported in aggregate only.")
	}

	reports := make([]*strategyReport, 0, len(a.strategies))
	for _, sType := range a.strategies {
		fmt.Printf("running %-16s ... ", sType)
		res, err := backtest.RunHistorical(ctx, trades, backtest.EngineConfig{
			Book:           a.book,
			StrategyType:   sType,
			Parameters:     a.params,
			InitialBalance: a.initialBalance,
			SlippageBPS:    a.slippageBPS,
			CommissionBPS:  a.commissionBPS,
		})
		if err != nil {
			fmt.Printf("FAILED: %v\n", err)
			continue
		}
		fmt.Printf("%d closed trades, net %.2f MXN\n", res.TotalTrades, res.TotalPnL)

		rep := &strategyReport{Strategy: sType, Overall: res, ByRegime: map[string]*regimeStat{}}
		attributeByRegime(rep, res, labels, a.minSamples)
		reports = append(reports, rep)
	}

	printReport(reports, a)

	if a.jsonOut != "" {
		f, err := os.Create(a.jsonOut)
		if err != nil {
			return err
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		// Signals are dropped from the JSON: they can run to hundreds of
		// thousands of entries and swamp the summary.
		for _, r := range reports {
			if r.Overall != nil {
				r.Overall.Signals = nil
			}
		}
		if err := enc.Encode(reports); err != nil {
			return err
		}
		fmt.Printf("\nwrote JSON results to %s\n", a.jsonOut)
	}
	return nil
}

// attributeByRegime walks the signal stream, pairing each SELL with the BUY
// that preceded it, and books the realized P&L against the regime that was
// active at the ENTRY instant — the moment the strategy actually committed.
func attributeByRegime(rep *strategyReport, res *backtest.BacktestResult, labels *regimeTimeline, minSamples int) {
	var entryAt time.Time
	inPosition := false

	for _, sig := range res.Signals {
		// TickTime is the simulated market time. Signal.Timestamp is wall-clock
		// (strategies stamp it with time.Now()), so using it here would bucket
		// every trade into whatever regime was active at the instant the
		// backtest ran — which is meaningless.
		at := sig.TickTime
		if at.IsZero() {
			at = sig.Timestamp
		}

		switch {
		case sig.Side == "BUY" && !inPosition:
			entryAt = at
			inPosition = true

		case sig.Side == "SELL" && inPosition:
			inPosition = false
			regime := "unlabelled"
			if labels != nil {
				if r, ok := labels.at(entryAt); ok {
					regime = r
				}
			}
			if regime == "unlabelled" {
				rep.Unattributed++
			}

			rs := rep.ByRegime[regime]
			if rs == nil {
				rs = &regimeStat{Regime: regime}
				rep.ByRegime[regime] = rs
			}
			rs.Trades++
			rs.NetPnL += sig.PnL
			if sig.PnL > 0 {
				rs.Wins++
				rs.GrossWin += sig.PnL
			} else {
				rs.Losses++
				rs.GrossLoss += math.Abs(sig.PnL)
			}
		}
	}

	for _, rs := range rep.ByRegime {
		rs.Supported = rs.Trades >= minSamples
	}
}

// ---------------------------------------------------------------------------
// Regime timeline
// ---------------------------------------------------------------------------

// regimeTimeline maps an instant to the regime label of the most recent
// snapshot at or before it. Snapshots are point samples at a fixed cadence, so
// a trade almost never lands exactly on one; carrying the previous label
// forward is the same thing the live router does between polls.
type regimeTimeline struct {
	times   []time.Time
	regimes []string
}

// count is nil-safe so callers can probe an absent timeline.
func (t *regimeTimeline) count() int {
	if t == nil {
		return 0
	}
	return len(t.times)
}

func (t *regimeTimeline) at(ts time.Time) (string, bool) {
	if t == nil || len(t.times) == 0 {
		return "", false
	}
	i := sort.Search(len(t.times), func(i int) bool { return t.times[i].After(ts) })
	if i == 0 {
		return "", false // before the first snapshot
	}
	return t.regimes[i-1], true
}

func loadLabels(path string) (*regimeTimeline, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	tl := &regimeTimeline{}
	type pair struct {
		ts     time.Time
		regime string
	}
	var pairs []pair
	for i, row := range rows {
		if i == 0 && len(row) > 0 && row[0] == "snapshot_at" {
			continue // header
		}
		if len(row) < 2 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, row[0])
		if err != nil {
			continue
		}
		pairs = append(pairs, pair{ts: ts.UTC(), regime: row[1]})
	}

	// Sort as paired records. Sorting the two parallel slices separately would
	// de-pair timestamps from their labels and silently mis-attribute every
	// trade, so the pairing is kept intact through the sort.
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].ts.Before(pairs[j].ts) })

	tl.times = make([]time.Time, len(pairs))
	tl.regimes = make([]string, len(pairs))
	for i, p := range pairs {
		tl.times[i] = p.ts
		tl.regimes[i] = p.regime
	}
	return tl, nil
}

// ---------------------------------------------------------------------------
// Reporting
// ---------------------------------------------------------------------------

func printReport(reports []*strategyReport, a runArgs) {
	fmt.Printf("\n%s\n", strings.Repeat("=", 100))
	fmt.Printf("BACKTEST RESULTS  book=%s  window=%s..%s\n", a.book, a.fromS, a.toS)
	fmt.Printf("fees: %.0f bps per leg, slippage %.0f bps | min samples for a supported verdict: %d\n",
		a.commissionBPS, a.slippageBPS, a.minSamples)
	fmt.Printf("%s\n\n", strings.Repeat("=", 100))

	fmt.Printf("%-18s %8s %8s %9s %14s %12s %10s\n", "STRATEGY", "TRADES", "WINS", "WIN%", "NET P&L (MXN)", "PROFIT FAC", "MAX DD%")
	fmt.Printf("%s\n", strings.Repeat("-", 100))
	for _, r := range reports {
		o := r.Overall
		fmt.Printf("%-18s %8d %8d %8.1f%% %14.2f %12.2f %9.2f%%\n",
			r.Strategy, o.TotalTrades, o.WinningTrades, o.WinRate, o.TotalPnL, o.ProfitFactor, o.MaxDrawdownPct)
	}

	var baseline *strategyReport
	for _, r := range reports {
		if r.Strategy == "random_baseline" {
			baseline = r
		}
	}

	fmt.Printf("\n\nPER-REGIME BREAKDOWN (attributed at entry time)\n")
	fmt.Printf("%s\n", strings.Repeat("-", 100))
	for _, r := range reports {
		fmt.Printf("\n%s\n", strings.ToUpper(r.Strategy))
		if len(r.ByRegime) == 0 {
			fmt.Println("  no closed trades")
			continue
		}
		names := make([]string, 0, len(r.ByRegime))
		for k := range r.ByRegime {
			names = append(names, k)
		}
		sort.Strings(names)

		fmt.Printf("  %-16s %8s %8s %14s %14s   %s\n", "REGIME", "TRADES", "WIN%", "NET P&L", "AVG/TRADE", "VERDICT")
		for _, n := range names {
			rs := r.ByRegime[n]
			verdict := "DIRECTIONAL ONLY — insufficient samples"
			if rs.Supported {
				verdict = "supported by adequate samples"
			}
			fmt.Printf("  %-16s %8d %7.1f%% %14.2f %14.4f   %s\n",
				n, rs.Trades, rs.winRate(), rs.NetPnL, rs.avgTrade(), verdict)
		}
	}

	if baseline != nil {
		fmt.Printf("\n\nVS RANDOM-SIGNAL BASELINE (net P&L per closed trade)\n")
		fmt.Printf("%s\n", strings.Repeat("-", 100))
		b := baseline.Overall
		bAvg := 0.0
		if b.TotalTrades > 0 {
			bAvg = b.TotalPnL / float64(b.TotalTrades)
		}
		fmt.Printf("  %-18s %10.4f MXN/trade over %d trades\n", "random_baseline", bAvg, b.TotalTrades)
		for _, r := range reports {
			if r.Strategy == "random_baseline" {
				continue
			}
			avg := 0.0
			if r.Overall.TotalTrades > 0 {
				avg = r.Overall.TotalPnL / float64(r.Overall.TotalTrades)
			}
			delta := avg - bAvg
			flag := "WORSE than random"
			if delta > 0 {
				flag = "better than random"
			}
			fmt.Printf("  %-18s %10.4f MXN/trade over %d trades  (%+.4f vs baseline — %s)\n",
				r.Strategy, avg, r.Overall.TotalTrades, delta, flag)
		}
	}
	fmt.Printf("\n%s\n", strings.Repeat("=", 100))
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("required")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("want YYYY-MM-DD or RFC3339, got %q", s)
	}
	return t.UTC(), nil
}

var _ = indicators.Trade{}
