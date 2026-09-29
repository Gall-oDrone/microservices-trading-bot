// Command execution-check tests two assumptions behind the daily-bar studies
// against the bot's own WebSocket trade archive (S3, book=btc_mxn):
//
//  1. Data: do Bitso's public daily candles (cmd/bitso-daily) match daily bars
//     rebuilt from the trades we recorded ourselves?
//  2. Execution: the studies fill every trade at the day's OPEN and charge a
//     flat 10 bps of slippage per leg, and the "maker" scenarios assume every
//     resting order fills. What does filling at the open actually cost?
//
// Usage (local `aws s3 sync` copy of s3://<archive-bucket>/trades):
//
//	go run ./cmd/execution-check -archive ./archive -candles ./bitso/btc_mxn_daily.csv
//
// Execution is measured from trade prints only (the archive has no order
// book), so every figure is for SMALL orders -- roughly the size of a typical
// print. Methods, per full Mexico-City day D with open time T0:
//
//   - Market order: VWAP of taker-buy (resp. taker-sell) prints in
//     [T0, T0+window) versus the candle open. Averaged over days, price drift
//     cancels and what remains is the half-spread a small market order pays.
//   - Maker order: a limit at the open price placed at T0. It counts as filled
//     by time T if a taker print TRADES THROUGH the limit (strictly below for a
//     buy, strictly above for a sell). Trade-through fills no matter where the
//     order sat in the queue, so this is the conservative fill rate; "touch"
//     (at the limit) is reported as the optimistic bound.
//   - Maker-then-chase: rest as maker; if not filled by the deadline, cross at
//     the last print. Its average per-leg cost (fee + price paid versus the
//     open) is the realistic replacement for the studies' flat assumptions.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/strategy-executor/internal/backtest/loader"
)

type candle struct {
	Date                   string
	Start                  time.Time
	Open, High, Low, Close float64
	Volume                 float64
	Trades                 int64
}

type print struct {
	TS     time.Time
	Price  float64
	Amount float64
	Taker  string // "buy" or "sell"
}

const day = 24 * time.Hour

func main() {
	archive := flag.String("archive", "", "local sync of the archive root (the dir that contains trades/)")
	prefix := flag.String("prefix", "trades", "archive prefix")
	candlesPath := flag.String("candles", "", "cmd/bitso-daily CSV for the same book")
	book := flag.String("book", "btc_mxn", "book")
	makerBPS := flag.Float64("maker-bps", 60, "maker fee bps")
	takerBPS := flag.Float64("taker-bps", 78, "taker fee bps")
	mktWindow := flag.Duration("market-window", 15*time.Minute, "window after the open for market-order VWAP (the book is thin at Mexico-City midnight; 5m often has no print)")
	chase := flag.Duration("chase-after", time.Hour, "maker-then-chase: cross if unfilled after this long")
	gapWarn := flag.Duration("gap-warn", time.Hour, "flag days containing a trade-free stretch longer than this (20-50 min quiet spells at night are normal)")
	flag.Parse()
	if *archive == "" || *candlesPath == "" {
		fail(fmt.Errorf("-archive and -candles are required"))
	}

	cs, err := loadCandles(*candlesPath)
	if err != nil {
		fail(err)
	}
	src := loader.NewS3Archive(loader.NewLocalObjectStore(*archive), loader.ArchiveConfig{Prefix: *prefix, Concurrency: 64})
	from := cs[0].Start
	to := cs[len(cs)-1].Start.Add(day)
	rows, st, err := src.LoadTrades(context.Background(), *book, from, to)
	if err != nil {
		fail(err)
	}
	ps := toPrints(rows)
	if len(ps) == 0 {
		fail(fmt.Errorf("no trades in archive for %s", *book))
	}
	fmt.Printf("ARCHIVE : %s\n", st.String())
	fmt.Printf("COVERAGE: %s .. %s (%d prints)\n\n", ps[0].TS.Format(time.RFC3339), ps[len(ps)-1].TS.Format(time.RFC3339), len(ps))

	days := fullDays(cs, ps[0].TS, ps[len(ps)-1].TS)
	if len(days) == 0 {
		fail(fmt.Errorf("no full candle day inside the archive coverage"))
	}
	reportData(days, ps, *gapWarn)
	reportExecution(days, ps, *mktWindow, *chase, *makerBPS, *takerBPS)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "execution-check: %v\n", err)
	os.Exit(1)
}

// ---------------------------------------------------------------------------
// Part 1: data agreement
// ---------------------------------------------------------------------------

type dayBar struct {
	Open, High, Low, Close, Volume float64
	N                              int
	MaxGap                         time.Duration
}

// barFor rebuilds [start, start+24h) from prints (sorted by time).
func barFor(ps []print, start time.Time) (dayBar, bool) {
	lo := sort.Search(len(ps), func(i int) bool { return !ps[i].TS.Before(start) })
	hi := sort.Search(len(ps), func(i int) bool { return !ps[i].TS.Before(start.Add(day)) })
	if lo >= hi {
		return dayBar{}, false
	}
	b := dayBar{Open: ps[lo].Price, Close: ps[hi-1].Price, High: ps[lo].Price, Low: ps[lo].Price}
	prev := start
	for _, p := range ps[lo:hi] {
		b.High = math.Max(b.High, p.Price)
		b.Low = math.Min(b.Low, p.Price)
		b.Volume += p.Amount
		b.N++
		if g := p.TS.Sub(prev); g > b.MaxGap {
			b.MaxGap = g
		}
		prev = p.TS
	}
	if g := start.Add(day).Sub(prev); g > b.MaxGap {
		b.MaxGap = g
	}
	return b, true
}

func bps(a, b float64) float64 { return (a/b - 1) * 1e4 }

func reportData(days []candle, ps []print, gapWarn time.Duration) {
	fmt.Println("=== 1. Bitso daily candles vs daily bars rebuilt from our archived trades ===")
	fmt.Printf("%-10s %8s %8s %8s %8s %9s %9s %8s\n", "day", "open bp", "high bp", "low bp", "close bp", "trades %", "volume %", "max gap")
	var dO, dH, dL, dC, rN, rV []float64
	flagged := 0
	for _, c := range days {
		b, ok := barFor(ps, c.Start)
		if !ok {
			fmt.Printf("%-10s  no archived trades\n", c.Date)
			continue
		}
		o, h, l, cl := bps(b.Open, c.Open), bps(b.High, c.High), bps(b.Low, c.Low), bps(b.Close, c.Close)
		n := float64(b.N) / float64(c.Trades) * 100
		v := b.Volume / c.Volume * 100
		mark := ""
		if b.MaxGap > gapWarn {
			mark = "  <- gap"
			flagged++
		}
		fmt.Printf("%-10s %8.1f %8.1f %8.1f %8.1f %8.1f%% %8.1f%% %8s%s\n", c.Date, o, h, l, cl, n, v, b.MaxGap.Round(time.Minute), mark)
		dO, dH, dL, dC, rN, rV = append(dO, math.Abs(o)), append(dH, math.Abs(h)), append(dL, math.Abs(l)), append(dC, math.Abs(cl)), append(rN, n), append(rV, v)
	}
	fmt.Printf("\nmedian |diff| bps: open %.1f  high %.1f  low %.1f  close %.1f | median share of Bitso's trades %.1f%%, volume %.1f%%\n",
		median(dO), median(dH), median(dL), median(dC), median(rN), median(rV))
	fmt.Printf("max |diff| bps   : open %.1f  high %.1f  low %.1f  close %.1f | days with a trade-free stretch > %s: %d of %d\n\n",
		max(dO), max(dH), max(dL), max(dC), gapWarn, flagged, len(dO))
}

// ---------------------------------------------------------------------------
// Part 2: execution at the open
// ---------------------------------------------------------------------------

func reportExecution(days []candle, ps []print, mktWindow, chase time.Duration, makerBPS, takerBPS float64) {
	fmt.Println("=== 2. Executing at the daily open (small orders; trade prints only) ===")
	var buySlip, sellSlip, sizes []float64
	horizons := []time.Duration{5 * time.Minute, 15 * time.Minute, time.Hour, 4 * time.Hour}
	fillThrough := map[time.Duration]int{}
	fillTouch := map[time.Duration]int{}
	legs := 0
	var chaseCost []float64
	chaseFilled := 0
	for _, c := range days {
		lo := sort.Search(len(ps), func(i int) bool { return !ps[i].TS.Before(c.Start) })
		hi := sort.Search(len(ps), func(i int) bool { return !ps[i].TS.Before(c.Start.Add(day)) })
		dayPs := ps[lo:hi]
		if len(dayPs) == 0 {
			continue
		}
		if v, ok := vwap(dayPs, c.Start, c.Start.Add(mktWindow), "buy"); ok {
			buySlip = append(buySlip, bps(v, c.Open))
		}
		if v, ok := vwap(dayPs, c.Start, c.Start.Add(mktWindow), "sell"); ok {
			sellSlip = append(sellSlip, -bps(v, c.Open))
		}
		for _, p := range dayPs {
			sizes = append(sizes, p.Amount)
		}
		for _, side := range []string{"buy", "sell"} {
			legs++
			for _, h := range horizons {
				if makerFilled(dayPs, c.Start, c.Start.Add(h), c.Open, side, true) {
					fillThrough[h]++
				}
				if makerFilled(dayPs, c.Start, c.Start.Add(h), c.Open, side, false) {
					fillTouch[h]++
				}
			}
			cost, filled := makerThenChase(dayPs, c.Start, chase, c.Open, side, makerBPS, takerBPS)
			chaseCost = append(chaseCost, cost)
			if filled {
				chaseFilled++
			}
		}
	}
	fmt.Printf("days: %d | typical print size: median %.5f BTC, p90 %.5f BTC\n\n", len(days), median(sizes), quantile(sizes, 0.9))
	fmt.Printf("Market order, VWAP of first %s vs candle open (positive = worse than the open):\n", mktWindow)
	fmt.Printf("  buy : mean %+6.1f bps  median %+6.1f bps  (n=%d)\n", mean(buySlip), median(buySlip), len(buySlip))
	fmt.Printf("  sell: mean %+6.1f bps  median %+6.1f bps  (n=%d)\n", mean(sellSlip), median(sellSlip), len(sellSlip))
	fmt.Printf("  => cost per leg of a small market order beyond the fee ~ %.1f bps (the studies assume 10)\n\n", (mean(buySlip)+mean(sellSlip))/2)
	fmt.Printf("Maker limit at the open price, share of %d legs (buy+sell) filled by:\n", legs)
	for _, h := range horizons {
		fmt.Printf("  %-5s trade-through %5.1f%%   touch %5.1f%%\n", h, pct(fillThrough[h], legs), pct(fillTouch[h], legs))
	}
	fmt.Printf("\nMaker-then-chase (rest at the open price; cross at the last print if unfilled after %s):\n", chase)
	fmt.Printf("  filled as maker %.1f%% of legs; average cost per leg incl. fee %.1f bps (median %.1f)\n", pct(chaseFilled, legs), mean(chaseCost), median(chaseCost))
	fmt.Printf("  compare: studies' maker+slippage %.0f bps, taker+slippage %.0f bps per leg\n", makerBPS+10, takerBPS+10)
}

func vwap(ps []print, from, to time.Time, taker string) (float64, bool) {
	var pv, v float64
	for _, p := range ps {
		if p.TS.Before(from) {
			continue
		}
		if !p.TS.Before(to) {
			break
		}
		if p.Taker == taker {
			pv += p.Price * p.Amount
			v += p.Amount
		}
	}
	if v == 0 {
		return 0, false
	}
	return pv / v, true
}

// makerFilled: a resting BUY at limit fills when a taker SELL prints below
// (through) or at-or-below (touch) the limit; mirror for a resting SELL.
func makerFilled(ps []print, from, to time.Time, limit float64, side string, through bool) bool {
	for _, p := range ps {
		if p.TS.Before(from) {
			continue
		}
		if !p.TS.Before(to) {
			return false
		}
		switch side {
		case "buy":
			if p.Taker == "sell" && (p.Price < limit || (!through && p.Price == limit)) {
				return true
			}
		case "sell":
			if p.Taker == "buy" && (p.Price > limit || (!through && p.Price == limit)) {
				return true
			}
		}
	}
	return false
}

// makerThenChase returns the per-leg cost in bps versus the open (fee plus
// adverse price) and whether the maker order filled (conservatively, by
// trade-through).
func makerThenChase(ps []print, start time.Time, chase time.Duration, open float64, side string, makerBPS, takerBPS float64) (float64, bool) {
	deadline := start.Add(chase)
	if makerFilled(ps, start, deadline, open, side, true) {
		return makerBPS, true
	}
	last := open
	for _, p := range ps {
		if !p.TS.Before(deadline) {
			break
		}
		last = p.Price
	}
	adverse := bps(last, open)
	if side == "sell" {
		adverse = -adverse
	}
	return takerBPS + adverse, false
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func toPrints(rows []loader.ArchiveTrade) []print {
	out := make([]print, 0, len(rows))
	for _, r := range rows {
		t := r.TakerSide()
		if t == "" || r.Price <= 0 {
			continue
		}
		out = append(out, print{TS: r.ExchangeTS, Price: r.Price, Amount: r.Amount, Taker: t})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	return out
}

// fullDays keeps candles whose whole 24h lies inside the archive coverage.
func fullDays(cs []candle, first, last time.Time) []candle {
	var out []candle
	for _, c := range cs {
		if !c.Start.Before(first) && !c.Start.Add(day).After(last) {
			out = append(out, c)
		}
	}
	return out
}

func loadCandles(path string) ([]candle, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(recs) < 2 {
		return nil, fmt.Errorf("%s: no rows", path)
	}
	idx := map[string]int{}
	for i, h := range recs[0] {
		idx[strings.TrimSpace(h)] = i
	}
	for _, k := range []string{"date", "open", "high", "low", "close", "volume", "trade_count", "bucket_start_utc"} {
		if _, ok := idx[k]; !ok {
			return nil, fmt.Errorf("%s: missing column %q (expected cmd/bitso-daily output)", path, k)
		}
	}
	var out []candle
	for _, r := range recs[1:] {
		st, err := time.Parse(time.RFC3339, r[idx["bucket_start_utc"]])
		if err != nil {
			return nil, err
		}
		f := func(k string) float64 { v, _ := strconv.ParseFloat(r[idx[k]], 64); return v }
		n, _ := strconv.ParseInt(r[idx["trade_count"]], 10, 64)
		out = append(out, candle{Date: r[idx["date"]], Start: st, Open: f("open"), High: f("high"), Low: f("low"),
			Close: f("close"), Volume: f("volume"), Trades: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d) * 100
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func quantile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[int(q*float64(len(s)-1))]
}

func median(xs []float64) float64 { return quantile(xs, 0.5) }

func max(xs []float64) float64 {
	m := math.Inf(-1)
	for _, x := range xs {
		m = math.Max(m, x)
	}
	return m
}
