// Command etoro-daily-executor runs the frozen SMA50 trend rule once a day on
// the eToro index CFDs NSDQ100 and SPX500, for the forward test registered in
// docs/backtest-readiness/FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md
// (porting plan phase P3). It is the eToro sibling of cmd/daily-executor and
// keeps its flow: lock, halt check, stale-data refusal, frozen rule, paper
// account, data-integrity checks, risk check, idempotent order, append-only
// ledger.
//
// Run it at 09:35 New York time on NYSE trading days (cron with
// CRON_TZ=America/New_York; scripts/etoro-daily-executor-run.sh). For each
// instrument it:
//
//  1. reads eToro's live daily candles (UTC-day buckets) and keeps NYSE
//     trading days only (shared/pkg/etorodaily), refusing stale data: the last
//     closed bar must be the latest closed trading day;
//
//  2. applies the frozen rule (shared/pkg/dailyrule, SMA50 of closes) and runs
//     the paper account with the pre-registered costs (shared/pkg/cfdsim);
//
//  3. with -demo, brings the demo position to the rule's target: an x1 market
//     buy of -amount USD when flat and the rule says long, a full close of the
//     recorded position when long and the rule says flat. Orders are sent only
//     in the NYSE cash session and only after the account agrees with the
//     ledger (no untracked position on the instrument). Each order is written
//     to an intent journal first and carries x-request-id =
//     UUIDv5(ClientRef), so a re-run after a crash resolves the first order
//     instead of placing a second one;
//
//  4. appends one line per instrument per decision day to the ledger
//     (shared/pkg/etoroledger) and never writes a second line for that day.
//
//     go run ./cmd/etoro-daily-executor                       # dry run: decide and record, no orders
//     go run ./cmd/etoro-daily-executor -no-record            # print only
//     go run ./cmd/etoro-daily-executor -demo -ledger ./etoro-daily-data/demo/ledger.jsonl
//
// Credentials: ETORO_PUBLIC_KEY / ETORO_PRIVATE_KEY (the candles route needs
// them too), from the environment or -env-file (chmod 600). ETORO_ENV must be
// demo or unset: the real account is refused unconditionally.
//
// Exit status: 0 when every instrument was decided (or already recorded) and
// every demo leg went through, 1 when anything failed or was blocked, 2 on bad
// usage or an invalid halt file. ETORO_EXECUTOR_DISABLED=1 exits 0 at once.
//
// Halt file: <ledger dir>/risk-state.json, the same file and format as the
// Bitso executor (shared/pkg/risk.LoadHaltState). While halted, a planned
// order is blocked and recorded; days without an order are recorded as usual.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/broker/etorobroker"
	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/etorodaily"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/strategy-executor/internal/stageenv"
)

const (
	minAmount = 1000 // eToro's minimum exposure for the index CFDs (P1 evidence)
	maxAmount = 1500 // EtoroDemoPolicy's order notional cap
)

type options struct {
	ledgerPath, candlesDir string
	demo, noRecord         bool
	amount                 float64
	policy                 risk.Policy
	halt                   *risk.HaltState
}

func main() {
	insts := flag.String("instruments", strings.Join(specNames(), ","), "comma-separated instruments (each must have a frozen spec)")
	var o options
	flag.StringVar(&o.ledgerPath, "ledger", "./etoro-daily-data/ledger.jsonl", "append-only JSONL ledger (use a separate one for -demo)")
	flag.StringVar(&o.candlesDir, "candles-dir", "", "where fetched bars are saved (default: <ledger dir>/candles)")
	flag.BoolVar(&o.demo, "demo", false, "place orders on the eToro DEMO account")
	flag.BoolVar(&o.noRecord, "no-record", false, "print the decision but do not write the ledger (dry run only)")
	flag.Float64Var(&o.amount, "amount", 1100, fmt.Sprintf("USD per entry, x1 (%d..%d)", minAmount, maxAmount))
	asOf := flag.String("as-of", "", "RFC3339 time to run as (default now); dry run only")
	riskPolicy := flag.String("risk-policy", "", "JSON risk policy (default: built-in shared/pkg/risk.EtoroDemoPolicy)")
	envFile := flag.String("env-file", defaultEnvFile(), "file with ETORO_PUBLIC_KEY / ETORO_PRIVATE_KEY (chmod 600)")
	flag.Parse()

	if os.Getenv("ETORO_EXECUTOR_DISABLED") == "1" {
		fmt.Println("etoro-daily-executor: ETORO_EXECUTOR_DISABLED=1, exiting without doing anything")
		return
	}
	now := time.Now().UTC()
	if *asOf != "" {
		if o.demo {
			usage("-as-of cannot be combined with -demo: orders are placed now")
		}
		t, err := time.Parse(time.RFC3339, *asOf)
		if err != nil {
			usage(fmt.Sprintf("-as-of: %v", err))
		}
		now = t.UTC()
	}
	if o.demo && o.noRecord {
		usage("-no-record cannot be combined with -demo: every order must be recorded")
	}
	if o.amount < minAmount || o.amount > maxAmount {
		usage(fmt.Sprintf("-amount must be in [%d, %d]", minAmount, maxAmount))
	}
	if o.candlesDir == "" {
		o.candlesDir = filepath.Join(filepath.Dir(o.ledgerPath), "candles")
	}
	var specs []instSpec
	for _, n := range strings.Split(*insts, ",") {
		n = strings.ToUpper(strings.TrimSpace(n))
		if n == "" {
			continue
		}
		s, ok := frozenSpecs[n]
		if !ok {
			usage(fmt.Sprintf("instrument %q has no frozen spec; only %s are pre-registered", n, strings.Join(specNames(), ", ")))
		}
		specs = append(specs, s)
	}
	if len(specs) == 0 {
		usage("no instruments")
	}
	policy := risk.EtoroDemoPolicy()
	if *riskPolicy != "" {
		p, err := risk.LoadPolicy(*riskPolicy)
		if err != nil {
			usage(err.Error())
		}
		policy = p
	}
	h, _, err := risk.LoadHaltState(risk.HaltPath(o.ledgerPath))
	if err != nil {
		usage(err.Error() + " (fix or remove it; nothing ran)")
	}
	if h.Halted {
		policy = risk.ApplyHalt(policy, h)
		o.halt = &h
	}
	o.policy = policy

	lock, err := lockFile(o.ledgerPath + ".lock")
	if err != nil {
		fail(err)
	}
	defer lock.Close()

	mode := etoroledger.ModeDryRun
	if o.demo {
		mode = etoroledger.ModeDemo
	}
	version := codeVersion()
	fmt.Printf("etoro-daily-executor %s | as of %s (New York %s) | mode %s | ledger %s | risk policy %s | %.0f USD x1\n",
		version, now.Format(time.RFC3339), now.In(mktcal.NewYork).Format("2006-01-02 15:04"), mode, o.ledgerPath, o.policy.Version, o.amount)
	if o.halt != nil {
		fmt.Printf("HALTED by %s: %s (by %s at %s); demo orders will be blocked and recorded\n",
			risk.HaltPath(o.ledgerPath), o.halt.Reason, o.halt.By, o.halt.At)
	}

	if *envFile != "" {
		if err := stageenv.LoadFile(*envFile); err != nil {
			fail(err)
		}
	}
	env, err := etoro.ParseEnvironment(strings.ToLower(strings.TrimSpace(os.Getenv("ETORO_ENV"))))
	if err != nil {
		usage(err.Error())
	}
	if env != etoro.EnvDemo {
		usage("ETORO_ENV=" + string(env) + ": this executor runs on the eToro demo account only (the real account is blocked in code)")
	}
	c, err := etoro.NewClient(etoro.Config{PublicKey: os.Getenv("ETORO_PUBLIC_KEY"), PrivateKey: os.Getenv("ETORO_PRIVATE_KEY"), Env: etoro.EnvDemo})
	if err != nil {
		fail(err)
	}
	led, err := etoroledger.Open(o.ledgerPath)
	if err != nil {
		fail(err)
	}
	de := demoEnv{b: etorobroker.New(c), led: led, intents: etoroledger.IntentDir(o.ledgerPath), policy: o.policy,
		halt: o.halt, amount: o.amount, now: time.Now}

	ctx := context.Background()
	failed := false
	for _, s := range specs {
		book := s.Book
		de.logf = func(format string, args ...any) {
			fmt.Printf("[%s] %s "+format+"\n", append([]any{book, time.Now().UTC().Format("15:04:05Z")}, args...)...)
		}
		fetch := func() ([]bitsodaily.Row, etorodaily.Report, error) {
			return etorodaily.Fetch(ctx, c, s.ID, etoro.MaxCandles, now, etorodaily.Options{})
		}
		if err := runBook(ctx, o, de, s, fetch, now, version, mode); err != nil {
			fmt.Printf("\n[%s] FAILED: %v\n", s.Book, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

// revisionTolBps is how far a recorded close may move before the run stops:
// eToro's live daily route serves two versions of recent closes, up to
// ~1.3 bps apart (seen 2026-10-10); a larger move is a real revision.
const revisionTolBps = 5

// runBook decides one instrument and records it (dry run) or runs its demo
// leg. A nil error with nothing written means the day was already recorded.
func runBook(ctx context.Context, o options, de demoEnv, s instSpec, fetch func() ([]bitsodaily.Row, etorodaily.Report, error), now time.Time, version, mode string) error {
	rows, rep, err := fetch()
	if err != nil {
		return fmt.Errorf("fetch candles: %w", err)
	}
	if len(rows) == 0 {
		return errors.New("no closed bars returned")
	}
	// Second fetch: the rule may act only if both versions agree.
	rows2, _, err := fetch()
	if err != nil {
		return fmt.Errorf("fetch candles (second read): %w", err)
	}
	revised, maxRev, err := compareFetches(rows, rows2)
	if err != nil {
		return err
	}
	csvPath := filepath.Join(o.candlesDir, fmt.Sprintf("%s_daily_%s.csv", s.Book, rows[len(rows)-1].Date))
	if err := bitsodaily.WriteCSV(csvPath, s.Symbol, rows); err != nil {
		return fmt.Errorf("save candles: %w", err)
	}
	sum, err := fileSHA256(csvPath)
	if err != nil {
		return err
	}
	d, bars, pts, err := decide(rows, now)
	if err != nil {
		return err
	}
	d2, _, _, err := decide(rows2, now)
	if err != nil {
		return err
	}
	if d2.Signal != d.Signal || d2.PrevSignal != d.PrevSignal {
		return fmt.Errorf("the two reads of eToro's bars disagree: %s/%s vs %s/%s (closes %v vs %v): not acting on it",
			d.Signal, d.PrevSignal, d2.Signal, d2.PrevSignal, d.Close, d2.Close)
	}
	p, err := paper(bars, pts, s)
	if err != nil {
		return err
	}
	rec := &etoroledger.Record{
		Schema: etoroledger.Schema, CodeVersion: version, Mode: mode, Venue: etorobroker.Venue, Book: s.Book,
		Instrument: etoroledger.Instrument{Symbol: s.Symbol, ID: s.ID}, Prereg: s.Prereg, Decision: d, Paper: p,
		Candles: etoroledger.CandleInfo{
			Source: fmt.Sprintf("etoro live candles OneDay, instrument %d, NYSE trading days", s.ID),
			First:  rows[0].Date, Last: rows[len(rows)-1].Date, Bars: len(rows), Missing: rep.MissingTrading, SHA256Short: sum[:16],
			Revised: revised, MaxRevisionBps: maxRev,
		},
	}
	printRecord(*rec, csvPath)

	// Data integrity (pre-registration §5): a recorded day must not change
	// beyond eToro's two-version noise.
	if prev, ok := de.led.Get(s.Book, d.BarDate); ok {
		if prev.Decision.Signal != d.Signal || relBps(prev.Decision.Close, d.Close) > revisionTolBps {
			return fmt.Errorf("%s was recorded as %s at close %v, bars now say %s at %v: investigate before continuing",
				d.BarDate, prev.Decision.Signal, prev.Decision.Close, d.Signal, d.Close)
		}
		if prev.Mode != mode {
			return fmt.Errorf("%s is already recorded in %s mode in this ledger; use a separate -ledger for %s", d.BarDate, prev.Mode, mode)
		}
		fmt.Printf("  ledger     : %s already recorded at %s, unchanged; nothing to do\n", d.BarDate, prev.RecordedAt)
		return nil
	}
	if err := checkHistory(de.led, s.Book, rows); err != nil {
		return err
	}
	if !o.demo {
		if o.noRecord {
			fmt.Printf("[%s] ledger: -no-record, nothing written\n", s.Book)
			return nil
		}
		rec.RecordedAt = time.Now().UTC().Format(time.RFC3339)
		if err := de.led.Append(*rec); err != nil {
			return err
		}
		fmt.Printf("[%s] ledger: recorded (dry run, no order placed)\n", s.Book)
		return nil
	}
	return runDemo(ctx, de, s, rec)
}

// checkHistory verifies that every recorded day of the book still has the
// same close in today's bars, within revisionTolBps.
func checkHistory(led *etoroledger.Ledger, book string, rows []bitsodaily.Row) error {
	closeOn := make(map[string]float64, len(rows))
	for _, r := range rows {
		closeOn[r.Date] = r.Close
	}
	first := ""
	if len(rows) > 0 {
		first = rows[0].Date
	}
	for _, r := range led.Records() {
		if r.Book != book || r.Decision.BarDate < first {
			continue // older than the 1000-bar window
		}
		c, ok := closeOn[r.Decision.BarDate]
		if !ok {
			return fmt.Errorf("recorded day %s is missing from today's bars", r.Decision.BarDate)
		}
		if relBps(r.Decision.Close, c) > revisionTolBps {
			return fmt.Errorf("recorded day %s had close %v, bars now say %v (%.1f bps): eToro revised history", r.Decision.BarDate, r.Decision.Close, c, relBps(r.Decision.Close, c))
		}
	}
	return nil
}

// compareFetches checks that two reads of the bars cover the same days and
// measures how far their closes differ.
func compareFetches(a, b []bitsodaily.Row) (revised int, maxBps float64, err error) {
	if len(a) != len(b) || len(a) == 0 || a[0].Date != b[0].Date || a[len(a)-1].Date != b[len(b)-1].Date {
		return 0, 0, fmt.Errorf("two reads of eToro's bars cover different days (%d vs %d bars): not acting on it", len(a), len(b))
	}
	for i := range a {
		if a[i].Date != b[i].Date {
			return 0, 0, fmt.Errorf("two reads of eToro's bars differ at %s / %s", a[i].Date, b[i].Date)
		}
		if a[i].Close != b[i].Close {
			revised++
			if d := relBps(a[i].Close, b[i].Close); d > maxBps {
				maxBps = d
			}
		}
	}
	if maxBps > revisionTolBps {
		return revised, maxBps, fmt.Errorf("two reads of eToro's bars differ by %.1f bps on some close (tolerance %d): not acting on it", maxBps, revisionTolBps)
	}
	return revised, maxBps, nil
}

func relBps(a, b float64) float64 {
	if a <= 0 {
		return 0
	}
	d := (b - a) / a * 1e4
	if d < 0 {
		d = -d
	}
	return d
}

func printRecord(r etoroledger.Record, csvPath string) {
	d, p := r.Decision, r.Paper
	fmt.Printf("\n[%s] %s (id %d) %s\n", r.Book, r.Instrument.Symbol, r.Instrument.ID, r.Prereg)
	fmt.Printf("  bars       : %d %s .. %s, %d missing trading days (sha256 %s) -> %s\n", r.Candles.Bars, r.Candles.First, r.Candles.Last, r.Candles.Missing, r.Candles.SHA256Short, csvPath)
	if r.Candles.Revised > 0 {
		fmt.Printf("  revisions  : %d closes differed between two reads (max %.2f bps, tolerance %d)\n", r.Candles.Revised, r.Candles.MaxRevisionBps, revisionTolBps)
	}
	fmt.Printf("  rule       : %s close %.2f vs SMA50 %.2f -> %s (was %s), fill %s\n", d.BarDate, d.Close, d.SMA, strings.ToUpper(d.Signal), d.PrevSignal, d.FillDate)
	if p.Started {
		fmt.Printf("  paper      : since %s, %d days, %d round trips, costs %s | position %s | equity %.4f vs CFD hold %.4f | max DD %.2f%% | financing %.2f%%\n",
			p.ForwardStart, p.Days, p.RoundTrips, p.CostsStatus, p.Position, p.Equity, p.HoldEquity, p.MaxDrawdown*100, p.FinancingPct)
	} else {
		fmt.Printf("  paper      : forward window starts %s (no forward bar yet)\n", p.ForwardStart)
	}
	fmt.Printf("  paper next : %s open -> %s\n", d.FillDate, strings.ToUpper(p.PendingAction))
}

func fileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// defaultEnvFile is ~/.config/microservices-trading-bot/etoro-demo.env.
func defaultEnvFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "microservices-trading-bot", "etoro-demo.env")
}

// lockFile takes an exclusive, non-blocking flock so two runs cannot
// interleave orders or ledger lines.
func lockFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another etoro-daily-executor holds %s: %w", path, err)
	}
	return f, nil
}

// codeVersion: ETORO_EXECUTOR_VERSION, else the VCS revision (+dirty).
func codeVersion() string {
	if v := os.Getenv("ETORO_EXECUTOR_VERSION"); v != "" {
		return v
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "unknown"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "+dirty"
	}
	return rev
}

func usage(msg string) {
	fmt.Fprintf(os.Stderr, "etoro-daily-executor: %s\n", msg)
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "etoro-daily-executor: %v\n", err)
	os.Exit(1)
}
