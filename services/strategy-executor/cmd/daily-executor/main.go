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
//  2. refuses to act on stale data (the last closed bar must be yesterday,
//     Mexico City time);
//  3. applies the rule with internal/dailyrule, the same code as
//     cmd/daily-research, the registered evaluation engine;
//  4. updates the paper account with daily-research's exact arithmetic;
//  5. appends one line per book per day to an append-only JSONL ledger, and
//     never writes a second line for the same day.
//
// Phase 1 is dry-run only: it prints what it would do and places no orders.
// Orders on Bitso stage (post-only limit, market fallback after a timeout)
// arrive in phase 2 behind an explicit flag.
//
//	go run ./cmd/daily-executor                          # today's decision, both books
//	go run ./cmd/daily-executor -as-of 2026-09-30T06:05:00Z  # what that day's run decided
//
// Exit status: 0 when every book was decided (or already recorded), 1 when
// any book failed (stale or missing candles, fetch error, data changed since
// it was recorded), 2 on bad usage. DAILY_EXECUTOR_DISABLED=1 exits 0 at once.
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
	"time"

	"bitso-trading-platform/strategy-executor/internal/bitsodaily"
)

func main() {
	books := flag.String("books", "btc_mxn,btc_usd", "comma-separated books (each must have a frozen spec)")
	ledgerPath := flag.String("ledger", "./daily-executor-data/ledger.jsonl", "append-only JSONL ledger")
	candlesDir := flag.String("candles-dir", "", "where fetched candles are saved (default: <ledger dir>/candles)")
	baseURL := flag.String("candles-base-url", bitsodaily.DefaultBaseURL, "Bitso API for candles (production; the forward tests are judged on it)")
	asOf := flag.String("as-of", "", "RFC3339 time to run as (default now); candles that had not closed by then are ignored")
	noRecord := flag.Bool("no-record", false, "print the decision but do not write the ledger")
	stage := flag.Bool("stage", false, "place orders on Bitso stage (not available until phase 2)")
	flag.Parse()

	if os.Getenv("DAILY_EXECUTOR_DISABLED") == "1" {
		fmt.Println("daily-executor: DAILY_EXECUTOR_DISABLED=1, exiting without doing anything")
		return
	}
	if *stage {
		usage("-stage is not implemented yet (phase 2); this build only runs dry")
	}
	now := time.Now().UTC()
	if *asOf != "" {
		t, err := time.Parse(time.RFC3339, *asOf)
		if err != nil {
			usage(fmt.Sprintf("-as-of: %v", err))
		}
		now = t.UTC()
	}
	if *candlesDir == "" {
		*candlesDir = filepath.Join(filepath.Dir(*ledgerPath), "candles")
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

	led, err := openLedger(*ledgerPath)
	if err != nil {
		fail(err)
	}
	version := codeVersion()
	fmt.Printf("daily-executor %s | as of %s (Mexico City %s) | mode dry-run | ledger %s\n",
		version, now.Format(time.RFC3339), now.In(bitsodaily.Mexico).Format("2006-01-02 15:04"), *ledgerPath)

	client := &http.Client{Timeout: 30 * time.Second}
	failed := false
	for _, s := range specs {
		if err := runBook(client, *baseURL, *candlesDir, s, now, led, version, *noRecord); err != nil {
			fmt.Printf("\n[%s] FAILED: %v\n", s.Book, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func runBook(client *http.Client, baseURL, candlesDir string, s bookSpec, now time.Time, led *ledger, version string, noRecord bool) error {
	from, err := time.Parse("2006-01-02", s.HistoryFrom)
	if err != nil {
		return err
	}
	cs, err := bitsodaily.FetchRange(client, baseURL, s.Book, from, now, 365*bitsodaily.Day)
	if err != nil {
		return fmt.Errorf("fetch candles: %w", err)
	}
	rows, _ := bitsodaily.ToRows(cs, now)
	if len(rows) == 0 {
		return fmt.Errorf("no closed candles returned")
	}
	csvPath := filepath.Join(candlesDir, fmt.Sprintf("%s_daily_%s.csv", s.Book, rows[len(rows)-1].Date))
	if err := bitsodaily.WriteCSV(csvPath, s.Book, rows); err != nil {
		return fmt.Errorf("save candles: %w", err)
	}
	sum, err := fileSHA256(csvPath)
	if err != nil {
		return err
	}
	bars, err := toBars(rows)
	if err != nil {
		return err
	}
	d, pts, err := decide(bars, now)
	if err != nil {
		return err
	}
	p, err := paper(bars, pts, s.ForwardStart, s.LegCostBps)
	if err != nil {
		return err
	}
	recent := rows
	if len(recent) > 60 {
		recent = recent[len(recent)-60:]
	}
	rec := record{
		RecordedAt:  time.Now().UTC().Format(time.RFC3339),
		CodeVersion: version,
		Mode:        "dry-run",
		Book:        s.Book,
		Prereg:      s.Prereg,
		Decision:    d,
		Paper:       p,
		Candles: candleInfo{
			Source: baseURL + "/api/v3/ohlc", First: rows[0].Date, Last: rows[len(rows)-1].Date,
			Bars: len(rows), RecentGaps: bitsodaily.Gaps(recent), SHA256Short: sum[:16],
		},
	}
	printRecord(rec, csvPath)

	// Data integrity (pre-registration §5): a day already recorded must not
	// change. If Bitso revised a candle, stop and surface it.
	if prev, ok := led.get(s.Book, d.BarDate); ok {
		if prev.Decision.Signal != d.Signal || prev.Decision.Close != d.Close {
			return fmt.Errorf("%s was recorded as %s at close %v, candles now say %s at %v: investigate before continuing",
				d.BarDate, prev.Decision.Signal, prev.Decision.Close, d.Signal, d.Close)
		}
		fmt.Printf("  ledger     : %s already recorded at %s, unchanged; nothing written\n", d.BarDate, prev.RecordedAt)
		return nil
	}
	if err := checkHistory(led, s.Book, rows); err != nil {
		return err
	}
	if noRecord {
		fmt.Println("  ledger     : -no-record, nothing written")
		return nil
	}
	if err := led.append(rec); err != nil {
		return err
	}
	fmt.Println("  ledger     : recorded")
	return nil
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
	fmt.Printf("  next open  : %s -> %s (dry run: no order placed)\n", d.FillDate, strings.ToUpper(p.PendingAction))
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
