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
// Real-account canary (2026-10-22, docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md
// §4 M1 / M2): with -real-canary and the canary's env file (ETORO_ENV=real)
// it reads the real account the same way, and when observations.jsonl
// exists next to the ledger (cmd/etoro-canary-observe writes it) it adds
// the "per night, as observed" section: measured vs the preview vs the
// frozen model, night by night, with the weekend charge flagged. The model
// is priced on exposure (amount x leverage), which equals the amount at x1.
//
//	go run ./cmd/etoro-fees-check -real-canary -ledger ./etoro-daily-data/real-canary/ledger.jsonl \
//	  -env-file ~/.config/microservices-trading-bot/etoro-real-canary.env
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
	realCanary := flag.Bool("real-canary", false, "read the REAL account for the measurement canary: the env file must say ETORO_ENV=real and -ledger must be under a real-canary/ directory (read calls only)")
	observations := flag.String("observations", "", "observations.jsonl of cmd/etoro-canary-observe for the per-night section (default: next to the ledger when present; \"-\" skips it)")
	flag.Parse()

	if *envFile != "" {
		if err := stageenv.LoadFile(*envFile); err != nil {
			die(err)
		}
	}
	env, err := etoro.ParseEnvironment(strings.ToLower(strings.TrimSpace(os.Getenv("ETORO_ENV"))))
	switch {
	case err != nil:
		usage(err.Error())
	case *realCanary && env != etoro.EnvReal:
		usage(fmt.Sprintf("-real-canary needs an env file with ETORO_ENV=real (got %q)", os.Getenv("ETORO_ENV")))
	case *realCanary && filepath.Base(filepath.Dir(*ledger)) != "real-canary":
		usage(fmt.Sprintf("-real-canary needs the canary ledger (a directory named real-canary), got %s", *ledger))
	case !*realCanary && env != etoro.EnvDemo:
		usage(fmt.Sprintf("ETORO_ENV must be demo (got %q): this tool reads the demo account unless -real-canary is given", os.Getenv("ETORO_ENV")))
	}
	c, err := etoro.NewClient(etoro.Config{PublicKey: os.Getenv("ETORO_PUBLIC_KEY"), PrivateKey: os.Getenv("ETORO_PRIVATE_KEY"), Env: env})
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
	rep, err := checkEnv(context.Background(), etorobroker.New(c), recs, now.AddDate(0, 0, -*sinceDays), *overnight, now, only, string(env))
	if err != nil {
		die(err)
	}
	if obsPath := *observations; obsPath != "-" {
		if obsPath == "" && *ledger != "" {
			obsPath = etoroledger.ObservationsPath(*ledger)
		}
		if obsPath != "" {
			obs, oerr := etoroledger.ReadObservations(obsPath)
			if oerr != nil {
				die(fmt.Errorf("observations: %w", oerr))
			}
			addNights(&rep, obs, obsPath)
		}
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
