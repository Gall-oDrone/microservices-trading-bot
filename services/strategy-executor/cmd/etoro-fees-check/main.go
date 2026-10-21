// Command etoro-fees-check answers the question left pending in
// docs/backtest-readiness/FORWARD-TEST-RESULTS-SMA50-INDEX-CFD-2026-10-19.md
// §3.2: does the eToro demo account charge overnight financing, and if so
// where does it book it? Over eleven position-nights the cash balance moved
// exactly by the price P&L; the pre-registration's frozen model expects
// 2.3 bps per calendar night.
//
// It reads the demo trading history (`fees` per closed position) and the
// open positions (`totalFees`), labels the executor's and the console's
// positions from the ledger, and prices every row with the frozen model for
// the same nights. It writes fees.json and fees.md to -out and prints the
// Markdown. Read calls only: no order, no PATCH, nothing written to the
// ledger.
//
//	go run ./cmd/etoro-fees-check -ledger ./etoro-daily-data/demo/ledger.jsonl \
//	  -positions 3616683146,3616683295 -out ../../docs/etoro/evidence-$(date +%F)
//
// Exit status: 0 on a verdict (whatever it is), 1 when the venue could not be
// read, 2 on bad usage. The verdict never changes the frozen costs: the
// pre-registration says the frozen figures decide; this turns an assumed
// cost into a measured one.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/broker/etorobroker"
	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/strategy-executor/internal/stageenv"
)

func main() {
	ledger := flag.String("ledger", "./etoro-daily-data/demo/ledger.jsonl", "the executor's demo ledger (read only; labels the rows)")
	out := flag.String("out", "", "directory for fees.json and fees.md (default: print only)")
	positions := flag.String("positions", "", "comma-separated position ids to report (default: every closed position since -since and every open one)")
	sinceDays := flag.Int("since-days", 60, "how far back to read the trading history (eToro allows < 1 year)")
	overnight := flag.Float64("overnight-bps", 2.3, "frozen overnight fee per calendar night at x1, bps of the position")
	envFile := flag.String("env-file", defaultEnvFile(), "file with ETORO_PUBLIC_KEY / ETORO_PRIVATE_KEY (chmod 600)")
	flag.Parse()

	if *envFile != "" {
		if err := stageenv.LoadFile(*envFile); err != nil {
			die(err)
		}
	}
	env, err := etoro.ParseEnvironment(strings.ToLower(strings.TrimSpace(os.Getenv("ETORO_ENV"))))
	if err != nil || env != etoro.EnvDemo {
		usage(fmt.Sprintf("ETORO_ENV must be demo (got %q): this tool reads the demo account only", os.Getenv("ETORO_ENV")))
	}
	c, err := etoro.NewClient(etoro.Config{PublicKey: os.Getenv("ETORO_PUBLIC_KEY"), PrivateKey: os.Getenv("ETORO_PRIVATE_KEY"), Env: etoro.EnvDemo})
	if err != nil {
		die(err)
	}
	var recs []etoroledger.Record
	if *ledger != "" {
		if recs, err = etoroledger.ReadFile(*ledger); err != nil {
			die(fmt.Errorf("ledger: %w", err))
		}
	}
	only := map[string]bool{}
	for _, id := range strings.Split(*positions, ",") {
		if id = strings.TrimSpace(id); id != "" {
			only[id] = true
		}
	}
	now := time.Now().UTC()
	rep, err := check(context.Background(), etorobroker.New(c), recs, now.AddDate(0, 0, -*sinceDays), *overnight, now, only)
	if err != nil {
		die(err)
	}
	md := markdown(rep)
	fmt.Print(md)
	if *out != "" {
		if err := os.MkdirAll(*out, 0o755); err != nil {
			die(err)
		}
		b, _ := json.MarshalIndent(rep, "", "  ")
		if err := os.WriteFile(filepath.Join(*out, "fees.json"), append(b, '\n'), 0o644); err != nil {
			die(err)
		}
		if err := os.WriteFile(filepath.Join(*out, "fees.md"), []byte(md), 0o644); err != nil {
			die(err)
		}
		fmt.Printf("\nwrote %s and %s\n", filepath.Join(*out, "fees.json"), filepath.Join(*out, "fees.md"))
	}
}

func defaultEnvFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "microservices-trading-bot", "etoro-demo.env")
}

func usage(msg string) {
	fmt.Fprintf(os.Stderr, "etoro-fees-check: %s\n", msg)
	os.Exit(2)
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "etoro-fees-check: %v\n", err)
	os.Exit(1)
}
