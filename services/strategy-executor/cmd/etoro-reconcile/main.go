// Command etoro-reconcile checks the eToro executor's demo ledger against the
// demo account (porting plan P3): every position the ledger holds is open
// with the same units, no other position exists on the executor's
// instruments, every executed close is in the trading history, and every
// order in the intent journal reached the ledger. It is read-only: it never
// writes the ledger and never places an order.
//
//	go run ./cmd/etoro-reconcile -ledger ./etoro-daily-data/demo/ledger.jsonl
//	go run ./cmd/etoro-reconcile -ledger … -json report.json
//
// Real-account canary (2026-10-22, docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md
// §8.4): with -real-canary and the canary's env file (ETORO_ENV=real) the
// same checks run against the real account and the canary ledger (which
// must live under a real-canary/ directory). -record-venue-close is the
// one write this command can make: a ledger position the account no longer
// holds and the trading history lists is recorded as an action venue_close
// line (stopped_out / took_profit / venue_close, with the gap), the line the
// executor's run writes on the demo cells and nothing writes on the canary.
//
//	go run ./cmd/etoro-reconcile -real-canary -ledger ./etoro-daily-data/real-canary/ledger.jsonl \
//	  -env-file ~/.config/microservices-trading-bot/etoro-real-canary.env -record-venue-close -by diego
//
// Exit status: 0 clean (warnings allowed), 1 drift found, 2 error. The P3
// exit criterion is five consecutive trading days of clean reports.
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
	ledger := flag.String("ledger", "./etoro-daily-data/demo/ledger.jsonl", "the executor's demo ledger")
	jsonOut := flag.String("json", "", "also write the report as JSON to this file")
	envFile := flag.String("env-file", defaultEnvFile(), "file with ETORO_PUBLIC_KEY / ETORO_PRIVATE_KEY (chmod 600)")
	realCanary := flag.Bool("real-canary", false, "reconcile the REAL account's measurement canary: the env file must say ETORO_ENV=real and -ledger must be under a real-canary/ directory")
	recordVC := flag.Bool("record-venue-close", false, "write an action venue_close ledger line for a position the ledger holds, the account does not, and the trading history lists (the canary has no executor run to do it); the only write this command makes")
	by := flag.String("by", "", "who records the venue close (with -record-venue-close)")
	reason := flag.String("reason", "venue close recorded from the trading history by etoro-reconcile", "why (with -record-venue-close)")
	flag.Parse()

	if *envFile != "" {
		if err := stageenv.LoadFile(*envFile); err != nil {
			die(err)
		}
	}
	env, err := etoro.ParseEnvironment(strings.ToLower(strings.TrimSpace(os.Getenv("ETORO_ENV"))))
	switch {
	case err != nil:
		die(err)
	case *realCanary && env != etoro.EnvReal:
		die(fmt.Errorf("-real-canary needs an env file with ETORO_ENV=real (got %q)", os.Getenv("ETORO_ENV")))
	case *realCanary && !canaryLedger(*ledger):
		die(fmt.Errorf("-real-canary needs the canary ledger (a directory named real-canary), got %s", *ledger))
	case !*realCanary && env != etoro.EnvDemo:
		die(fmt.Errorf("ETORO_ENV must be demo (got %q) unless -real-canary is given", os.Getenv("ETORO_ENV")))
	case *recordVC && strings.TrimSpace(*by) == "":
		die(fmt.Errorf("-record-venue-close needs -by (who records it)"))
	}
	c, err := etoro.NewClient(etoro.Config{PublicKey: os.Getenv("ETORO_PUBLIC_KEY"), PrivateKey: os.Getenv("ETORO_PRIVATE_KEY"), Env: env})
	if err != nil {
		die(err)
	}
	b := etorobroker.New(c)
	if *recordVC {
		positions, perr := b.Positions(context.Background())
		if perr != nil {
			die(fmt.Errorf("positions: %w", perr))
		}
		outs, verr := recordVenueCloses(context.Background(), b, *ledger, positions, time.Now(), string(env), "etoro-reconcile", strings.TrimSpace(*by), strings.TrimSpace(*reason))
		for _, o := range outs {
			fmt.Printf("venue close %-8s position %s: recorded=%v %s %s\n", o.Book, o.PositionID, o.Recorded, o.Kind, o.Note)
		}
		if verr != nil {
			die(verr)
		}
		if len(outs) == 0 {
			fmt.Println("venue close: nothing to record (every ledger position is held, or the ledger is flat)")
		}
	}
	recs, err := etoroledger.ReadFile(*ledger)
	if err != nil {
		die(err)
	}
	intents, err := etoroledger.Intents(etoroledger.IntentDir(*ledger))
	if err != nil {
		die(err)
	}
	rep, err := reconcile(context.Background(), b, recs, intents, time.Now())
	if err != nil {
		die(err)
	}
	rep.Ledger = *ledger
	rep.Env = string(env)
	printReport(rep)
	if *jsonOut != "" {
		b, _ := json.MarshalIndent(rep, "", "  ")
		if err := os.MkdirAll(filepath.Dir(*jsonOut), 0o755); err != nil {
			die(err)
		}
		if err := os.WriteFile(*jsonOut, append(b, '\n'), 0o644); err != nil {
			die(err)
		}
	}
	if !rep.Clean {
		os.Exit(1)
	}
}

func printReport(r report) {
	status := "CLEAN"
	if !r.Clean {
		status = "DRIFT"
	}
	fmt.Printf("etoro-reconcile %s | %s | %s%s\n", r.GeneratedAt, r.Ledger, status, envTag(r.Env))
	if r.PositionReads > 1 || r.HistoryReads > 1 {
		fmt.Printf("  settled    : portfolio after %d reads, history after %d (%.0f s)\n", r.PositionReads, r.HistoryReads, r.SettleWaitedS)
	}
	if a := r.Account; a != nil {
		fmt.Printf("  account    : %s cash %.2f equity %.2f\n", a.Currency, a.Cash, a.Equity)
	}
	for _, b := range r.Books {
		fmt.Printf("  %-8s   : ledger %s %s (last bar %s, %d orders checked) | account %v | financing %.2f\n",
			b.Book, b.LedgerState, b.LedgerPositionID, b.LastBarDate, b.OrdersChecked, b.AccountPositions, b.FinancingToDate)
	}
	for _, f := range r.Findings {
		fmt.Printf("  %-5s %-8s %s: %s\n", strings.ToUpper(f.Severity), f.Book, f.Code, f.Message)
	}
}

func defaultEnvFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "microservices-trading-bot", "etoro-demo.env")
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "etoro-reconcile: %v\n", err)
	os.Exit(2)
}
