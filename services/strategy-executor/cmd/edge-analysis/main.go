// Command edge-analysis answers a question that precedes any strategy tuning:
//
//	Is there enough price movement in this market to pay the round-trip cost?
//
// A backtest tells you whether a *particular* strategy made money. It cannot
// tell you whether the strategy was badly tuned or whether the market was
// unwinnable at the fee level you are paying. Those two situations demand
// completely different responses -- tune the signal vs. change the cost
// structure -- so distinguishing them is the highest-value analysis available.
//
// Two measurements are produced:
//
//  1. MOVE DISTRIBUTION. Over several holding horizons, the distribution of
//     absolute forward returns in bps, and the fraction of observations that
//     exceed the round-trip cost. This is the raw opportunity set. If, say,
//     only 3% of 15-minute windows move more than the cost, then a strategy
//     must be right about *which* 3% with high precision merely to break even.
//
//  2. PERFECT-FORESIGHT ORACLE. The exact optimal trading result given
//     complete knowledge of all future prices, computed by dynamic programming
//     over a proportional per-leg cost. This is a hard upper bound: no
//     strategy, however good, can beat it on this data. If the oracle is
//     barely profitable at the real fee level, no amount of signal work will
//     produce a profitable strategy and the only remaining levers are cost
//     and venue.
//
// The oracle is deliberately generous -- it assumes zero latency, unlimited
// fill at the bar close, and no market impact -- which is precisely what makes
// a *negative* oracle result conclusive.
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"bitso-trading-platform/strategy-executor/internal/backtest/loader"
	"bitso-trading-platform/strategy-executor/internal/indicators"
)

func main() {
	var (
		archiveDir = flag.String("archive", "", "local archive root (an `aws s3 sync` target)")
		bucket     = flag.String("bucket", "", "S3 bucket (ignored when -archive is set)")
		region     = flag.String("region", "us-east-1", "AWS region")
		prefix     = flag.String("prefix", "trades", "archive key prefix")
		book       = flag.String("book", "btc_mxn", "book to analyse")
		fromStr    = flag.String("from", "", "window start (RFC3339 or YYYY-MM-DD)")
		toStr      = flag.String("to", "", "window end (RFC3339 or YYYY-MM-DD)")
		barStr     = flag.String("bar", "1m", "bar interval for the oracle and move distribution")
		feeBPS     = flag.Float64("commission-bps", 65, "per-leg commission in bps")
		slipBPS    = flag.Float64("slippage-bps", 10, "per-leg slippage in bps")
	)
	flag.Parse()

	from, err := parseTime(*fromStr)
	if err != nil {
		fatalf("bad -from: %v", err)
	}
	to, err := parseTime(*toStr)
	if err != nil {
		fatalf("bad -to: %v", err)
	}
	barInterval, err := time.ParseDuration(*barStr)
	if err != nil {
		fatalf("bad -bar: %v", err)
	}

	ctx := context.Background()

	var store loader.ObjectStore
	if *archiveDir != "" {
		store = loader.NewLocalObjectStore(*archiveDir)
	} else {
		s, err := loader.NewAWSObjectStore(ctx, *region, *bucket)
		if err != nil {
			fatalf("aws store: %v", err)
		}
		store = s
	}

	src := loader.NewS3Archive(store, loader.ArchiveConfig{
		Bucket: *bucket, Prefix: *prefix, Concurrency: 64,
	})
	trades, st, err := loader.LoadTradesOnly(ctx, src, *book, from, to)
	if err != nil {
		fatalf("load: %v", err)
	}
	fmt.Printf("LOAD: %s\n\n", st)

	bars := loader.AggregateBars(trades, barInterval)
	if len(bars) < 2 {
		fatalf("not enough bars (%d)", len(bars))
	}

	roundTrip := 2 * (*feeBPS + *slipBPS)
	fmt.Printf("%s\n", rule)
	fmt.Printf("EDGE vs COST  book=%s  bars=%s (%d bars)  window=%s .. %s\n",
		*book, barInterval, len(bars),
		bars[0].Timestamp.Format(time.RFC3339), bars[len(bars)-1].Timestamp.Format(time.RFC3339))
	fmt.Printf("cost model: %.0f bps commission + %.0f bps slippage per leg => %.0f bps ROUND TRIP\n",
		*feeBPS, *slipBPS, roundTrip)
	fmt.Printf("%s\n\n", rule)

	reportMoveDistribution(bars, roundTrip)
	reportOracle(bars, *feeBPS, *slipBPS)
	reportBuyHold(bars, *feeBPS, *slipBPS)
}

const rule = "===================================================================================================="

// ---------------------------------------------------------------------------
// 1. Move distribution
// ---------------------------------------------------------------------------

var horizons = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	1 * time.Hour,
	4 * time.Hour,
	24 * time.Hour,
}

// reportMoveDistribution measures, per holding horizon, how large the available
// price moves actually are relative to the cost of capturing one.
//
// Forward returns are located by TIME, not by bar index. Empty bars are not
// emitted by AggregateBars, so "20 bars ahead" can be an hour ahead in a quiet
// stretch; indexing would silently stretch the horizon exactly where the market
// is least active.
func reportMoveDistribution(bars []indicators.OHLCV, roundTripBPS float64) {
	fmt.Println("1. AVAILABLE MOVE vs ROUND-TRIP COST")
	fmt.Println("   |forward return| in bps, by holding horizon. 'PAYS' = share of windows whose move")
	fmt.Printf("   exceeds the %.0f bps round trip -- i.e. the share that could profit even with PERFECT timing.\n\n", roundTripBPS)

	fmt.Printf("   %-8s %8s %8s %8s %8s %8s %8s %10s\n",
		"HORIZON", "N", "MEDIAN", "P75", "P90", "P99", "MAX", "PAYS")
	fmt.Printf("   %s\n", "---------------------------------------------------------------------------------")

	for _, h := range horizons {
		moves := forwardAbsMovesBPS(bars, h)
		if len(moves) == 0 {
			continue
		}
		sort.Float64s(moves)

		pays := 0
		for _, m := range moves {
			if m > roundTripBPS {
				pays++
			}
		}
		fmt.Printf("   %-8s %8d %8.1f %8.1f %8.1f %8.1f %8.1f %9.2f%%\n",
			h, len(moves),
			pct(moves, 0.50), pct(moves, 0.75), pct(moves, 0.90),
			pct(moves, 0.99), moves[len(moves)-1],
			100*float64(pays)/float64(len(moves)))
	}
	fmt.Println()
}

// forwardAbsMovesBPS returns |return| in bps from each bar to the first bar at
// least `horizon` later.
func forwardAbsMovesBPS(bars []indicators.OHLCV, horizon time.Duration) []float64 {
	var out []float64
	j := 0
	for i := range bars {
		target := bars[i].Timestamp.Add(horizon)
		if j < i {
			j = i
		}
		for j < len(bars) && bars[j].Timestamp.Before(target) {
			j++
		}
		if j >= len(bars) {
			break
		}
		if bars[i].Close <= 0 {
			continue
		}
		r := (bars[j].Close - bars[i].Close) / bars[i].Close * 10000
		out = append(out, math.Abs(r))
	}
	return out
}

func pct(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(q * float64(len(sorted)-1))
	return sorted[i]
}

// ---------------------------------------------------------------------------
// 2. Perfect-foresight oracle
// ---------------------------------------------------------------------------

// oracleResult is the outcome of the optimal-with-hindsight strategy.
type oracleResult struct {
	multiple float64 // terminal quote balance starting from 1.0
	trades   int     // completed round trips
}

// perfectForesight computes the exact maximum achievable return given full
// knowledge of every future bar close, with a proportional per-leg cost.
//
// This is the classic two-state dynamic program. Working in log space keeps it
// numerically stable over tens of thousands of bars and turns the multiplicative
// fee into an additive constant:
//
//	cash[i] = max(cash[i-1], hold[i-1] + log(P_i) + log(1-c))   // sell at bar i
//	hold[i] = max(hold[i-1], cash[i-1] - log(P_i) + log(1-c))   // buy  at bar i
//
// cash is log(quote units), hold is log(base units). Starting flat with 1 unit
// of quote, the answer is exp(cash[n-1]).
//
// The cost `c` is charged on BOTH legs, so a round trip that does not clear
// 2c is correctly rejected by the DP -- which is exactly why the trade count
// falls as fees rise.
func perfectForesight(bars []indicators.OHLCV, perLegCostBPS float64) oracleResult {
	c := perLegCostBPS / 10000
	if c >= 1 {
		return oracleResult{multiple: 1}
	}
	logKeep := math.Log(1 - c)

	cash, hold := 0.0, math.Inf(-1)
	cashTrades, holdTrades := 0, 0

	for i := range bars {
		p := bars[i].Close
		if p <= 0 {
			continue
		}
		logP := math.Log(p)

		// Evaluate both transitions against the PREVIOUS state so that a buy
		// and a sell cannot both execute on the same bar.
		sell, sellTrades := math.Inf(-1), 0
		if !math.IsInf(hold, -1) {
			sell = hold + logP + logKeep
			sellTrades = holdTrades + 1 // a round trip completes on the sell
		}
		buy := cash - logP + logKeep
		buyTrades := cashTrades

		if sell > cash {
			cash, cashTrades = sell, sellTrades
		}
		if buy > hold {
			hold, holdTrades = buy, buyTrades
		}
	}
	return oracleResult{multiple: math.Exp(cash), trades: cashTrades}
}

func reportOracle(bars []indicators.OHLCV, feeBPS, slipBPS float64) {
	fmt.Println("2. PERFECT-FORESIGHT ORACLE (hard upper bound on ANY strategy)")
	fmt.Println("   Optimal trading with complete knowledge of all future prices, by cost level.")
	fmt.Println("   No strategy can beat these numbers on this data. Zero latency, perfect fills assumed.")
	fmt.Println()

	type row struct {
		label      string
		perLegBPS  float64
		annotation string
	}
	rows := []row{
		{"frictionless", 0, "theoretical ceiling, no costs at all"},
		{"10 bps/leg", 10, "major-exchange taker (e.g. Binance-class)"},
		{"25 bps/leg", 25, "current backtest engine DEFAULT"},
		{"50 bps/leg", 50, "Bitso MAKER (post-only limit orders)"},
		{"65 bps/leg", 65, "Bitso retail TAKER"},
		{feeLabel(feeBPS, slipBPS), feeBPS + slipBPS, "as configured, incl. slippage"},
	}

	fmt.Printf("   %-16s %10s %12s %14s   %s\n", "COST LEVEL", "PER LEG", "ORACLE MAX", "ORACLE TRADES", "NOTE")
	fmt.Printf("   %s\n", "----------------------------------------------------------------------------------------------")
	for _, r := range rows {
		res := perfectForesight(bars, r.perLegBPS)
		fmt.Printf("   %-16s %9.0fb %11.2fx %14d   %s\n",
			r.label, r.perLegBPS, res.multiple, res.trades, r.annotation)
	}
	fmt.Println()
	fmt.Println("   Read this as: the oracle's return is the ENTIRE opportunity available. A realistic")
	fmt.Println("   strategy captures a small fraction of it. If the oracle is near 1.00x, the market is")
	fmt.Println("   unwinnable at that cost and signal quality is irrelevant.")
	fmt.Println()
}

func feeLabel(fee, slip float64) string {
	return fmt.Sprintf("%.0f+%.0f configured", fee, slip)
}

// ---------------------------------------------------------------------------
// 3. Buy and hold reference
// ---------------------------------------------------------------------------

// reportBuyHold gives the do-nothing benchmark. A strategy that trades hundreds
// of times and underperforms a single buy-and-hold is destroying value through
// churn, and that is worth seeing next to the oracle.
func reportBuyHold(bars []indicators.OHLCV, feeBPS, slipBPS float64) {
	first, last := bars[0].Close, bars[len(bars)-1].Close
	if first <= 0 {
		return
	}
	c := (feeBPS + slipBPS) / 10000
	gross := last / first
	net := gross * (1 - c) * (1 - c)

	fmt.Println("3. BUY AND HOLD REFERENCE (one round trip, no signal at all)")
	fmt.Printf("   entry %.2f -> exit %.2f  |  gross %.4fx (%+.2f%%)  |  net of one round trip %.4fx (%+.2f%%)\n",
		first, last, gross, (gross-1)*100, net, (net-1)*100)
	fmt.Println()
}

// ---------------------------------------------------------------------------

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "edge-analysis: "+format+"\n", args...)
	os.Exit(1)
}
