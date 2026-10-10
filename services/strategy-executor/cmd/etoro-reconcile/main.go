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
	flag.Parse()

	if *envFile != "" {
		if err := stageenv.LoadFile(*envFile); err != nil {
			die(err)
		}
	}
	env, err := etoro.ParseEnvironment(strings.ToLower(strings.TrimSpace(os.Getenv("ETORO_ENV"))))
	if err != nil || env != etoro.EnvDemo {
		die(fmt.Errorf("ETORO_ENV must be demo (got %q)", os.Getenv("ETORO_ENV")))
	}
	c, err := etoro.NewClient(etoro.Config{PublicKey: os.Getenv("ETORO_PUBLIC_KEY"), PrivateKey: os.Getenv("ETORO_PRIVATE_KEY"), Env: etoro.EnvDemo})
	if err != nil {
		die(err)
	}
	recs, err := etoroledger.ReadFile(*ledger)
	if err != nil {
		die(err)
	}
	intents, err := etoroledger.Intents(etoroledger.IntentDir(*ledger))
	if err != nil {
		die(err)
	}
	rep, err := reconcile(context.Background(), etorobroker.New(c), recs, intents, time.Now())
	if err != nil {
		die(err)
	}
	rep.Ledger = *ledger
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
	fmt.Printf("etoro-reconcile %s | %s | %s\n", r.GeneratedAt, r.Ledger, status)
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
