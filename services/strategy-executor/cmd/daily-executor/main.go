// Command daily-executor runs the frozen SMA50 trend rule once a day for the
// forward tests pre-registered in docs/backtest-readiness/:
//
//   - FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md        (btc_mxn)
//   - FORWARD-TEST-PREREGISTRATION-SMA50-BTCUSD-2026-09-29.md (btc_usd)
//
// Run it shortly after Mexico City midnight, when yesterday's daily candle
// has closed. For each book it:
//
//  1. fetches Bitso PRODUCTION daily candles (public, no keys), because the
//     forward tests are judged on real market prices;
//
//  2. refuses to act on stale data (the last closed bar must be yesterday,
//     Mexico City time);
//
//  3. applies the rule with internal/dailyrule, the same code as
//     cmd/daily-research, the registered evaluation engine;
//
//  4. updates the paper account with daily-research's exact arithmetic;
//
//  5. with -stage, trades a fixed BTC size on Bitso STAGE (internal/dailyexec:
//     post-only limit at the best price, market fallback after the maker
//     timeout), tracking the position from its own fills;
//
//  6. appends one line per book per day to an append-only JSONL ledger, and
//     never writes a second line for the same day.
//
//     go run ./cmd/daily-executor                               # dry run: decide and record, no orders
//     go run ./cmd/daily-executor -as-of 2026-09-30T06:05:00Z   # what that day's run decided
//     go run ./cmd/daily-executor -stage -ledger ./stage/ledger.jsonl   # trade on Bitso stage
//
// Stage credentials come from STAGE_BITSO_API_KEY / STAGE_BITSO_API_SECRET,
// loaded from -env-file (default ~/.config/microservices-trading-bot/
// bitso-stage.env, which must be chmod 600) when not already in the
// environment. The stage client refuses any base URL other than stage.
//
// Exit status: 0 when every book was decided (or already recorded), 1 when
// any book failed (stale or missing candles, fetch error, data changed since
// it was recorded, order error, order blocked by the risk check), 2 on bad
// usage. DAILY_EXECUTOR_DISABLED=1 exits 0 at once.
//
// Risk: with -stage, every planned order first goes through the pre-trade
// check in shared/pkg/risk (size, position, notional, orders per day, price
// deviation from the decision close, global halt), using -risk-policy or the
// built-in risk.DefaultPolicy(). A blocked order is not sent and not retried:
// the day is recorded with stage action "blocked" and the findings, and the
// run exits 1. See risk.go. The check never changes a signal or the paper
// account.
//
// Halt file (R2): <ledger dir>/risk-state.json, written by an operator as
// {"halted": true, "reason": "...", "by": "...", "at": "<RFC 3339>"}, halts
// stage orders like the policy's halted flag: a planned order is blocked and
// recorded with the halt. Days without an order are recorded as usual. An
// unreadable or invalid file exits 2 before anything runs.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/strategy-executor/internal/bitsodaily"
	"bitso-trading-platform/strategy-executor/internal/bitsostage"
	"bitso-trading-platform/strategy-executor/internal/dailyexec"
)

type options struct {
	ledgerPath, candlesDir, baseURL string
	noRecord, stage                 bool
	size                            float64
	exec                            dailyexec.Config
	riskPolicy                      risk.Policy
	halt                            *risk.HaltState // operator halt file in force, if any
}

func main() {
	books := flag.String("books", "btc_mxn,btc_usd", "comma-separated books (each must have a frozen spec)")
	var o options
	flag.StringVar(&o.ledgerPath, "ledger", "./daily-executor-data/ledger.jsonl", "append-only JSONL ledger")
	flag.StringVar(&o.candlesDir, "candles-dir", "", "where fetched candles are saved (default: <ledger dir>/candles)")
	flag.StringVar(&o.baseURL, "candles-base-url", bitsodaily.DefaultBaseURL, "Bitso API for candles (production; the forward tests are judged on it)")
	asOf := flag.String("as-of", "", "RFC3339 time to run as (default now); dry run only")
	flag.BoolVar(&o.noRecord, "no-record", false, "print the decision but do not write the ledger (dry run only)")
	flag.BoolVar(&o.stage, "stage", false, "place orders on Bitso STAGE")
	flag.Float64Var(&o.size, "size", 0.001, "BTC bought per entry on stage (max 0.01)")
	envFile := flag.String("env-file", defaultEnvFile(), "file with STAGE_BITSO_API_KEY / STAGE_BITSO_API_SECRET (chmod 600)")
	o.exec = dailyexec.DefaultConfig()
	flag.DurationVar(&o.exec.MakerTimeout, "maker-timeout", o.exec.MakerTimeout, "rest the post-only order this long before the market fallback")
	flag.DurationVar(&o.exec.Poll, "poll", o.exec.Poll, "how often to check fills")
	riskPolicy := flag.String("risk-policy", "", "JSON risk policy for stage orders (default: built-in shared/pkg/risk.DefaultPolicy)")
	flag.Parse()

	if os.Getenv("DAILY_EXECUTOR_DISABLED") == "1" {
		fmt.Println("daily-executor: DAILY_EXECUTOR_DISABLED=1, exiting without doing anything")
		return
	}
	now := time.Now().UTC()
	if *asOf != "" {
		if o.stage {
			usage("-as-of cannot be combined with -stage: orders are placed now")
		}
		t, err := time.Parse(time.RFC3339, *asOf)
		if err != nil {
			usage(fmt.Sprintf("-as-of: %v", err))
		}
		now = t.UTC()
	}
	if o.stage && o.noRecord {
		usage("-no-record cannot be combined with -stage: every order must be recorded")
	}
	if o.size <= 0 || o.size > maxSize {
		usage(fmt.Sprintf("-size must be in (0, %v]", maxSize))
	}
	if o.candlesDir == "" {
		o.candlesDir = filepath.Join(filepath.Dir(o.ledgerPath), "candles")
	}
	policy, err := risk.LoadPolicy(*riskPolicy)
	if err != nil {
		usage(err.Error())
	}
	o.riskPolicy = policy
	if o.riskPolicy, o.halt, err = loadHalt(o.ledgerPath, o.riskPolicy); err != nil {
		usage(err.Error() + " (fix or remove it; nothing ran)")
	}
	var specs []bookSpec
	for _, b := range strings.Split(*books, ",") {
		b = strings.ToLower(strings.TrimSpace(b))
		if b == "" {
			continue
		}
		s, ok := frozenSpecs[b]
		if !ok {
			usage(fmt.Sprintf("book %q has no frozen spec; only btc_mxn and btc_usd are pre-registered", b))
		}
		specs = append(specs, s)
	}
	if len(specs) == 0 {
		usage("no books")
	}

	lock, err := lockFile(o.ledgerPath + ".lock")
	if err != nil {
		fail(err)
	}
	defer lock.Close()

	var ex dailyexec.Exchange
	mode := "dry-run"
	if o.stage {
		mode = "stage"
		if *envFile != "" {
			if err := loadEnvFile(*envFile); err != nil {
				fail(err)
			}
		}
		secret := os.Getenv("STAGE_BITSO_API_SECRET")
		if secret == "" {
			secret = os.Getenv("STAGE_BITSO_APISECRET") // legacy name used by order-management
		}
		base := os.Getenv("BITSO_API_BASE_URL")
		if base == "" {
			base = bitsostage.StageBaseURL
		}
		c, err := bitsostage.New(base, os.Getenv("STAGE_BITSO_API_KEY"), secret)
		if err != nil {
			fail(err)
		}
		ex = c
	}

	led, err := openLedger(o.ledgerPath)
	if err != nil {
		fail(err)
	}
	version := codeVersion()
	fmt.Printf("daily-executor %s | as of %s (Mexico City %s) | mode %s | ledger %s | risk policy %s\n",
		version, now.Format(time.RFC3339), now.In(bitsodaily.Mexico).Format("2006-01-02 15:04"), mode, o.ledgerPath, o.riskPolicy.Version)
	if o.halt != nil {
		fmt.Printf("HALTED by %s: %s (by %s at %s); stage orders will be blocked and recorded\n",
			risk.HaltPath(o.ledgerPath), o.halt.Reason, o.halt.By, o.halt.At)
	}

	// Step 1, sequential: candles, decision, paper account, integrity checks.
	client := &http.Client{Timeout: 30 * time.Second}
	failed := false
	var pending []*record
	for _, s := range specs {
		rec, err := prepareBook(client, o, s, now, led, version, mode)
		if err != nil {
			fmt.Printf("\n[%s] FAILED: %v\n", s.Book, err)
			failed = true
			continue
		}
		if rec != nil {
			pending = append(pending, rec)
		}
	}

	// Step 2: record (dry run), or trade then record (stage). Books trade in
	// parallel because a leg can rest for up to the maker timeout.
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, rec := range pending {
		wg.Add(1)
		go func(rec *record) {
			defer wg.Done()
			if err := finishBook(o, ex, led, rec); err != nil {
				mu.Lock()
				fmt.Printf("\n[%s] FAILED: %v\n", rec.Book, err)
				failed = true
				mu.Unlock()
			}
		}(rec)
	}
	wg.Wait()
	if failed {
		os.Exit(1)
	}
}

// prepareBook returns the day's record, or nil when it is already recorded.
func prepareBook(client *http.Client, o options, s bookSpec, now time.Time, led *ledger, version, mode string) (*record, error) {
	from, err := time.Parse("2006-01-02", s.HistoryFrom)
	if err != nil {
		return nil, err
	}
	cs, err := bitsodaily.FetchRange(client, o.baseURL, s.Book, from, now, 365*bitsodaily.Day)
	if err != nil {
		return nil, fmt.Errorf("fetch candles: %w", err)
	}
	rows, _ := bitsodaily.ToRows(cs, now)
	if len(rows) == 0 {
		return nil, fmt.Errorf("no closed candles returned")
	}
	csvPath := filepath.Join(o.candlesDir, fmt.Sprintf("%s_daily_%s.csv", s.Book, rows[len(rows)-1].Date))
	if err := bitsodaily.WriteCSV(csvPath, s.Book, rows); err != nil {
		return nil, fmt.Errorf("save candles: %w", err)
	}
	sum, err := fileSHA256(csvPath)
	if err != nil {
		return nil, err
	}
	bars, err := toBars(rows)
	if err != nil {
		return nil, err
	}
	d, pts, err := decide(bars, now)
	if err != nil {
		return nil, err
	}
	p, err := paper(bars, pts, s.ForwardStart, s.LegCostBps)
	if err != nil {
		return nil, err
	}
	recent := rows
	if len(recent) > 60 {
		recent = recent[len(recent)-60:]
	}
	rec := &record{
		CodeVersion: version,
		Mode:        mode,
		Book:        s.Book,
		Prereg:      s.Prereg,
		Decision:    d,
		Paper:       p,
		Candles: candleInfo{
			Source: o.baseURL + "/api/v3/ohlc", First: rows[0].Date, Last: rows[len(rows)-1].Date,
			Bars: len(rows), RecentGaps: bitsodaily.Gaps(recent), SHA256Short: sum[:16],
		},
	}
	printRecord(*rec, csvPath)

	// Data integrity (pre-registration §5): a day already recorded must not
	// change. If Bitso revised a candle, stop and surface it.
	if prev, ok := led.get(s.Book, d.BarDate); ok {
		if prev.Decision.Signal != d.Signal || prev.Decision.Close != d.Close {
			return nil, fmt.Errorf("%s was recorded as %s at close %v, candles now say %s at %v: investigate before continuing",
				d.BarDate, prev.Decision.Signal, prev.Decision.Close, d.Signal, d.Close)
		}
		if prev.Mode != mode {
			return nil, fmt.Errorf("%s is already recorded in %s mode in this ledger; use a separate -ledger for %s", d.BarDate, prev.Mode, mode)
		}
		fmt.Printf("  ledger     : %s already recorded at %s, unchanged; nothing to do\n", d.BarDate, prev.RecordedAt)
		return nil, nil
	}
	if err := checkHistory(led, s.Book, rows); err != nil {
		return nil, err
	}
	return rec, nil
}

// finishBook trades on stage when enabled, then appends the record.
func finishBook(o options, ex dailyexec.Exchange, led *ledger, rec *record) error {
	if !o.stage {
		if o.noRecord {
			fmt.Printf("[%s] ledger: -no-record, nothing written\n", rec.Book)
			return nil
		}
		rec.RecordedAt = time.Now().UTC().Format(time.RFC3339)
		if err := led.append(*rec); err != nil {
			return err
		}
		fmt.Printf("[%s] ledger: recorded (dry run, no order placed)\n", rec.Book)
		return nil
	}

	pos := lastStagePosition(led, rec.Book)
	action, qty := planAction(rec.Decision.Signal, pos, o.size)
	st := &stageInfo{Env: bitsostage.StageBaseURL, Target: rec.Decision.Signal, Action: action, PositionBefore: pos, PositionAfter: pos}
	logf := func(format string, args ...any) {
		fmt.Printf("[%s] %s "+format+"\n", append([]any{rec.Book, time.Now().UTC().Format("15:04:05Z")}, args...)...)
	}
	logf("stage: rule says %s, executor holds %s (%.8f BTC) -> %s %.8f BTC", rec.Decision.Signal, pos.State, pos.BTC, action, qty)
	var blocked error
	if action != "none" {
		ri, err := checkRisk(o.riskPolicy, ex, led, rec, action, qty, pos)
		if err != nil {
			return fmt.Errorf("stage %s: %w (nothing recorded; re-run today to retry)", action, err)
		}
		st.Risk = ri
		ri.Halt = o.halt
		for _, f := range ri.Findings {
			logf("risk: %s %s: %s", f.Severity, f.Rule, f.Message)
		}
		if !ri.Allowed {
			started, err := legStarted(ex, rec.Book, rec.Decision.FillDate, action)
			if err != nil {
				return fmt.Errorf("stage %s blocked by risk policy %s, and checking for an earlier partial leg failed: %w (nothing sent, nothing recorded)", action, ri.PolicyVersion, err)
			}
			if started {
				return fmt.Errorf("stage %s blocked by risk policy %s, but an earlier run already started this leg on Bitso: nothing sent, nothing recorded; resolve by hand", action, ri.PolicyVersion)
			}
			// Skip and record, never retry: the day is written as blocked
			// with the position unchanged, so a re-run finds it recorded.
			st.Action = actionBlocked
			blocked = fmt.Errorf("stage %s %.8f BTC blocked by risk policy %s (no order sent; recorded as blocked)", action, qty, ri.PolicyVersion)
			logf("risk: %v", blocked)
		} else {
			logf("risk: %s %.8f BTC at ~%.2f allowed by policy %s", action, qty, ri.Order.Price, ri.PolicyVersion)
		}
	}
	if action != "none" && blocked == nil {
		leg := dailyexec.Leg{Book: rec.Book, Side: action, Qty: qty, FillDate: rec.Decision.FillDate}
		res, err := dailyexec.Run(ex, dailyexec.RealClock{}, o.exec, leg, logf)
		if err != nil {
			// Not recorded: the next run on the same day resumes from Bitso's
			// trades for this leg's client ids.
			return fmt.Errorf("stage %s: %w (nothing recorded; re-run today to resume)", action, err)
		}
		st.Leg = &res
		st.PositionAfter = applyFill(pos, res.BaseDelta)
		logf("stage: %s filled %.8f/%.8f BTC gross (maker %.8f, market %.8f) avg %.2f fees %v, net BTC %+.8f -> holds %s %.8f BTC",
			action, res.Filled, res.Target, res.MakerFilled, res.TakerFilled, res.AvgPrice, res.Fees, res.BaseDelta, st.PositionAfter.State, st.PositionAfter.BTC)
	}
	rec.Stage = st
	rec.RecordedAt = time.Now().UTC().Format(time.RFC3339)
	if err := led.append(*rec); err != nil {
		return err
	}
	logf("ledger: recorded")
	return blocked
}

// checkHistory verifies that every earlier ledger day for this book still has
// the same close in today's candles.
func checkHistory(led *ledger, book string, rows []bitsodaily.Row) error {
	closeOn := make(map[string]float64, len(rows))
	for _, r := range rows {
		closeOn[r.Date] = r.Close
	}
	for _, r := range led.entries {
		if r.Book != book {
			continue
		}
		c, ok := closeOn[r.Decision.BarDate]
		if !ok {
			return fmt.Errorf("recorded day %s is missing from today's candles", r.Decision.BarDate)
		}
		if c != r.Decision.Close {
			return fmt.Errorf("recorded day %s had close %v, candles now say %v: Bitso revised history", r.Decision.BarDate, r.Decision.Close, c)
		}
	}
	return nil
}

func printRecord(r record, csvPath string) {
	d, p := r.Decision, r.Paper
	fmt.Printf("\n[%s] %s\n", r.Book, r.Prereg)
	fmt.Printf("  candles    : %d bars %s .. %s (sha256 %s) -> %s\n", r.Candles.Bars, r.Candles.First, r.Candles.Last, r.Candles.SHA256Short, csvPath)
	if r.Candles.RecentGaps != "" {
		fmt.Printf("  gaps (60d) : %s\n", r.Candles.RecentGaps)
	}
	fmt.Printf("  rule       : %s close %.2f vs SMA50 %.2f -> %s (was %s)\n", d.BarDate, d.Close, d.SMA, strings.ToUpper(d.Signal), d.PrevSignal)
	fmt.Printf("  paper      : since %s, %d days, %d fills, %.0f bps/leg | position %s | equity %.4f (if closed %.4f) vs hold %.4f | max DD %.2f%%\n",
		p.ForwardStart, p.Days, p.Fills, p.LegCostBps, p.Position, p.Equity, p.EquityClosed, p.HoldEquity, p.MaxDrawdown*100)
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

// codeVersion identifies the build that made each decision: the
// DAILY_EXECUTOR_VERSION env var if set, else the VCS revision stamped by
// `go build`, with "+dirty" if the tree had local changes.
func codeVersion() string {
	if v := os.Getenv("DAILY_EXECUTOR_VERSION"); v != "" {
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
	fmt.Fprintf(os.Stderr, "daily-executor: %s\n", msg)
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "daily-executor: %v\n", err)
	os.Exit(1)
}
