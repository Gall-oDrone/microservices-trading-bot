// Command daily-reconcile checks the daily-executor's stage ledger against
// Bitso stage: every leg's trades (quantity, notional, fees, base change,
// order ids), no trades on days without a leg, each book's position against
// its legs, and the BTC balance against both books' positions (plan
// §6.4.10, internal/reconcile).
//
//	go run ./cmd/daily-reconcile -ledger ./stage/ledger.jsonl
//	go run ./cmd/daily-reconcile -ledger ./stage/ledger.jsonl -out ./stage/reconcile.json
//
// It only reads: the client is used through reconcile.Source, which has no
// order methods, and bitsostage.New refuses any base URL but stage.
// Credentials are loaded like the executor's (-env-file, chmod 600). Run it
// after the day's executor run has finished; a leg still resting shows as
// unrecorded fills.
//
// Exit status: 0 when everything reconciles, 3 when there is any break, 1
// on errors (ledger, credentials, Bitso), 2 on bad usage.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/strategy-executor/internal/reconcile"
	"bitso-trading-platform/strategy-executor/internal/stageenv"
)

func main() {
	ledger := flag.String("ledger", "./daily-executor-data/ledger.jsonl", "the executor's stage ledger (JSONL)")
	envFile := flag.String("env-file", stageenv.DefaultFile(), "file with STAGE_BITSO_API_KEY / STAGE_BITSO_API_SECRET (chmod 600)")
	out := flag.String("out", "", "also write the JSON report here (written atomically)")
	flag.Parse()
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	os.Exit(run(*ledger, *envFile, *out, os.Stdout, os.Stderr))
}

func run(ledgerPath, envFile, outPath string, stdout, stderr io.Writer) int {
	recs, err := dailyledger.ReadFile(ledgerPath)
	if err != nil {
		fmt.Fprintln(stderr, "daily-reconcile:", err)
		return 1
	}
	if envFile != "" {
		if err := stageenv.LoadFile(envFile); err != nil {
			fmt.Fprintln(stderr, "daily-reconcile:", err)
			return 1
		}
	}
	c, err := stageenv.Client()
	if err != nil {
		fmt.Fprintln(stderr, "daily-reconcile:", err)
		return 1
	}
	return report(c, recs, time.Now(), outPath, stdout, stderr)
}

// report runs the reconciliation and prints it; split from run so tests use
// a fake source.
func report(src reconcile.Source, recs []dailyledger.Record, now time.Time, outPath string, stdout, stderr io.Writer) int {
	rep, err := reconcile.Run(src, recs, now)
	if err != nil {
		fmt.Fprintln(stderr, "daily-reconcile:", err)
		return 1
	}
	for _, l := range rep.Legs {
		fmt.Fprintf(stdout, "leg   %-8s %s %-4s %-9s", l.Book, l.FillDate, l.Side, l.Status)
		for _, d := range l.Diffs {
			fmt.Fprintf(stdout, " | %s", d)
		}
		fmt.Fprintln(stdout)
	}
	for _, d := range rep.Days {
		if d.Status != reconcile.StatusClean {
			fmt.Fprintf(stdout, "day   %-8s %s %s: %d trades (%.8f BTC) the ledger does not record\n", d.Book, d.FillDate, d.Reason, d.Trades, d.BaseBTC)
		}
	}
	for _, p := range rep.Positions {
		fmt.Fprintf(stdout, "pos   %-8s ledger %.8f, sum of legs %.8f, ok=%v\n", p.Book, p.LedgerBTC, p.SumDeltaBTC, p.OK)
	}
	for _, b := range rep.Balances {
		fmt.Fprintf(stdout, "bal   %-8s %.8f available, %.8f held by the books, ok=%v\n", b.Currency, b.Balance, b.Required, b.OK)
	}
	fmt.Fprintf(stdout, "reconcile: %d legs, %d leg-less days checked, %d breaks\n", len(rep.Legs), len(rep.Days), rep.Breaks)
	if outPath != "" {
		if err := writeJSON(outPath, rep); err != nil {
			fmt.Fprintln(stderr, "daily-reconcile:", err)
			return 1
		}
	}
	if !rep.OK {
		return 3
	}
	return 0
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
