// Command etoro-risk-report is the risk and analytics report of the eToro
// index-CFD executor (porting plan phase P4): VaR / ES / stress on NYSE
// trading days for the demo positions, a CFD-priced Monte Carlo of the
// frozen rule, P&L attribution with financing, and fill quality of the demo
// orders, from files the executor and cmd/etoro-reconcile already write.
// It is the eToro sibling of the Bitso monitor's risk metrics
// (shared/pkg/varmodel, shared/pkg/montecarlo) built on shared/pkg/cfdrisk.
//
//	go run ./cmd/etoro-risk-report -ledger ./etoro-daily-data/demo/ledger.jsonl \
//	  -candles ./etoro-daily-data/demo/candles \
//	  -yahoo ../../docs/backtest-readiness/evidence-2026-10-10/data \
//	  -reconcile ./etoro-daily-data/demo/reconcile \
//	  -size 1100 -capital 100000 -paths 2000 -seed 1 \
//	  -out ./etoro-daily-data/demo/risk/2026-10-13
//
// It writes risk.json (schema cfdrisk-report/v1) and risk.md to -out and
// prints risk.md. It reads the ledger, the newest <book>_daily_<date>.csv
// per book, the Yahoo index histories (optional: without them the stress
// episodes come from the paths embedded in cfdrisk and the Monte Carlo
// resamples eToro's own ~950 bars only) and the reconcile reports
// (optional: financing_to_date and portfolio lag). It never writes any of
// them and makes no network call.
//
// Exit status: 0, unless an input is unreadable (1) or the usage is wrong
// (2). It is a report, not a gate: a yellow backtest or a stress loss is
// printed, not enforced.
//
// Costs default to the pre-registration's frozen values and carry a
// costs_status label, as cmd/index-research does. The measured in-session
// costs of 2026-10-12 (0.2-0.4 bps of spread per open) are a second Monte
// Carlo scenario, never the frozen one.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bitso-trading-platform/shared/pkg/mktcal"
)

func main() {
	var o options
	flag.StringVar(&o.ledger, "ledger", "./etoro-daily-data/demo/ledger.jsonl", "the executor's demo ledger (read only)")
	flag.StringVar(&o.candles, "candles", "", "directory of <book>_daily_<date>.csv (default: <ledger dir>/candles)")
	flag.StringVar(&o.yahoo, "yahoo", "", "directory of ndx_yahoo_daily.csv / gspc_yahoo_daily.csv (optional)")
	flag.StringVar(&o.reconcile, "reconcile", "", "directory of reconcile reports <date>.json (optional)")
	flag.StringVar(&o.out, "out", "", "output directory for risk.json and risk.md (default: <ledger dir>/risk/<as-of>)")
	flag.StringVar(&o.asOf, "as-of", "", "New York date YYYY-MM-DD the report is as of (default: today in New York)")
	flag.Float64Var(&o.size, "size", 1100, "position size, USD (the ETF fixed fee is relative to it)")
	flag.Float64Var(&o.capital, "capital", 100000, "capital for the reverse stress, USD (the demo account's)")
	flag.IntVar(&o.paths, "paths", 2000, "Monte Carlo paths per scenario")
	flag.Uint64Var(&o.seed, "seed", 1, "Monte Carlo seed")
	flag.IntVar(&o.horizon, "mc-horizon", 252, "Monte Carlo horizon, trading days")
	flag.IntVar(&o.block, "mc-block", 20, "Monte Carlo mean block, days")
	flag.StringVar(&o.mcSince, "mc-since", "", "resample moves from this date on (YYYY-MM-DD; default: all history)")
	flag.Float64Var(&o.nsdqSpread, "spread-bps-nsdq100", 15.5, "frozen NSDQ100 round-trip spread, bps")
	flag.Float64Var(&o.spxSpread, "spread-bps-spx500", 14.8, "frozen SPX500 round-trip spread, bps")
	flag.Float64Var(&o.overnight, "overnight-bps", 2.3, "frozen overnight fee per calendar night at x1, bps")
	flag.Float64Var(&o.measuredSpread, "measured-spread-bps", 0.5, "measured in-session round-trip spread, bps (second Monte Carlo scenario)")
	flag.Float64Var(&o.etfFee, "etf-fee", 1.5, "ETF benchmark commission per order, USD")
	flag.StringVar(&o.costsStatus, "costs-status", "FROZEN (pre-registration §3: the provisional weekend-capture values of 2026-10-10 became final)", "label carried with the cost block")
	flag.Parse()

	if o.ledger == "" {
		usage("-ledger is required")
	}
	if o.candles == "" {
		o.candles = filepath.Join(filepath.Dir(o.ledger), "candles")
	}
	now := time.Now().UTC()
	if o.asOf == "" {
		o.asOf = now.In(mktcal.NewYork).Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", o.asOf); err != nil {
		usage("-as-of: " + err.Error())
	}
	if o.out == "" {
		o.out = filepath.Join(filepath.Dir(o.ledger), "risk", o.asOf)
	}
	if o.paths <= 0 || o.size <= 0 {
		usage("-paths and -size must be positive")
	}

	rep, err := run(o, now)
	if err != nil {
		fail(err)
	}
	md := renderMarkdown(rep)
	if err := writeOutputs(o.out, rep, md); err != nil {
		fail(err)
	}
	fmt.Print(md)
	fmt.Printf("\nwrote %s and %s\n", filepath.Join(o.out, "risk.json"), filepath.Join(o.out, "risk.md"))
}

func usage(msg string) {
	fmt.Fprintf(os.Stderr, "etoro-risk-report: %s\n", msg)
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "etoro-risk-report: %v\n", err)
	os.Exit(1)
}
