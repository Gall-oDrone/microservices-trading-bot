// Command etoro-canary-observe is the measurement instrument of the
// real-account canary (docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md §3.5,
// built 2026-10-22). It reads the account, the open positions (with the
// fees eToro has charged them so far), the trading history since the
// canary's first open and the cost preview for the held size, and appends
// one line to observations.jsonl next to the canary ledger: the day's
// delta in fees per position (the financing measurement, M1 / M2), the
// history rows that appeared since the previous observation (the history's
// lag, M4), the account's cash and equity (M7).
//
// It is read-only by construction: the only eToro calls are GET pnl, GET
// history, POST costs (a what-if preview), GET rates and GET eligibility.
// It never sends an order, never edits a position and never writes the
// ledger. It has no trading authority and no cron ever gives the executor
// the real account; this command is the only thing that runs on a schedule
// against it (18:00 New York daily, weekends included: eToro books
// overnight fees at 17:00 New York).
//
//	# real canary (the default paths)
//	etoro-canary-observe
//	# rehearsal on the demo account
//	etoro-canary-observe -allow-demo -env-file ~/.config/microservices-trading-bot/etoro-demo.env \
//	  -ledger ./etoro-daily-data/rehearsal-canary/ledger.jsonl -ledger-name etoro-canary-rehearsal
//	# day-0 captures (eligibility, costs at the canary size, rates, account), read-only;
//	# not with -dry, which writes nothing at all (captures included)
//	etoro-canary-observe -capture ../../docs/etoro/evidence-real-canary-2026-11-03
//
// Exit status: 0 when every read succeeded (the line is written), 1 when a
// read failed (the line is still written, with its errors, so a gap in the
// series is visible; the alerts job picks the exit code up), 2 on usage.
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

	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/strategy-executor/internal/stageenv"
)

func main() {
	ledger := flag.String("ledger", "./etoro-daily-data/real-canary/ledger.jsonl", "the canary ledger (read only; labels the positions)")
	ledgerName := flag.String("ledger-name", "etoro-real-canary", "the ledger's name, written on every observation")
	out := flag.String("out", "", "observations file (default: observations.jsonl next to the ledger)")
	envFile := flag.String("env-file", defaultEnvFile(), "file with ETORO_PUBLIC_KEY / ETORO_PRIVATE_KEY / ETORO_ENV (chmod 600); values are read, never printed")
	allowDemo := flag.Bool("allow-demo", false, "accept ETORO_ENV=demo (the rehearsal); by default the env file must say real and the ledger must be under a real-canary/ directory")
	dry := flag.Bool("dry", false, "print the observation, write nothing")
	capture := flag.String("capture", "", "also write the day-0 captures (eligibility.json, costs.json, rates.json, account.json, SHA256SUMS) to this directory, read-only")
	previewInstrument := flag.Int64("preview-instrument", 28, "instrument previewed when no position is held (28 = NSDQ100)")
	previewAmount := flag.Float64("preview-amount", 50, "invested amount of the preview when no position is held")
	previewLeverage := flag.Int("preview-leverage", 20, "leverage of the preview when no position is held")
	sinceDays := flag.Int("since-days", 90, "how far back to read the trading history when the ledger has no open yet")
	flag.Parse()

	if *envFile != "" {
		if err := stageenv.LoadFile(*envFile); err != nil {
			die(2, err)
		}
	}
	env, err := etoro.ParseEnvironment(strings.ToLower(strings.TrimSpace(os.Getenv("ETORO_ENV"))))
	if err != nil {
		die(2, err)
	}
	switch {
	case env == etoro.EnvDemo && !*allowDemo:
		die(2, fmt.Errorf("ETORO_ENV is demo: this is the real canary's observer; pass -allow-demo for the rehearsal"))
	case env == etoro.EnvReal && *allowDemo:
		die(2, fmt.Errorf("-allow-demo with a real env file: drop the flag"))
	case env == etoro.EnvReal && filepath.Base(filepath.Dir(*ledger)) != "real-canary":
		die(2, fmt.Errorf("a real env file needs the canary ledger (a directory named real-canary), got %s", *ledger))
	}
	c, err := etoro.NewClient(etoro.Config{PublicKey: os.Getenv("ETORO_PUBLIC_KEY"), PrivateKey: os.Getenv("ETORO_PRIVATE_KEY"), Env: env})
	if err != nil {
		die(2, err)
	}
	recs, err := etoroledger.ReadFile(*ledger)
	if err != nil {
		die(2, fmt.Errorf("ledger: %w", err))
	}
	path := *out
	if path == "" {
		path = etoroledger.ObservationsPath(*ledger)
	}
	prevAll, err := etoroledger.ReadObservations(path)
	if err != nil {
		die(2, fmt.Errorf("observations: %w", err))
	}
	ctx := context.Background()
	now := time.Now().UTC()
	o := observe(ctx, c, input{recs: recs, prev: etoroledger.LatestObservation(prevAll), now: now, env: string(env), ledger: *ledgerName,
		sinceDays: *sinceDays, previewInstrument: *previewInstrument, previewAmount: *previewAmount, previewLeverage: *previewLeverage})
	b, _ := json.MarshalIndent(o, "", "  ")
	fmt.Println(string(b))
	if *capture != "" {
		if errs := captureDay0(ctx, c, *capture, *previewInstrument, *previewAmount, *previewLeverage, *dry); len(errs) > 0 {
			o.Errors = append(o.Errors, errs...)
		}
	}
	if !*dry {
		if err := etoroledger.AppendObservation(path, o); err != nil {
			die(1, fmt.Errorf("write %s: %w", path, err))
		}
		fmt.Fprintf(os.Stderr, "etoro-canary-observe: appended to %s (%d position(s), %d new history row(s), %d error(s))\n", path, len(o.Positions), len(o.HistoryNew), len(o.Errors))
	}
	if len(o.Errors) > 0 {
		for _, e := range o.Errors {
			fmt.Fprintf(os.Stderr, "etoro-canary-observe: read failed: %s\n", e)
		}
		os.Exit(1)
	}
}

func defaultEnvFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "microservices-trading-bot", "etoro-real-canary.env")
}

func die(code int, err error) {
	fmt.Fprintf(os.Stderr, "etoro-canary-observe: %v\n", err)
	os.Exit(code)
}
