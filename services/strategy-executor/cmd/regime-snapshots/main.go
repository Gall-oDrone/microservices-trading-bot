// Command regime-snapshots replays the archived trade history through the same
// indicator pipeline the live strategy-executor runs, and emits one JSON line
// per sampled snapshot in the shape strategy-router's classifier.Snapshot
// expects.
//
// The output is designed to be piped straight into the existing offline CLI:
//
//	go run ./cmd/regime-snapshots -archive <dir> -from 2026-08-19 -to 2026-09-22 \
//	  | go run ../strategy-router/cmd/classifier-backtest/main.go -stdin
//
// Fidelity notes — these determine whether the resulting regime distribution
// means anything at all:
//
//   - Bars are 1-minute, matching INDICATORS_BAR_INTERVAL (default "1m").
//   - Indicator periods default to the production values in
//     services/strategy-executor/internal/config.Load: SMA/EMA/Bollinger 20,
//     RSI 14, ATR 14, Bollinger stddev 2.0.
//   - Each snapshot is computed from the trailing barFetchLimit bars only
//     (minBarsRequired + INDICATORS_BAR_LIMIT_BUFFER, floored at 30), exactly
//     as indicators.Service.ComputeAndStore does.
//   - Price is the last bar's close, which is what Service populates as
//     CurrentPrice and what the classifier reads as Snapshot.Price.
//   - Only bars that closed at or before the sample instant are visible, so
//     there is no look-ahead bias.
//   - The default sampling cadence is 30s, matching ROUTER_INTERVAL_SEC. This
//     is the cadence at which the live router actually classifies, so the
//     resulting histogram is the distribution the router would have seen.
//
// A coarser -cadence (e.g. 24h) does NOT give a "daily regime" view unless
// -bar is widened too: with 1m bars and a 14-period ATR, one sample per day
// describes only the last ~20 minutes of that day. Use -bar 1h -cadence 1h for
// a macro view, and say which one you are reporting.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/strategy-executor/internal/backtest/loader"
	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// snapshotLine mirrors strategy-router/internal/classifier.Snapshot.
//
// That struct carries no json tags, so encoding/json matches on the exact Go
// field names. The tags below reproduce those names verbatim. strategy-router
// is a separate Go module and its classifier package cannot be imported here,
// so this mirror is the seam — TestSnapshotLineFieldNames locks the key set so
// a rename on either side fails loudly instead of silently producing
// all-zero snapshots that classify as "neutral".
type snapshotLine struct {
	Book        string  `json:"Book"`
	Price       float64 `json:"Price"`
	ATR         float64 `json:"ATR"`
	EMA         float64 `json:"EMA"`
	RSI         float64 `json:"RSI"`
	BBUpper     float64 `json:"BBUpper"`
	BBMiddle    float64 `json:"BBMiddle"`
	BBLower     float64 `json:"BBLower"`
	DataHealthy bool    `json:"DataHealthy"`
	StaleReason string  `json:"StaleReason"`

	// SnapshotAt is an extra field, ignored by classifier.Snapshot's decoder.
	// It lets the same JSONL be sliced by day for a per-day breakdown without
	// reimplementing the classifier anywhere.
	SnapshotAt string `json:"SnapshotAt"`
}

type config struct {
	archiveDir  string
	bucket      string
	region      string
	prefix      string
	book        string
	from        string
	to          string
	barInterval time.Duration
	cadence     time.Duration
	smaPeriod   int
	emaPeriod   int
	rsiPeriod   int
	bbPeriod    int
	bbStdDev    float64
	atrPeriod   int
	barBuffer   int
	out         string
	statsOut    string
}

func main() {
	var c config
	flag.StringVar(&c.archiveDir, "archive", "", "local directory holding an `aws s3 sync` copy of the archive (preferred; avoids re-downloading)")
	flag.StringVar(&c.bucket, "bucket", "", "S3 bucket to read directly when -archive is empty")
	flag.StringVar(&c.region, "region", "us-east-1", "AWS region for -bucket")
	flag.StringVar(&c.prefix, "prefix", "trades", "archive root prefix")
	flag.StringVar(&c.book, "book", "btc_mxn", "book to analyze")
	flag.StringVar(&c.from, "from", "", "window start (YYYY-MM-DD or RFC3339)")
	flag.StringVar(&c.to, "to", "", "window end (YYYY-MM-DD or RFC3339)")
	flag.DurationVar(&c.barInterval, "bar", time.Minute, "bar interval (production INDICATORS_BAR_INTERVAL is 1m)")
	flag.DurationVar(&c.cadence, "cadence", 30*time.Second, "snapshot sampling cadence (production ROUTER_INTERVAL_SEC is 30s)")
	flag.IntVar(&c.smaPeriod, "sma", 20, "SMA period")
	flag.IntVar(&c.emaPeriod, "ema", 20, "EMA period")
	flag.IntVar(&c.rsiPeriod, "rsi", 14, "RSI period")
	flag.IntVar(&c.bbPeriod, "bb", 20, "Bollinger period")
	flag.Float64Var(&c.bbStdDev, "bb-stddev", 2.0, "Bollinger standard deviations")
	flag.IntVar(&c.atrPeriod, "atr", 14, "ATR period")
	flag.IntVar(&c.barBuffer, "bar-buffer", 5, "INDICATORS_BAR_LIMIT_BUFFER")
	flag.StringVar(&c.out, "out", "", "write JSONL here instead of stdout")
	flag.StringVar(&c.statsOut, "stats", "", "write a per-day coverage summary (JSON) here")
	flag.Parse()

	if err := run(c); err != nil {
		fmt.Fprintf(os.Stderr, "regime-snapshots: %v\n", err)
		os.Exit(1)
	}
}

func run(c config) error {
	from, err := parseTime(c.from)
	if err != nil {
		return fmt.Errorf("-from: %w", err)
	}
	to, err := parseTime(c.to)
	if err != nil {
		return fmt.Errorf("-to: %w", err)
	}
	if !to.After(from) {
		return fmt.Errorf("-to (%s) must be after -from (%s)", to, from)
	}

	ctx := context.Background()

	var store loader.ObjectStore
	switch {
	case c.archiveDir != "":
		store = loader.NewLocalObjectStore(c.archiveDir)
	case c.bucket != "":
		s, err := loader.NewAWSObjectStore(ctx, c.region, c.bucket)
		if err != nil {
			return err
		}
		store = s
	default:
		return fmt.Errorf("provide -archive (local sync dir) or -bucket")
	}

	src := loader.NewS3Archive(store, loader.ArchiveConfig{
		Bucket:      c.bucket,
		Prefix:      c.prefix,
		Concurrency: 64,
	})

	fmt.Fprintf(os.Stderr, "loading %s ...\n", src.Describe())
	trades, st, err := loader.LoadTradesOnly(ctx, src, c.book, from, to)
	if err != nil {
		return fmt.Errorf("load: %w", err)
	}
	fmt.Fprintf(os.Stderr, "%s\n", st)
	if len(trades) == 0 {
		return fmt.Errorf("no trades in window — nothing to classify")
	}

	bars := loader.AggregateBars(trades, c.barInterval)
	fmt.Fprintf(os.Stderr, "aggregated %d trades into %d %s bars\n", len(trades), len(bars), c.barInterval)

	minBars := minBarsRequired(c)
	fetchLimit := barFetchLimit(c)

	sma := indicators.NewSMA(c.smaPeriod)
	ema := indicators.NewEMA(c.emaPeriod)
	rsi := indicators.NewRSI(c.rsiPeriod)
	bb := indicators.NewBollinger(c.bbPeriod, c.bbStdDev)
	atr := indicators.NewATR(c.atrPeriod)

	w := os.Stdout
	if c.out != "" {
		f, err := os.Create(c.out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	enc := json.NewEncoder(bw)

	perDay := map[string]*dayStat{}

	emitted, skipped := 0, 0
	for sampleAt := from.Truncate(c.cadence); !sampleAt.After(to); sampleAt = sampleAt.Add(c.cadence) {
		visible := loader.BarsUpTo(bars, sampleAt)
		if len(visible) < minBars {
			skipped++
			continue
		}
		if len(visible) > fetchLimit {
			visible = visible[len(visible)-fetchLimit:]
		}

		// Mirror the live staleness guard: if the most recent closed bar is
		// older than the sample instant by more than the staleness budget, the
		// live service would have marked the book unhealthy rather than
		// classifying on frozen values. Emitting those as healthy snapshots
		// would invent regime samples during collector outages.
		lastBar := visible[len(visible)-1].Timestamp
		age := sampleAt.Sub(lastBar)
		healthy := age <= maxStaleness(c)

		closes := barCloses(visible)
		price := closes[len(closes)-1]

		line := snapshotLine{
			Book:        c.book,
			Price:       price,
			DataHealthy: healthy,
			SnapshotAt:  sampleAt.UTC().Format(time.RFC3339),
		}
		if !healthy {
			line.StaleReason = fmt.Sprintf("bar data stale: last bar %s ago", age.Round(time.Second))
		}

		if v, err := ema.Compute(closes); err == nil {
			line.EMA = v
		}
		if v, err := rsi.Compute(closes); err == nil {
			line.RSI = v
		}
		if b, err := bb.ComputeBands(closes); err == nil {
			line.BBUpper, line.BBMiddle, line.BBLower = b.Upper, b.Middle, b.Lower
		}
		if v, err := atr.ComputeFromBars(visible); err == nil {
			line.ATR = v
		}
		// SMA is computed for parity with the live cycle even though the
		// classifier does not read it; a failure here would signal that the
		// bar window is malformed.
		_, _ = sma.Compute(closes)

		if err := enc.Encode(line); err != nil {
			return err
		}
		emitted++

		day := sampleAt.UTC().Format("2006-01-02")
		ds := perDay[day]
		if ds == nil {
			ds = &dayStat{Day: day}
			perDay[day] = ds
		}
		ds.Snapshots++
		if !healthy {
			ds.Unhealthy++
		}
		if price > 0 {
			ds.observeATRPct(line.ATR / price * 100)
		}
	}

	fmt.Fprintf(os.Stderr, "emitted %d snapshots (skipped %d for insufficient bars) at cadence %s\n", emitted, skipped, c.cadence)

	if c.statsOut != "" {
		if err := writeDayStats(c.statsOut, perDay, trades, c.book); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote per-day summary to %s\n", c.statsOut)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mirrors of the unexported live helpers in indicators/bars_helpers.go
// ---------------------------------------------------------------------------

func minBarsRequired(c config) int {
	need := c.smaPeriod
	if c.bbPeriod > need {
		need = c.bbPeriod
	}
	if c.emaPeriod > need {
		need = c.emaPeriod
	}
	if c.rsiPeriod+1 > need {
		need = c.rsiPeriod + 1
	}
	if c.atrPeriod+1 > need {
		need = c.atrPeriod + 1
	}
	return need
}

func barFetchLimit(c config) int {
	limit := minBarsRequired(c) + c.barBuffer
	if limit < 30 {
		limit = 30
	}
	if limit > 500 {
		limit = 500
	}
	return limit
}

// maxStaleness scales with the bar interval. The live default (MaxStaleness)
// is tuned for 1m bars; at a wider -bar the equivalent budget is proportional.
func maxStaleness(c config) time.Duration {
	if c.barInterval <= time.Minute {
		return 5 * time.Minute
	}
	return 5 * c.barInterval
}

func barCloses(bars []indicators.OHLCV) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.Close
	}
	return out
}

// ---------------------------------------------------------------------------
// Per-day coverage summary
// ---------------------------------------------------------------------------

type dayStat struct {
	Day        string  `json:"day"`
	Trades     int     `json:"trades"`
	Snapshots  int     `json:"snapshots"`
	Unhealthy  int     `json:"unhealthy_snapshots"`
	ATRPctMin  float64 `json:"atr_pct_min"`
	ATRPctMax  float64 `json:"atr_pct_max"`
	ATRPctMean float64 `json:"atr_pct_mean"`

	atrSum float64
	atrN   int
	seeded bool
}

func (d *dayStat) observeATRPct(v float64) {
	if !d.seeded {
		d.ATRPctMin, d.ATRPctMax, d.seeded = v, v, true
	}
	if v < d.ATRPctMin {
		d.ATRPctMin = v
	}
	if v > d.ATRPctMax {
		d.ATRPctMax = v
	}
	d.atrSum += v
	d.atrN++
}

func writeDayStats(path string, perDay map[string]*dayStat, trades []indicators.Trade, book string) error {
	for _, t := range trades {
		day := t.Timestamp.UTC().Format("2006-01-02")
		ds := perDay[day]
		if ds == nil {
			ds = &dayStat{Day: day}
			perDay[day] = ds
		}
		ds.Trades++
	}

	out := make([]*dayStat, 0, len(perDay))
	for _, d := range perDay {
		if d.atrN > 0 {
			d.ATRPctMean = d.atrSum / float64(d.atrN)
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day < out[j].Day })

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"book": book, "days": out})
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
